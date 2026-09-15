// Command buildbee-loadtest drives a Server with many Runs at once and
// reports how it kept up. The agents are simulated: they stream events for
// a while and stop, so the load is the Server's, not a model's.
//
//	buildbee-loadtest -url http://buildbee.lan:8080 -runs 200 -workers 10 -slots 5
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/codemodify/buildbee/internal/loadtest"
)

func main() {
	var o loadtest.Options
	var asJSON bool
	flag.StringVar(&o.Server, "url", "http://127.0.0.1:8080", "Server base URL")
	flag.StringVar(&o.As, "as", "loadtest", "the Person queueing the work")
	flag.IntVar(&o.Runs, "runs", 50, "Runs to queue")
	flag.IntVar(&o.Workers, "workers", 5, "workers to start")
	flag.IntVar(&o.Slots, "slots", 4, "Runs each worker executes at once")
	flag.DurationVar(&o.Work, "work", 2*time.Second, "how long one simulated agent turn takes")
	flag.IntVar(&o.Events, "events", 40, "events one Run streams")
	flag.DurationVar(&o.Deadline, "deadline", 10*time.Minute, "give up after this")
	flag.BoolVar(&asJSON, "json", false, "print the report as JSON")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	o.Log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	fmt.Fprintf(os.Stderr, "%d runs through %d workers x %d slots against %s\n", o.Runs, o.Workers, o.Slots, o.Server)
	rep, err := loadtest.Run(ctx, o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "loadtest:", err)
		os.Exit(1)
	}
	if asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(rep)
	} else {
		fmt.Println(rep)
	}
	for _, e := range rep.Errors {
		fmt.Fprintln(os.Stderr, "!", e)
	}
	if rep.Failed > 0 || len(rep.Errors) > 0 {
		os.Exit(1)
	}
}
