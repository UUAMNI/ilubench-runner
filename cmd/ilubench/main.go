// Command ilubench runs the IlùBench two-arm elicitation protocol against a
// model API. It is the Go port of runner.py; see README.md for usage and
// PORT_PLAN.md for the port's status.
package main

import (
	"os"

	"github.com/UUAMNI/ilubench-runner/internal/run"
)

func main() {
	os.Exit(run.Main(os.Args[1:], run.Options{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Getenv: os.Getenv,
	}))
}
