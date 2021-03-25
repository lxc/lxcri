#define _GNU_SOURCE
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <pwd.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/prctl.h>
#include <sys/types.h>
#include <unistd.h>

const char *syncfifo_path = "syncfifo";
const char *cmdline_path = "cmdline";
const char *environ_path = "environ";
const char *error_log = "error.log";

// A conformance test that will fail if SETENV_OVERWRITE is set to 0
// is "StatefulSet [k8s.io] Basic StatefulSet functionality [StatefulSetBasic]
// should have a working scale subresource [Conformance]" In the spec two
// conflicting PATH environment variables are defined. The container image
// 'httpd:2.4.38-alpine' only defines the second.
//
// This value must be set to control the behaviour for conflicting environment
// variables: SETENV_OVERWRITE=0  the variable that is defined first takes
// precedence SETENV_OVERWRITE=1  the variable that is defined last overwrites
// all previous definitions
#ifndef SETENV_OVERWRITE
#define SETENV_OVERWRITE 1
#endif

#define ERROR(format, ...)                                                \
	{                                                                 \
		dprintf(errfd, "[lxcri-init] " format, ##__VA_ARGS__); \
		exit(EXIT_FAILURE);                                       \
	}

int writefifo(const char *fifo, const char *msg)
{
	int fd;

	fd = open(fifo, O_WRONLY | O_CLOEXEC);
	if (fd == -1)
		return -1;

	if (write(fd, msg, strlen(msg)) == -1)
		return -1;

	return close(fd);
}

/* load_cmdline reads up to maxargs-1 cmdline arguments from path into args.
 * path is the path of the cmdline file.
 * The cmdline files contains a list of null terminated arguments
 * similar to /proc/<pid>/cmdline.
 * Each argument must have a maximum length of buflen (including null).
 */
int load_cmdline(const char *path, char *buf, int buflen, char **args, int maxargs)
{
	int fd;
	FILE *f;
	int n = 0;

	fd = open(path, O_RDONLY | O_CLOEXEC);
	if (fd == -1)
		return -1;

	f = fdopen(fd, "r");
	if (f == NULL)
		return -1;

	for (n = 0; n < maxargs - 1; n++) {
		char c;
		int i;
		/* read until next '\0' or EOF */
		for (i = 0; i < buflen; i++) {
			c = getc(f);
			if (c == EOF) {
				break;
			}
			buf[i] = c;
			if (c == '\0')
				break;
		}

		if (errno != 0) /* getc failed */
			return -1;

		if (c == EOF) {
			if (i > 0) /* trailing garbage */
				return -1;
			args[n] = (char *)NULL;
			break;
		}

		args[n] = strndup(buf, i);
		if (errno != 0) /* strndup failed */
			return -1;
	}
	/* cmdline is empty */
	if (n == 0)
		return -1;

	return 0;
}

/* load_environ loads environment variables from path,
 * and adds them to the process environment.
 * path is the path to a list of
 * null terminated environment variables like /proc/<pid>/environ
 * Each variable must have a maximum length of buflen (including null)
 * see https://pubs.opengroup.org/onlinepubs/9699919799/basedefs/V1_chap08.html#tag_08_01
 */
int load_environ(const char *path, char *buf, int buflen)
{
	int fd;
	FILE *f;

	fd = open(path, O_RDONLY | O_CLOEXEC);
	if (fd == -1)
		return 0;

	f = fdopen(fd, "r");
	if (f == NULL)
		return -1;

	for (;;) {
		char *key = NULL;
		char c;
		int i;
		/* read until next '\0' or EOF */
		for (i = 0; i < buflen; i++) {
			c = getc(f);
			if (c == EOF) {
				break;
			}
			buf[i] = c;
			if (c == '\0')
				break;

			/* split at first '=' */
			if (key == NULL && c == '=') {
				buf[i] = '\0';
				key = buf;
			}
		}

		if (errno != 0) /* getc failed */
			return -1;

		if (c == EOF) {
			if (i > 0) /* trailing garbage */
				return -1;
			break;
		}

		/* malformed content e.g 'fooo\0' or 'fooo=<EOF>' */
		if (key == NULL || i == strlen(key))
			return -1;

		if (setenv(key, buf + strlen(key) + 1, SETENV_OVERWRITE) == -1)
			return -1;
	}
	return 0;
}

/* Ensure_HOME_exists sets the HOME environment variable if it is not set.
 * There are containers that don't run without HOME being set e.g 'cilium v1.9.0'
 */
int ensure_HOME_exists()
{
	struct passwd *pw;
	int root_fd;

	// fast path
	if (getenv("HOME") != NULL)
		return 0;

	pw = getpwuid(geteuid());
	/* ignore error from getpwuid */
	errno = 0;
	if (pw != NULL && pw->pw_dir != NULL)
		return setenv("HOME", pw->pw_dir, 0);

	root_fd = open("/root", O_PATH | O_CLOEXEC);
	errno = 0;
	if (root_fd != -1) {
		close(root_fd);
		errno = 0;
		return setenv("HOME", "/root", 0);
	}

	return setenv("HOME", "/", 0);
}

/* To avoid leaking inherited file descriptors,
 * all file descriptors except stdio (0,1,2) are closed.
 * File descriptor leaks may lead to serious security issues.
 */
int close_extra_fds(int errfd)
{
	int fd;
	DIR *dirp = NULL;
	struct dirent *entry = NULL;

	fd = open("/proc/self/fd", O_RDONLY | O_CLOEXEC);
	if (fd == -1)
		ERROR("open /proc/self/fd failed");

	dirp = fdopendir(fd);
	if (dirp == NULL)
		ERROR("fdopendir for /proc/self/fd failed");

	while ((entry = readdir(dirp)) != NULL) {
		int xfd = atoi(entry->d_name);
		if ((xfd > 2) && (xfd != fd))
			close(fd);
	}

	closedir(dirp);
	errno = 0; // ignore errors from close
	return 0;
}

int main(int argc, char **argv)
{
	/* Buffer for reading arguments and environment variables.
	 * There is not a limit per environment variable, but we limit it to 1MiB here
	 * https://stackoverflow.com/questions/53842574/max-size-of-environment-variables-in-kubernetes.
	 *
	 * For arguments "Additionally, the limit per string is 32 pages
	 * (the kernel constant MAX_ARG_STRLEN),
	 * and the maximum number of strings is 0x7FFFFFFF."
	 */
	char buf[1024 * 1024];

	/* Null terminated list of cmline arguments.
	 * See 'man 2 execve' 'Limits on size of arguments and environment'
	 */
	char *args[256];

	const char *container_id;

	int ret = 0;

	int errfd;

	/* write errors to error.log if it exists otherwise to stderr */
	errfd = open(error_log, O_WRONLY | O_CLOEXEC);
	if (errfd == -1) {
		errno = 0;
		errfd = 2;
	}

	if (argc != 2)
		ERROR("invalid number of arguments %d\n"
		      "usage: %s <containerID>\n",
		      argc, argv[0]);

	container_id = argv[1];

	/* clear environment */
	environ = NULL;

	ret = load_environ(environ_path, buf, sizeof(buf));
	if (ret == -1)
		ERROR("error reading environment file \"%s\": %s\n",
		      environ_path, strerror(errno));

	ret = load_cmdline(cmdline_path, buf, sizeof(buf), args, sizeof(args));
	if (ret == -1)
		ERROR("error reading cmdline file \"%s\": %s\n", cmdline_path,
		      strerror(errno));

	if (ensure_HOME_exists() == -1)
		ERROR("failed to set HOME environment variable: %s\n",
		      strerror(errno));

	if (writefifo(syncfifo_path, container_id) == -1)
		ERROR("failed to write syncfifo: %s\n", strerror(errno));

	if (chdir("cwd") == -1)
		ERROR("failed to change working directory: %s\n",
		      strerror(errno));

	if (close_extra_fds(errfd) == -1)
		ERROR("failed to close extra fds: %s\n", strerror(errno));

	if (execvp(args[0], args) == -1)
		ERROR("failed to exec \"%s\": %s\n", args[0], strerror(errno));
}
