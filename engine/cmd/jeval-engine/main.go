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
		if len(os.Args) != 3 || (os.Args[1] != "--storage-check" && os.Args[1] != "--database") {
			fmt.Fprintln(os.Stderr, "usage: jeval-engine [--database ABSOLUTE_FILE | --storage-check ABSOLUTE_DIRECTORY]")
			os.Exit(2)
		}
		if os.Args[1] == "--storage-check" {
			if err := storage.Probe(context.Background(), os.Args[2]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Fprintln(os.Stdout, `{"storageCheck":"ok","schemaVersion":2,"scope":"synthetic-preview-only"}`)
			return
		}
	}
	record, err := demo.Load()
	if err == nil {
		if len(os.Args) == 1 {
			err = protocol.Serve(os.Stdin, os.Stdout, record)
		} else {
			var store *storage.Store
			store, err = storage.Open(context.Background(), os.Args[2])
			if err == nil {
				err = protocol.ServeWithStore(os.Stdin, os.Stdout, record, store)
				if closeErr := store.Close(); err == nil {
					err = closeErr
				}
			}
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
