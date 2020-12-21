#define _GNU_SOURCE
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

int writefifo(const char *fifo, const char *msg)
{
	int fd;

	// Open FIFO for write only
	fd = open(fifo, O_WRONLY | O_CLOEXEC);
	if (fd == -1)
		return -1;

	if (write(fd, msg, strlen(msg)) == -1)
		return -1;

	return close(fd);
}

/* reads up to maxlines-1 lines from path into lines */
int load_cmdline(const char *path, char *buf, int buflen, char **lines, int maxlines)
{
	int fd;
	FILE *f;
	int n = 0;

	fd = open(path, O_RDONLY | O_CLOEXEC);
	if (fd == -1)
		return 200;

	f = fdopen(fd, "r");
	if (f == NULL)
		return 201;

	for (n = 0; n < maxlines - 1; n++) {
		char c;
		int i;
		// read until next '\0' or EOF
		for (i = 0; i < buflen; i++) {
			c = getc(f);
			if (c == EOF) {
				break;
			}
			buf[i] = c;
			if (c == '\0')
				break;
		}

		if (errno != 0) // getc failed
			return 202;

		if (c == EOF) {
			if (i > 0) // trailing garbage
				return 203;
			lines[n] = (char *)NULL;
			break;
		}

		lines[n] = strndup(buf, i);
		if (errno != 0) // strndup failed
			return 204;
	}
	// empty cmdline
	if (n < 1)
		return 205;

	return 0;
}

// https://pubs.opengroup.org/onlinepubs/9699919799/basedefs/V1_chap08.html#tag_08_01
int load_environment(const char *path, char *buf, int buflen)
{
	int fd;
	FILE *f;

	fd = open(path, O_RDONLY | O_CLOEXEC);
	if (fd == -1)
		return 210;

	f = fopen(path, "r");
	if (f == NULL)
		return 211;

	for (;;) {
		char *key = NULL;
		char c;
		int i;
		// read until next '\0' or EOF
		for (i = 0; i < buflen; i++) {
			c = getc(f);
			if (c == EOF) {
				break;
			}
			buf[i] = c;
			if (c == '\0')
				break;

			// split at first '='
			if (key == NULL && c == '=') {
				buf[i] = '\0';
				key = buf;
			}
		}

		if (errno != 0) // getc failed
			return 212;

		if (c == EOF) {
			if (i > 0) // trailing garbage
				return 213;
			break;
		}

		// malformed variable
		// e.g 'fooo\0' or 'fooo=\0'
		if (key == NULL || i == strlen(key))
			return 214;

		if (setenv(key, buf + strlen(key) + 1, 0) == -1)
			return 215;
	}
	return 0;
}

void try_setenv_HOME()
{
	struct passwd *pw;

	if (getenv("HOME") != NULL)
		return;

	pw = getpwuid(geteuid());
	if (pw != NULL && pw->pw_dir != NULL)
		setenv("HOME", pw->pw_dir, 0);
	else
		setenv("HOME", "/", 0);

	// ignore errors
	errno = 0;
}

int main(int argc, char **argv)
{
	// Buffer for reading arguments and environment variables.
	// There is not a limit per environment variable, but we limit it to 1MiB here
	// https://stackoverflow.com/questions/53842574/max-size-of-environment-variables-in-kubernetes
	// For arguments "Additionally, the limit per string is 32 pages (the kernel
	// constant MAX_ARG_STRLEN), and the maximum number of strings is 0x7FFFFFFF."
	char buf[1024 * 1024];
	// see 'man 2 execve' 'Limits on size of arguments and environment'
	// ... ARG_MAX constant (either defined in <limits.h> or available at
	// run time using the call sysconf(_SC_ARG_MAX))
	char *args[256]; // > _POSIX_ARG_MAX+1

	const char *cid;

	int ret = 0;

	if (argc != 2) {
		fprintf(stderr, "invalid number of arguments %d\n", argc);
		fprintf(stderr, "usage: %s <containerID>\n", argv[0]);
		exit(-1);
	}
	cid = argv[1];

	// environment is already cleared by liblxc
	// environ = NULL;
	ret = load_environment(environ_path, buf, sizeof(buf));
	if (ret != 0) {
		if (errno != 0)
			fprintf(stderr, "error reading environment file \"%s\": %s\n",
				environ_path, strerror(errno));
		exit(ret);
	}

	ret = load_cmdline(cmdline_path, buf, sizeof(buf), args, sizeof(args));
	if (ret != 0) {
		if (errno != 0)
			fprintf(stderr, "error reading cmdline file \"%s\": %s\n",
				cmdline_path, strerror(errno));
		exit(ret);
	}

	try_setenv_HOME();

	if (writefifo(syncfifo_path, cid) == -1) {
		perror("failed to write syncfifo");
		exit(220);
	}

	if (chdir("cwd") == -1) {
		perror("failed to change working directory");
		exit(221);
	}
	execvp(args[0], args);
}
