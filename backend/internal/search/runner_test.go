package search

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHelperStderrIsNeverSurfacedAndCancelStopsProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helper")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'super-secret' >&2\nprintf '{\"code\":\"search_login\"}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	rep, err := ExecRunner(path)(context.Background(), Request{Op: "search"})
	if err != nil || rep.Code != "search_login" {
		t.Fatalf("%+v %v", rep, err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = ExecRunner(path)(ctx, Request{Op: "search"})
	if Code(err) != "search_timeout" || time.Since(start) > 2*time.Second {
		t.Fatalf("cancel failed: %v", err)
	}
}

func TestStreamingRunnerEmitsEarlyAndRefusesDuplicateOrTruncatedFrames(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       int
		valid      bool
	}{
		{"complete", `printf '{"index":0,"reply":{"code":"search_empty"}}\n'`, 1, true},
		{"duplicate", `printf '{"index":0,"reply":{}}\n{"index":0,"reply":{}}\n'`, 1, false},
		{"truncated", `printf '{"index":0,"reply":'`, 0, false},
		{"missing", `printf ''`, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "helper")
			if err := os.WriteFile(path, []byte("#!/bin/sh\n"+tc.body+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			count := 0
			err := ExecStreamRunner(path)(context.Background(), Request{Op: "batch.stream", Searches: []Request{{Op: "search"}}}, func(i int, r Reply) error { count++; return nil })
			if (err == nil) != tc.valid || count != tc.want {
				t.Fatalf("calls=%d err=%v", count, err)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "helper")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '{\"index\":0,\"reply\":{}}\\n'\nexec sleep 15\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := time.Now()
	count := 0
	err := ExecStreamRunner(path)(ctx, Request{Searches: []Request{{}}}, func(int, Reply) error { count++; cancel(); return nil })
	if count != 1 || time.Since(start) >= time.Second || Code(err) != "search_timeout" {
		t.Fatalf("late callback or process leak: %d %v", count, err)
	}
}
