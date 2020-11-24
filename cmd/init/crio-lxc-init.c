#define _GNU_SOURCE
#include <stdio.h>
#include <unistd.h>
#include <sys/types.h>
#include <fcntl.h>
#include <string.h>
#include <signal.h>
#include <errno.h>
#include <stdlib.h>
#include <sys/prctl.h>
#include <pwd.h>

#ifndef PREFIX
#define PREFIX "/.crio-lxc/"
#endif

#define runtime_path(NAME) PREFIX NAME

const char* syncfifo = runtime_path("syncfifo");
const char* cmdline_path = runtime_path("cmdline.txt");
const char* environ_path = runtime_path("environ");

int writefifo(const char* fifo, const char*msg) {
  int fd;

#ifdef DEBUG
  printf("writing fifo %s\n", fifo);
#endif

  // Open FIFO for write only 
  fd = open(fifo, O_WRONLY | O_CLOEXEC); 
  if (fd == -1)
    return -1;

  if (write(fd, msg, strlen(msg)) == -1)
    return -1;
  
  return close(fd);
}

/* reads up to maxlines-1 lines from path into lines */
int readlines(const char* path, char *buf, int buflen, char **lines, int maxlines) {
  FILE *f;
  char *line;
  int n;

#ifdef DEBUG
  printf("reading lines from %s buflen:%d maxlines:%d\n", path, buflen, maxlines);
#endif

  // FIXME open file with O_CLOEXEC

  f = fopen(path, "r");
  if(f == NULL)
      return -1;
  
  errno = 0;
  for(n = 0; n < maxlines-1; n++) {
    // https://pubs.opengroup.org/onlinepubs/009696699/functions/fgets.html
    line = fgets(buf, buflen, f);
    if (line == NULL) 
      break;
    // line gets truncated if it is longer than buflen ?
    lines[n] = strndup(line, strlen(line)-1);
  }
  if (errno != 0)
    return -1;

  if (fclose(f) != 0)
    return -1;

  lines[n] = (char *) NULL;
  return n;
}


// https://pubs.opengroup.org/onlinepubs/9699919799/basedefs/V1_chap08.html#tag_08_01
int load_environment(const char* path, char *buf, int buflen) {
  FILE *f;

#ifdef DEBUG
  printf("reading env from %s buflen:%d\n", path, buflen);
#endif

  f = fopen(path, "r");
  if(f == NULL)
      return -1;
  
  char c;

  while(c != EOF) {
    char *value = NULL;

    for(int i = 0; i < buflen; i++) {
      c = getc(f);
      if (c == EOF)  {
        // we should have receive a '\0' before
        buf[i] = '\0';
        break;
      }

      buf[i] = c;
      if (c == '\0') 
        break;
          
      // buffer is full but we did neither receive '\0' nor EOF before
      if (i == buflen-1) {
        //errno = E2BIG; 
        errno = 31;
        goto out;
      }

      // terminate enviornment key
      // the checks above ensure that we are not at the end of the buffer here
      if (value == NULL && c == '=') {
        buf[i] = '\0';
        value = buf + ( i+1 );
      }
    }
    if (errno != 0) {
      errno = 32;
      goto out;
    }

    // 'foo='
    if (value == NULL) {
      errno = 33;
      errno = EINVAL;
      goto out;
    }
#ifdef DEBUG    
    printf("setenv %s\n", buf);
#endif
    if (setenv(buf, value, 1) == -1)
      goto out;
  }

out:
  fclose(f);
  return (errno != 0) ? errno : 0;
}

int sethome() {
  struct passwd *pw;

  if (getenv("HOME") != NULL) {
    return 0;
  }
  pw = getpwuid(geteuid());
  if (pw != NULL) {
   if (pw->pw_dir != NULL)
     return setenv("HOME",  pw->pw_dir, 0);
  }
  // This is best effort so we ignore the errno set by getpwuid.
  if (errno != 0)
    errno = 0;
  return setenv("HOME", "/", 0);
}

int main(int argc, char** argv)
{
  // Buffer for reading arguments and environment variables.
  // There is not a limit per environment variable, but we limit it to 1MiB here
  // https://stackoverflow.com/questions/53842574/max-size-of-environment-variables-in-kubernetes
  // For arguments "Additionally, the limit per string is 32 pages (the kernel constant MAX_ARG_STRLEN), and the maximum number of strings is 0x7FFFFFFF."
  char buf[1024*1024];
  // see 'man 2 execve' 'Limits on size of arguments and environment'
  // ... ARG_MAX constant (either defined in <limits.h> or available at run time using the call sysconf(_SC_ARG_MAX))
  char *args[256]; // > _POSIX_ARG_MAX+1 

  const char* cid;

  if (argc != 2) {
    fprintf(stderr, "invalid number of arguments (expected 2 was %d) usage: %s <containerID>\n", argc, argv[0]);
    exit(1);
  }
   cid = argv[1];

  if (readlines(cmdline_path, buf, sizeof(buf), args, sizeof(args)) == -1){
    perror("failed to read cmdline file");
    exit(2);
   }
  
  // environment is already cleared by liblxc
  //environ = NULL;
  if (load_environment(environ_path, buf, sizeof(buf)) == -1){
    perror("failed to read environment file");
    if (errno == 0)
      exit(3);
    else 
      exit(errno);
   }
  
  if (sethome() == -1) {
    perror("failed to set HOME");
    exit(4);
  }

  if (writefifo(syncfifo, cid) == -1) {
    perror("failed to write syncfifo");
    exit(5);
  }
      
  execvp(args[0],args);
}
