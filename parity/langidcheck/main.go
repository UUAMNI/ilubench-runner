// Command langidcheck exposes the Go port's output-language heuristic and
// notes builder to parity/shadow_compare.py, so real model responses
// archived by either implementation can be classified by both and compared.
//
// It reads JSON lines on stdin, {"text": ..., "raw_path": ...}, and writes one
// JSON line per input, {"output_language": ..., "notes": ...}. Test tooling
// only; the shipped binary does not expose this.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/UUAMNI/ilubench-runner/internal/langid"
)

func main() {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 64<<20) // archived responses can be long
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	for in.Scan() {
		var req struct {
			Text    string `json:"text"`
			RawPath string `json:"raw_path"`
		}
		if err := json.Unmarshal(in.Bytes(), &req); err != nil {
			fmt.Fprintf(os.Stderr, "langidcheck: bad input line: %v\n", err)
			os.Exit(2)
		}
		resp := struct {
			OutputLanguage string `json:"output_language"`
			Notes          string `json:"notes"`
		}{langid.Detect(req.Text), langid.FactualNotes(req.Text, req.RawPath)}
		b, _ := json.Marshal(resp)
		out.Write(b)
		out.WriteByte('\n')
	}
	if err := in.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "langidcheck: %v\n", err)
		os.Exit(2)
	}
}
