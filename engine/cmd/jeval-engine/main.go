package main

import (
	"context"
	"fmt"
	"jeval/engine/internal/demo"
	"jeval/engine/internal/protocol"
	"jeval/engine/internal/storage"
	"os"
)

func main() {
	if len(os.Args) > 1 {
		if len(os.Args) != 3 || os.Args[1] != "--storage-check" {
			fmt.Fprintln(os.Stderr, "usage: jeval-engine [--storage-check ABSOLUTE_DIRECTORY]")
			os.Exit(2)
		}
		if err := storage.Probe(context.Background(), os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stdout, `{"storageCheck":"ok","schemaVersion":1,"scope":"synthetic-preview-only"}`)
		return
	}
	record, err := demo.Load()
	if err == nil {
		err = protocol.Serve(os.Stdin, os.Stdout, record)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
