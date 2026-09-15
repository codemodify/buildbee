// Command buildbee talks to the BuildBee Server (Project, Task, Handoff).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
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
	case "usage":
		out, err := c.Usage(flagValue(args[1:], "project"), flagValue(args[1:], "days"))
		if err != nil {
			return err
		}
		return c.PrintJSON(out)
	default:
		usage(os.Stderr)
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

const projectUsage = "usage: buildbee project create --name NAME [--bots] | project update --id ID [--autopilot on|off] " +
	"[--merge-policy auto|approval] [--max-runs N] [--repo URL] [--branch NAME] [--instructions TEXT]"

func runProject(args []string, c *client.Client) error {
	if len(args) == 0 {
		return errors.New(projectUsage)
	}
	switch args[0] {
	case "create":
		name := flagValue(args[1:], "name")
		if name == "" {
			return errors.New(projectUsage)
		}
		out, err := c.CreateProject(name, hasFlag(args[1:], "bots"))
		if err != nil {
			return err
		}
		return c.PrintJSON(out)
	case "update":
		id := flagValue(args[1:], "id")
		if id == "" {
			return errors.New(projectUsage)
		}
		patch := map[string]any{}
		if v := flagValue(args[1:], "max-runs"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return errors.New(projectUsage)
			}
			patch["max_runs"] = n
		}
		switch v := flagValue(args[1:], "autopilot"); v {
		case "":
		case "on", "off":
			patch["auto_run"] = v == "on"
		default:
			return errors.New(projectUsage)
		}
		for flag, field := range map[string]string{"merge-policy": "merge_policy", "repo": "repo_url", "branch": "default_branch", "instructions": "instructions"} {
			if v, ok := flagSet(args[1:], flag); ok {
				patch[field] = v
			}
		}
		out, err := c.UpdateProject(id, patch)
		if err != nil {
			return err
		}
		return c.PrintJSON(out)
	default:
		return errors.New(projectUsage)
	}
}

const taskUsage = "usage: buildbee task list --project ID | task create --project ID --title TITLE [--body TEXT] [--to ROLE|none]"

func runTask(args []string, c *client.Client) error {
	if len(args) == 0 {
		return errors.New(taskUsage)
	}
	projectID := flagValue(args[1:], "project")
	if projectID == "" {
		return errors.New(taskUsage)
	}
	switch args[0] {
	case "list":
		out, err := c.ListTasks(projectID)
		if err != nil {
			return err
		}
		return c.PrintJSON(out)
	case "create":
		title := flagValue(args[1:], "title")
		if title == "" {
			return errors.New(taskUsage)
		}
		out, err := c.CreateTask(projectID, title, flagValue(args[1:], "body"), flagValue(args[1:], "to"))
		if err != nil {
			return err
		}
		return c.PrintJSON(out)
	default:
		return errors.New(taskUsage)
	}
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

const runUsage = "usage: buildbee run start --task ID [--agent AGENT] [--bot MEMBER_ID] [--follow] | run show --id ID | " +
	"run cancel --id ID | run steer --id ID --text TEXT [--interrupt]"

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
	case "steer":
		id, text := flagValue(args[1:], "id"), flagValue(args[1:], "text")
		if id == "" || text == "" {
			return errors.New(runUsage)
		}
		out, err := c.SteerRun(id, text, hasFlag(args[1:], "interrupt"))
		if err != nil {
			return err
		}
		return c.PrintJSON(out)
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
	case "steer":
		fmt.Fprintf(info, "[%v] %v\n", ev.Payload["by"], ev.Payload["text"])
	}
}

const routineUsage = "usage: buildbee routine list --project ID | routine create --project ID --name NAME --prompt TEXT " +
	"[--schedule 24h] [--bot MEMBER_ID] [--enabled] | routine run --id ID"

func runRoutine(args []string, c *client.Client) error {
	if len(args) == 0 {
		return errors.New(routineUsage)
	}
	var out map[string]any
	var err error
	switch args[0] {
	case "list":
		projectID := flagValue(args[1:], "project")
		if projectID == "" {
			return errors.New(routineUsage)
		}
		out, err = c.ListRoutines(projectID)
	case "create":
		projectID, name, prompt := flagValue(args[1:], "project"), flagValue(args[1:], "name"), flagValue(args[1:], "prompt")
		if projectID == "" || name == "" || prompt == "" {
			return errors.New(routineUsage)
		}
		out, err = c.CreateRoutine(projectID, map[string]any{"name": name, "prompt": prompt, "schedule": flagValue(args[1:], "schedule"),
			"bot_member_id": flagValue(args[1:], "bot"), "enabled": hasFlag(args[1:], "enabled")})
	case "run":
		id := flagValue(args[1:], "id")
		if id == "" {
			return errors.New(routineUsage)
		}
		out, err = c.RunRoutine(id)
	default:
		return errors.New(routineUsage)
	}
	if err != nil {
		return err
	}
	return c.PrintJSON(out)
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

// flagSet returns a flag's value and whether it was given (possibly empty).
func flagSet(args []string, name string) (string, bool) {
	long := "--" + name
	for i, a := range args {
		if a == long && i+1 < len(args) {
			return args[i+1], true
		}
		if v, ok := strings.CutPrefix(a, long+"="); ok {
			return v, true
		}
	}
	return "", false
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
  buildbee project create --name NAME [--bots]     (--bots adds Scout, Builder, Sentry, Pulse)
  buildbee project update --id ID [--autopilot on|off] [--merge-policy auto|approval]
                          [--max-runs N] [--repo URL] [--branch NAME] [--instructions TEXT]
  buildbee task list --project ID
  buildbee task create --project ID --title TITLE [--body TEXT] [--to ROLE|none]
  buildbee handoff create --task ID [--to ID | --to-role ROLE] [--note TEXT] [--autorun]
  buildbee run start --task ID [--agent AGENT] [--bot MEMBER_ID] [--follow]
  buildbee run show --id ID
  buildbee run cancel --id ID
  buildbee run steer --id ID --text TEXT [--interrupt]
  buildbee usage [--project ID] [--days 30]
  buildbee routine list --project ID
  buildbee routine create --project ID --name NAME --prompt TEXT [--schedule 24h] [--bot MEMBER_ID] [--enabled]
  buildbee routine run --id ID

Environment:
  BUILDBEE_URL           Server base URL (default http://127.0.0.1:8080)
  BUILDBEE_AS            your name in BuildBee (default: your login name)

Runs are queued on the Server and executed by any buildbee-worker that
offers the agent. --follow streams the Run until it finishes.
`)
}
