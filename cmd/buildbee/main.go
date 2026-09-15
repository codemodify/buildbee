// Command buildbee talks to the BuildBee Server (Project, Task, Handoff).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/codemodify/buildbee/internal/client"
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
	case "run":
		return runRun(args[1:], c)
	case "routine":
		return runRoutine(args[1:], c)
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
		return fmt.Errorf("usage: buildbee handoff create --task ID [--to ID | --to-role ROLE] [--note TEXT] [--autorun]")
	}
	task := flagValue(args[1:], "task")
	to := flagValue(args[1:], "to")
	toRole := flagValue(args[1:], "to-role")
	note := flagValue(args[1:], "note")
	autorun := hasFlag(args[1:], "autorun")
	if task == "" || (to == "" && toRole == "") {
		return fmt.Errorf("usage: buildbee handoff create --task ID [--to ID | --to-role ROLE] [--note TEXT] [--autorun]")
	}
	out, err := c.CreateHandoff(task, to, note, toRole, autorun)
	if err != nil {
		return err
	}
	return c.PrintJSON(out)
}

const runUsage = "usage: buildbee run start --task ID [--agent AGENT] [--bot MEMBER_ID] [--follow] | run show --id ID | run cancel --id ID"

func runRun(args []string, c *client.Client) error {
	if len(args) == 0 {
		return errors.New(runUsage)
	}
	switch args[0] {
	case "start":
		task := flagValue(args[1:], "task")
		if task == "" {
			return errors.New(runUsage)
		}
		run, err := c.CreateRun(task, flagValue(args[1:], "agent"), flagValue(args[1:], "bot"))
		if err != nil {
			return err
		}
		if !hasFlag(args[1:], "follow") {
			return c.PrintJSON(run)
		}
		id, _ := run["id"].(string)
		fmt.Fprintf(os.Stderr, "run %s queued\n", id)
		status, err := c.Follow(context.Background(), id, func(ev client.RunEvent) { printEvent(c.Output, os.Stderr, ev) })
		if err != nil {
			return err
		}
		if status != "succeeded" {
			return fmt.Errorf("run %s %s", id, status)
		}
		return nil
	case "show", "cancel":
		id := flagValue(args[1:], "id")
		if id == "" {
			return errors.New(runUsage)
		}
		var out map[string]any
		var err error
		if args[0] == "cancel" {
			out, err = c.UpdateRun(id, "canceled", "canceled from the CLI")
		} else {
			out, err = c.GetRun(id)
		}
		if err != nil {
			return err
		}
		return c.PrintJSON(out)
	default:
		return errors.New(runUsage)
	}
}

// printEvent writes agent output to out and everything else to info.
func printEvent(out, info io.Writer, ev client.RunEvent) {
	text, _ := ev.Payload["text"].(string)
	switch ev.Kind {
	case "token":
		fmt.Fprint(out, text)
	case "log":
		fmt.Fprint(info, text)
	case "status":
		fmt.Fprintf(info, "[%v] %v\n", ev.Payload["status"], ev.Payload["detail"])
	case "tool_call":
		fmt.Fprintf(info, "[tool] %v\n", ev.Payload["name"])
	}
}

func runRoutine(args []string, c *client.Client) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: buildbee routine list --project ID | routine run --id ID")
	}
	switch args[0] {
	case "list":
		projectID := flagValue(args[1:], "project")
		if projectID == "" {
			return fmt.Errorf("usage: buildbee routine list --project ID")
		}
		out, err := c.ListRoutines(projectID)
		if err != nil {
			return err
		}
		return c.PrintJSON(out)
	case "run":
		id := flagValue(args[1:], "id")
		if id == "" {
			return fmt.Errorf("usage: buildbee routine run --id ID")
		}
		out, err := c.RunRoutine(id)
		if err != nil {
			return err
		}
		return c.PrintJSON(out)
	default:
		return fmt.Errorf("usage: buildbee routine list --project ID | routine run --id ID")
	}
}

func hasFlag(args []string, name string) bool {
	want := "--" + name
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
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
  buildbee handoff create --task ID [--to ID | --to-role ROLE] [--note TEXT] [--autorun]
  buildbee run start --task ID [--agent AGENT] [--bot MEMBER_ID] [--follow]
  buildbee run show --id ID
  buildbee run cancel --id ID
  buildbee routine list --project ID
  buildbee routine run --id ID

Environment:
  BUILDBEE_URL           Server base URL (default http://127.0.0.1:8080)
  BUILDBEE_AS            your name in BuildBee (default: your login name)

Runs are queued on the Server and executed by any buildbee-worker that
offers the agent. --follow streams the Run until it finishes.
`)
}
