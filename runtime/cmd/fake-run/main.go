// Command fake-run posts a successful Run to the Server without a Sandbox.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/codemodify/buildbee/runtime/notify"
)

func main() {
	server := flag.String("server", env("BUILDBEE_URL", "http://127.0.0.1:8080"), "Server base URL")
	task := flag.String("task", "", "Task ID")
	flag.Parse()
	if *task == "" {
		fmt.Fprintln(os.Stderr, "usage: fake-run --task TASK_ID [--server URL]")
		os.Exit(1)
	}
	run, err := notify.New(*server).FakeSuccess(*task)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(run)
}

func env(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
