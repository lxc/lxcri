package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

var logPrefix string

func init() {
	logPrefix = fmt.Sprintf(">> %s(pid:%d) ", os.Args[0], os.Getpid())
}

func logf(format string, args ...interface{}) {
	fmt.Printf(logPrefix+format+"\n", args...)
}

func main() {
	sigs := make(chan os.Signal, 1)

	// SIGHUP by default terminates the process, if the process does not catch it.
	// `nohup` can be used ignore it.
	// see https://en.wikipedia.org/wiki/SIGHUP
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)

	go func() {
		sig := <-sigs
		logf("received signal %q", sig)
	}()

	logf("begin")

	sec := 3
	if s, ok := os.LookupEnv("SLEEP"); ok {
		n, err := strconv.Atoi(s)
		if err != nil {
			panic(err)
		}
		logf("using env SLEEP value %s", s)
		sec = n
	}

	f, err := os.Open("/proc/self/mounts")
	if err != nil {
		panic(err)
	}
	logf("writing /proc/self/mounts")
	io.Copy(os.Stdout, f)

	logf("sleeping for %d seconds", sec)
	time.Sleep(time.Second * time.Duration(sec))

	logf("end")
}
