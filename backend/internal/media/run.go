package media

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// StderrLimit keeps the tail of diagnostics, where yt-dlp prints ERROR lines.
const StderrLimit = 16 << 10

var ErrOutputLimit = errors.New("media tool output limit")

// Exec runs one tool invocation; tests replace it with fixtures.
type Exec func(ctx context.Context, name string, args, env []string, stdoutMax int64) (stdout, stderr []byte, err error)

type capped struct {
	mu       sync.Mutex
	b        []byte
	max      int64
	overflow func()
}

func (c *capped) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if int64(len(c.b)+len(p)) > c.max {
		c.overflow()
		return 0, ErrOutputLimit
	}
	c.b = append(c.b, p...)
	return len(p), nil
}

type tail struct {
	mu sync.Mutex
	b  []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > StderrLimit {
		t.b = append([]byte(nil), t.b[len(t.b)-StderrLimit:]...)
	}
	return len(p), nil
}

// Run starts the tool in its own process group, so a timeout, cancellation
// or oversized reply also stops helpers it started (for example ffmpeg).
func Run(ctx context.Context, name string, args, env []string, stdoutMax int64) ([]byte, []byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	var once sync.Once
	var reason error
	kill := func(why error) {
		once.Do(func() {
			reason = why
			if cmd.Process != nil {
				syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		})
	}
	out := &capped{max: stdoutMax, overflow: func() { go kill(ErrOutputLimit) }}
	errs := &tail{}
	cmd.Stdout, cmd.Stderr = out, errs
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			kill(ctx.Err())
		case <-done:
		}
	}()
	err := cmd.Wait()
	close(done)
	once.Do(func() {})
	if reason != nil {
		err = reason
	}
	out.mu.Lock()
	defer out.mu.Unlock()
	errs.mu.Lock()
	defer errs.mu.Unlock()
	return out.b, errs.b, err
}

// Proc is one long-running download in its own process group.
type Proc struct {
	cmd    *exec.Cmd
	errs   *tail
	lines  chan struct{}
	stop   sync.Once
	grace  time.Duration
	exited chan struct{}
}

// Start runs the tool and calls onLine for each stdout line, in order.
func Start(name string, args, env []string, onLine func(string)) (*Proc, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	p := &Proc{cmd: cmd, errs: &tail{}, lines: make(chan struct{}), grace: 10 * time.Second, exited: make(chan struct{})}
	cmd.Stderr = p.errs
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	// Lower priority for the group; ffmpeg children inherit it.
	syscall.Setpriority(syscall.PRIO_PROCESS, cmd.Process.Pid, 10)
	go func() {
		defer close(p.lines)
		s := bufio.NewScanner(out)
		s.Buffer(make([]byte, 64<<10), 64<<10)
		for s.Scan() {
			onLine(s.Text())
		}
		io.Copy(io.Discard, out)
	}()
	return p, nil
}

// Stop asks the group to finish (yt-dlp keeps .part files for resuming) and
// kills it if it has not exited after the grace period.
func (p *Proc) Stop() {
	p.stop.Do(func() {
		pid := p.cmd.Process.Pid
		syscall.Kill(-pid, syscall.SIGTERM)
		go func() {
			select {
			case <-p.exited:
			case <-time.After(p.grace):
				syscall.Kill(-pid, syscall.SIGKILL)
			}
		}()
	})
}

// Wait returns the stderr tail and the exit error after stdout is drained.
func (p *Proc) Wait() (string, error) {
	<-p.lines
	err := p.cmd.Wait()
	close(p.exited)
	// Helpers left by the tool may not outlive it.
	syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	p.errs.mu.Lock()
	defer p.errs.mu.Unlock()
	return string(p.errs.b), err
}
