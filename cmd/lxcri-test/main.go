package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {

	sigs := make(chan os.Signal, 1)

	// SIGHUP by default terminates the process, if the process does not catch it.
	// `nohup` can be used ignore it.
	// see https://en.wikipedia.org/wiki/SIGHUP
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)

	go func() {
		sig := <-sigs
		fmt.Println()
		fmt.Println("received signal:", sig)
	}()

	fmt.Printf("%#v\n", os.Args)
	println("sleeping for 30 seconds")
	f, err := os.Open("/proc/self/mounts")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	io.Copy(os.Stdout, f)
	time.Sleep(time.Second * 30)
}
