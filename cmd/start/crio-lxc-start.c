#define _GNU_SOURCE
#include <stdio.h>
#include <unistd.h>
#include <sys/types.h>
#include <fcntl.h>
#include <string.h>
#include <signal.h>
#include <errno.h>

#include <lxc/lxccontainer.h>

/*
/ Set to 0 to disable use of lxc-init.
/ The container process should have PID 1.
*/
#define ENABLE_LXCINIT 0

/* NOTE lxc_execute.c was taken as guidline and some lines where copied. */
int main(int argc, char** argv)
{
	int ret;
	struct lxc_container *c;
	int err = EXIT_FAILURE;
	const char * name;
	const char * lxcpath;
	const char * rcfile;

	/* Ensure stdout and stderr are line bufferd. */
	setvbuf(stdout, NULL, _IOLBF, -1);
	setvbuf(stderr, NULL, _IOLBF, -1);

	if (argc != 4) {
		fprintf(stderr, "invalid cmdline: usage %s <container_name> <lxcpath> <config_path>\n", argv[0]);
		exit(err);
	}

	ret = isatty(STDIN_FILENO);
	if (ret < 0) {
		perror("isatty");
		exit(96);
	}

	/*
	/ If this is non interactive, get rid of our controlling terminal,
	/ since we don't want lxc's setting of ISIG to ignore user's ^Cs.
	/ Ignore any error - because controlling terminal could be a PTY.
	*/
	setsid();

	name = argv[1];
	lxcpath = argv[2];
	rcfile = argv[3];

	c = lxc_container_new(name, lxcpath);
	if (!c) {
		fprintf(stderr, "failed to create container");
		exit(err);
	}

	c->clear_config(c);
	if (!c->load_config(c, rcfile)) {
		fprintf(stderr, "failed to load container config file");
		goto out;
	}

	/* Do not daemonize - this would null the inherited stdio. */
	c->daemonize = false;

	if (!c->start(c, ENABLE_LXCINIT, NULL)) {
		fprintf(stderr, "lxc container failed to start");
		goto out;
	}

	if (WIFEXITED(c->error_num))
		err = WEXITSTATUS(c->error_num);
	else
		/* Try to die with the same signal the task did. */
		kill(0, WTERMSIG(c->error_num));

out:
	lxc_container_put(c);
	exit(err);
}
