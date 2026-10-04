package main

import (
	"fmt"
	"jeval/engine/internal/demo"
	"jeval/engine/internal/protocol"
	"os"
)

func main() {
	record, err := demo.Load()
	if err == nil {
		err = protocol.Serve(os.Stdin, os.Stdout, record)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
