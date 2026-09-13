// Command buildbee is the BuildBee CLI. Members use it to talk to the Server
// (Project, Task, Run) from a terminal. This scaffold only implements version.
package main

import (
	"fmt"
	"os"
)

// Version is the CLI version string. Overridden at build time later.
const Version = "0.0.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage(os.Stderr)
		return fmt.Errorf("missing command")
	}
	switch args[0] {
	case "version", "-v", "--version":
		fmt.Printf("buildbee %s\n", Version)
		return nil
	case "help", "-h", "--help":
		usage(os.Stdout)
		return nil
	default:
		usage(os.Stderr)
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func usage(w *os.File) {
	fmt.Fprint(w, `buildbee — CLI for a BuildBee Project

Usage:
  buildbee version   Print the CLI version
  buildbee help      Show this help
`)
}
