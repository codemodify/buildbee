// Command buildbee talks to the BuildBee Server (Project, Task, Handoff).
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/codemodify/buildbee/cli/internal/client"
)

// Version is the CLI version string. Overridden at build time later.
const Version = "0.0.0"

func main() {
	if err := run(os.Args[1:], client.New()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, c *client.Client) error {
	if len(args) == 0 {
		usage(os.Stderr)
		return fmt.Errorf("missing command")
	}
	switch args[0] {
	case "version", "-v", "--version":
		fmt.Fprintf(c.Output, "buildbee %s\n", Version)
		return nil
	case "help", "-h", "--help":
		usage(os.Stdout)
		return nil
	case "project":
		return runProject(args[1:], c)
	case "task":
		return runTask(args[1:], c)
	case "handoff":
		return runHandoff(args[1:], c)
	default:
		usage(os.Stderr)
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func runProject(args []string, c *client.Client) error {
	if len(args) == 0 || args[0] != "create" {
		return fmt.Errorf("usage: buildbee project create --name NAME")
	}
	name := flagValue(args[1:], "name")
	if name == "" {
		return fmt.Errorf("usage: buildbee project create --name NAME")
	}
	out, err := c.CreateProject(name)
	if err != nil {
		return err
	}
	return c.PrintJSON(out)
}

func runTask(args []string, c *client.Client) error {
	if len(args) == 0 || args[0] != "list" {
		return fmt.Errorf("usage: buildbee task list --project ID")
	}
	projectID := flagValue(args[1:], "project")
	if projectID == "" {
		return fmt.Errorf("usage: buildbee task list --project ID")
	}
	out, err := c.ListTasks(projectID)
	if err != nil {
		return err
	}
	return c.PrintJSON(out)
}

func runHandoff(args []string, c *client.Client) error {
	if len(args) == 0 || args[0] != "create" {
		return fmt.Errorf("usage: buildbee handoff create --task ID --from ID --to ID [--note TEXT]")
	}
	task := flagValue(args[1:], "task")
	from := flagValue(args[1:], "from")
	to := flagValue(args[1:], "to")
	note := flagValue(args[1:], "note")
	if task == "" || from == "" || to == "" {
		return fmt.Errorf("usage: buildbee handoff create --task ID --from ID --to ID [--note TEXT]")
	}
	out, err := c.CreateHandoff(task, from, to, note)
	if err != nil {
		return err
	}
	return c.PrintJSON(out)
}

func flagValue(args []string, name string) string {
	long := "--" + name
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == long && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, long+"=") {
			return strings.TrimPrefix(a, long+"=")
		}
	}
	return ""
}

func usage(w io.Writer) {
	fmt.Fprint(w, `buildbee — CLI for a BuildBee Project

Usage:
  buildbee version
  buildbee project create --name NAME
  buildbee task list --project ID
  buildbee handoff create --task ID --from ID --to ID [--note TEXT]

Environment:
  BUILDBEE_URL   Server base URL (default http://127.0.0.1:8080)
`)
}
