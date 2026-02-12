package main

import (
	"fmt"
	"os"

	"editor/config"
	"editor/editor"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default()
	}

	e := editor.New(cfg)

	files := []string{}
	args := os.Args[1:]
	isDirOpen := false

	// Check if first argument is a directory
	if len(args) > 0 {
		info, err := os.Stat(args[0])
		if err == nil && info.IsDir() {
			// Change to that directory
			if err := os.Chdir(args[0]); err != nil {
				fmt.Fprintf(os.Stderr, "error: cannot change to directory %s: %v\n", args[0], err)
				os.Exit(1)
			}
			// Don't pass directory as a file to open
			files = args[1:]
			isDirOpen = true
		} else {
			// Not a directory, treat all args as files
			files = args
		}
	}

	if err := e.Run(files, isDirOpen); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
