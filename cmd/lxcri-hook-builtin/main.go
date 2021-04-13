package main

import (
	"fmt"
	"github.com/drachenfels-de/lxcri/pkg/specki"
	"os"
)

func main() {
	state, spec, err := specki.InitHook(os.Stdin)
	if err != nil {
		panic(err)
	}
	fmt.Printf("----> %s %#v\n", os.Args[0], state)
	fmt.Printf("----> %s %#v\\n", os.Args[0], spec)
}
