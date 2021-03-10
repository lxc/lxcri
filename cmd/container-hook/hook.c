#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <libgen.h> // dirname
#include <limits.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mount.h>
#include <sys/stat.h>
#include <sys/sysmacros.h>
#include <sys/types.h>
#include <unistd.h>

#define ERROR(...)                   \
	{                            \
		printf(__VA_ARGS__); \
		ret = EXIT_FAILURE;  \
		goto out;            \
	}

int mask_paths_at(int rootfs, int runtime, const char *masked)
{
	// limits.h PATH_MAX
	char line[PATH_MAX];
	const char *rel;
	int fd;
	FILE *f;

	printf("reading file \"%s\" from runtime directory\n", masked);
	fd = openat(runtime, masked, O_RDONLY);
	if (fd == -1) {
		if (errno == ENOENT) {
			printf("file \"%s\" does not exist\n", masked);
			return 0;
		}
		return -1;
	}

	f = fdopen(fd, "r");
	if (f == NULL) {
		printf("file descriptor for runtime directory is null: %s",
		       strerror(errno));
		close(fd);
		return -1;
	}

	if (fchdir(rootfs) != 0) {
		printf("file to change to rootfs: %s\n", strerror(errno));
		goto out;
	}

	while (fgets(line, sizeof(line), f) != NULL) {
		line[strlen(line) - 1] = '\0';		  // remove newline;
		rel = (line[0] == '/') ? line + 1 : line; // trim leading '/'
		struct stat path_stat;
		if (stat(rel, &path_stat) == -1) {
			if (errno == ENOENT) {
				printf("ignore non existing filepath %s\n", rel);
				errno = 0;
				continue;
			}
			goto out;
		}

		if (S_ISDIR(path_stat.st_mode)) {
			printf("masking directory %s\n", rel);
			if (mount("tmpfs", rel, "tmpfs", MS_RDONLY, NULL) == -1)
				goto out;
		} else {
			printf("masking file %s\n", rel);
			if (mount("/dev/null", rel, NULL, MS_BIND, NULL) == -1)
				goto out;
		}
	}
out:
	fclose(f);
	return (errno == 0) ? 0 : -1;
}

/* reads up to maxlines-1 lines from path into lines */
int create_devices_at(int rootfs, int runtime, const char *devices)
{
	int fd;
	FILE *f = NULL;
	char buf[256];

	printf("reading file \"%s\" from runtime directory\n", devices);
	fd = openat(runtime, devices, O_RDONLY);
	if (fd == -1) {
		if (errno == ENOENT) {
			printf("file \"%s\" does not exist\n", devices);
			return 0;
		}
		return -1;
	}

	f = fdopen(fd, "r");
	if (f == NULL) {
		printf("file descriptor for runtime directory is null: %s",
		       strerror(errno));
		close(fd);
		return -1;
	}

	for (int line = 0;; line++) {
		char mode;
		int major, minor;
		unsigned int filemode;
		int uid, gid;
		char *dir = NULL;
		char *dev = NULL;
		char *sep = NULL;
		char *tmp = NULL;
		int ret;

		if (fchdir(rootfs) == -1) {
			printf("file to change to rootfs: %s\n", strerror(errno));
			goto out;
		}

		ret = fscanf(f, "%s %c %d %d %o %d:%d\n", &buf[0], &mode,
			     &major, &minor, &filemode, &uid, &gid);
		if (ret == EOF)
			goto out;

		if (ret != 7) {
			// errno is not set on a matching error
			printf("invalid format at line %d at token %d\n", line, ret);
			fclose(f);
			return -1;
		}

		dev = (buf[0] == '/') ? buf + 1 : buf;

		struct stat path_stat;
		if (stat(dev, &path_stat) == 0) {
			printf("ignore existing device %s\n", dev);
			continue;
		}

		int ft;
		switch (mode) {
		case 'b':
			ft = S_IFBLK;
			break;
		case 'c':
			ft = S_IFCHR;
			break;
		case 'f':
			ft = S_IFIFO;
			break;
		default:
			printf("%s:%d unsupported device mode '%c'\n", devices, line, mode);
			return -1;
		}

		sep = strrchr(dev, '/');
		if (sep != NULL) {
		  printf("creating non-existent directories for device path \"%s\"\n", dev);
			*sep = '\0';
			tmp = dev;
			dev = sep + 1;
			for ((dir = strtok(tmp, "/")); dir != NULL;
			     dir = strtok(NULL, "/")) {
				if (mkdir(dir, 0755) == -1) {
					if (errno == EEXIST)
						errno = 0;
					else
						goto out;
				}
				if (chdir(dir) != 0) {
          printf("%s:%d failed to change to directory \"%s\": %s\n", 
							devices, line, dir, strerror(errno));
					goto out;
				}
			}
		}
		printf("creating device: %s %c %d %d mode:%o %d:%d\n",
		       dev, mode, major, minor, filemode, uid, gid);
		ret = mknod(dev, ft | filemode, makedev(major, minor));
		if (ret == -1) {
			printf("%s:%d failed to create device \"%s\"\n",
			       devices, line, dev);
			goto out;
		}
		ret = chown(dev, uid, gid);
		if (ret == -1) {
			printf("%s:%d failed to chown %d:%d device \"%s\"\n",
			       devices, line, uid, gid, dev);
			goto out;
		}
	}

	int ret;

	ret = symlink("/proc/self/fd/0", "/dev/stdin");
	if (ret == -1) {
		printf("failed to create symlink /dev/stdin -> /proc/self/fd/0 : %s\n", strerror(errno));
		goto out;
	}
	ret = symlink("/proc/self/fd/1", "/dev/stdout");
	if (ret == -1) {
		printf("failed to create symlink /dev/stdout -> /proc/self/fd/1 : %s\n", strerror(errno));
		goto out;
	}
	ret = symlink("/proc/self/fd/2", "/dev/stderr");
	if (ret == -1) {
		printf("failed to create symlink /dev/stderr -> /proc/self/fd/2 : %s\n", strerror(errno));
		goto out;
	}
out:
	fclose(f);
	return (errno == 0) ? 0 : -1;
}

int main(int argc, char **argv)
{
	const char *rootfs_mount;
	const char *config_file;
	const char *runtime_path;
	int rootfs_fd;
	int runtime_fd;
	int ret = EXIT_SUCCESS;

	rootfs_mount = getenv("LXC_ROOTFS_MOUNT");
	config_file = getenv("LXC_CONFIG_FILE");

	if (rootfs_mount == NULL)
		ERROR("LXC_ROOTFS_MOUNT environment variable not set\n");

	if (config_file == NULL)
		ERROR("LXC_CONFIG_FILE environment variable not set\n");

	rootfs_fd = open(rootfs_mount, O_PATH);
	if (rootfs_fd == -1)
		ERROR("failed to open rootfs mount directory: %s",
		      strerror(errno));

	runtime_path = dirname(strdup(config_file));
	runtime_fd = open(runtime_path, O_PATH);

	if (runtime_fd == -1)
		ERROR("failed to open runtime directory: %s", strerror(errno));

	printf("create devices int container rootfs\n");
	if (create_devices_at(rootfs_fd, runtime_fd, "devices.txt") == -1)
		ERROR("failed to create devices: %s", strerror(errno));

	printf("masking files and directories in container rootfs\n");
	if (mask_paths_at(rootfs_fd, runtime_fd, "masked.txt") == -1)
		ERROR("failed to mask paths: %s", strerror(errno));

out:
	if (rootfs_fd >= 0)
		close(rootfs_fd);

	if (runtime_fd >= 0)
		close(runtime_fd);

	exit(ret);
}
