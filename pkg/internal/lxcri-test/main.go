package main

import (
	"fmt"
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
	time.Sleep(time.Second * 30)
}
