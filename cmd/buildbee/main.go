// Command buildbee talks to the BuildBee Server (Project, Task, Handoff).
package main

import (
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

func runRun(args []string, c *client.Client) error {
	if len(args) == 0 || args[0] != "start" {
		return fmt.Errorf("usage: buildbee run start --task ID [--repo-url URL] [--cmd CMD] [--fake] [--acp] [--agent claude|codex|opencode|goose|fake]")
	}
	task := flagValue(args[1:], "task")
	if task == "" {
		return fmt.Errorf("usage: buildbee run start --task ID [--repo-url URL] [--cmd CMD] [--fake] [--acp] [--agent claude|codex|opencode|goose|fake]")
	}
	repo := flagValue(args[1:], "repo-url")
	cmd := flagValue(args[1:], "cmd")
	fake := hasFlag(args[1:], "fake")
	acpMode := hasFlag(args[1:], "acp")
	agent := flagValue(args[1:], "agent")
	if acpMode && agent == "" {
		agent = "auto"
	}
	if acpMode && (agent == "fake" || fake) {
		out, err := c.FakeACPRun(task, agent)
		if err != nil {
			return err
		}
		return c.PrintJSON(out)
	}
	if fake && !acpMode {
		out, err := c.FakeStartRun(task, cmd, repo)
		if err != nil {
			return err
		}
		return c.PrintJSON(out)
	}
	workerURL := os.Getenv("BUILDBEE_WORKER_URL")
	if workerURL == "" {
		workerURL = "http://127.0.0.1:8090"
	}
	run, err := c.CreateRun(task)
	if err != nil {
		return err
	}
	runID, _ := run["id"].(string)
	title, notes := "", ""
	if t, err := c.GetTask(task); err == nil {
		title, _ = t["title"].(string)
	}
	out, err := c.WorkerStart(workerURL, task, runID, repo, cmd, fake, acpMode, agent, title, notes)
	if err != nil {
		// Record the truth: the Run did not execute. Never fall back to a fake success.
		if _, uerr := c.UpdateRun(runID, "failed", "worker: "+err.Error()); uerr != nil {
			fmt.Fprintf(os.Stderr, "also failed to mark run %s failed: %v\n", runID, uerr)
		}
		return fmt.Errorf("run %s failed: %w", runID, err)
	}
	return c.PrintJSON(out)
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
  buildbee run start --task ID [--repo-url URL] [--cmd CMD] [--fake] [--acp] [--agent claude|codex|fake]
  buildbee routine list --project ID
  buildbee routine run --id ID

Environment:
  BUILDBEE_URL           Server base URL (default http://127.0.0.1:8080)
  BUILDBEE_AS            your name in BuildBee (default: your login name)
  BUILDBEE_WORKER_URL   Worker (default http://127.0.0.1:8090)
`)
}
