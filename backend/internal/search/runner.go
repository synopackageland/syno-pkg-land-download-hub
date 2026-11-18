package search

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"time"
)

// ExecRunner uses fixed argv, private stdin and a clean environment. Neither
// stdout diagnostics nor stderr (possibly containing tracker secrets) escape.
func ExecRunner(binary string) Runner {
	return func(ctx context.Context, in Request) (Reply, error) {
		ctx, cancel := context.WithTimeout(ctx, helperDeadline(in))
		defer cancel()
		b, err := json.Marshal(in)
		if err != nil {
			return Reply{}, err
		}
		cmd := exec.CommandContext(ctx, binary)
		cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
		cmd.Stdin = bytes.NewReader(b)
		cmd.Stderr = io.Discard
		cmd.WaitDelay = time.Second
		out := &boundedOutput{max: 16 << 20}
		cmd.Stdout = out
		if err = cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return Reply{}, ctx.Err()
			}
			return Reply{}, fail("search_helper")
		}
		var rep Reply
		d := json.NewDecoder(bytes.NewReader(out.b))
		d.DisallowUnknownFields()
		if d.Decode(&rep) != nil {
			return Reply{}, fail("search_helper")
		}
		return rep, nil
	}
}

type boundedOutput struct {
	b   []byte
	max int
}

func (w *boundedOutput) Write(p []byte) (int, error) {
	if len(w.b)+len(p) > w.max {
		return 0, fail("search_output_limit")
	}
	w.b = append(w.b, p...)
	return len(p), nil
}

// ExecStreamRunner reads bounded NDJSON from the same private helper process.
// Bad/duplicate indexes and truncated output fail closed. A callback failure
// cancels the helper, including its outstanding network requests.
func ExecStreamRunner(binary string) StreamRunner {
	return func(ctx context.Context, in Request, emit func(int, Reply) error) error {
		ctx, cancel := context.WithTimeout(ctx, helperDeadline(in))
		defer cancel()
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, binary)
		cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
		cmd.Stdin = bytes.NewReader(b)
		cmd.Stderr = io.Discard
		cmd.WaitDelay = time.Second
		pipe, err := cmd.StdoutPipe()
		if err != nil {
			return fail("search_helper")
		}
		if err = cmd.Start(); err != nil {
			return fail("search_helper")
		}
		d := json.NewDecoder(io.LimitReader(pipe, 16<<20))
		d.DisallowUnknownFields()
		seen := make(map[int]bool)
		for {
			var row struct {
				Index int   `json:"index"`
				Reply Reply `json:"reply"`
			}
			err = d.Decode(&row)
			if err == io.EOF {
				break
			}
			if err != nil || row.Index < 0 || row.Index >= len(in.Searches) || seen[row.Index] {
				err = fail("search_helper")
				break
			}
			seen[row.Index] = true
			if err = emit(row.Index, row.Reply); err != nil {
				break
			}
		}
		if err != io.EOF && err != nil {
			cancel()
		}
		waitErr := cmd.Wait()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil && err != io.EOF {
			return err
		}
		if waitErr != nil || len(seen) != len(in.Searches) {
			return fail("search_helper")
		}
		return nil
	}
}

func helperDeadline(in Request) time.Duration {
	if in.SolverURL != "" {
		return solverDeadline
	}
	return 25 * time.Second
}
