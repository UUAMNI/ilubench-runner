// Command ilubench runs the IlùBench two-arm elicitation protocol against a
// model API. It is the Go port of runner.py; see README.md for usage and
// PORT_PLAN.md for the port's status.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/UUAMNI/ilubench-runner/internal/run"
)

func main() {
	// Ctrl-C (SIGINT) or SIGTERM cancels the context, which aborts the
	// in-flight HTTP request; run.Main then exits 130 without a stack trace.
	// Once the first signal has been delivered, stop() restores the default
	// handler so a second Ctrl-C kills the process immediately.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		stop()
	}()
	code := run.Main(ctx, os.Args[1:], run.Options{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Getenv: os.Getenv,
	})
	stop()
	os.Exit(code)
}
