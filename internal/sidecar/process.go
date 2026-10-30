package sidecar

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// Child stop windows, per platform and in total.
const (
	lineLimit     = 500
	stopGraceTime = 5 * time.Second
	stopKillTime  = 2 * time.Second
)

var (
	errProcNotRunning = errors.New("service is not running")
	errProcRunning    = errors.New("service is already running")
	errProcStopTimed  = errors.New("timed out waiting for process to stop")
)

// Proc supervises the single child process of one inbound and keeps the last
// lines it printed. The captured buffer survives a restart of the child.
type Proc struct {
	name string
	out  *lineBuffer

	mu       sync.Mutex
	cmd      *exec.Cmd
	done     chan struct{}
	running  bool
	stopping bool
}

// NewProc returns an idle supervised process for the given log label.
func NewProc(name string) *Proc {
	return &Proc{name: name, out: newLineBuffer(name)}
}

// IsRunning reports whether a child was spawned and has not exited yet.
func (p *Proc) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// LastLine returns the most recently captured line, or an empty string.
func (p *Proc) LastLine() string { return p.out.last() }

// Lines returns a snapshot of the captured lines, oldest first. A max of zero
// or less returns everything retained.
func (p *Proc) Lines(max int) []string { return p.out.snapshot(max) }

// Signal forwards a signal to the live child.
func (p *Proc) Signal(sig syscall.Signal) error {
	p.mu.Lock()
	cmd := p.cmd
	running := p.running
	p.mu.Unlock()
	if !running || cmd == nil || cmd.Process == nil {
		return errProcNotRunning
	}
	return cmd.Process.Signal(sig)
}

// Start spawns the child with the given argv. stdout and stderr share one
// capture stream; the child inherits the panel's environment plus extraEnv and
// working directory and reads end-of-file on stdin.
func (p *Proc) Start(bin string, args []string, extraEnv ...string) error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return errProcRunning
	}
	p.mu.Unlock()

	cmd := exec.CommandContext(context.Background(), bin, args...)
	if len(extraEnv) > 0 {
		// Credentials are handed over in the environment instead of argv, which
		// any local user can read from /proc/<pid>/cmdline.
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	cmd.Stdout = p.out
	cmd.Stderr = p.out
	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan struct{})
	p.mu.Lock()
	p.cmd = cmd
	p.done = done
	p.running = true
	p.stopping = false
	p.mu.Unlock()

	go p.watch(cmd, done)
	return nil
}

// Stop ends the child: TERM then KILL after the grace window on Unix, an
// immediate kill on Windows. Both platforms wait for the exit notification.
func (p *Proc) Stop() error {
	p.mu.Lock()
	cmd := p.cmd
	done := p.done
	if !p.running || cmd == nil || done == nil {
		p.mu.Unlock()
		return errProcNotRunning
	}
	// Mark the stop intentional before signalling so the watcher cannot log a
	// crash for it.
	p.stopping = true
	p.mu.Unlock()

	if runtime.GOOS == "windows" {
		_ = cmd.Process.Kill()
		if !awaitDone(done, stopKillTime) {
			return errProcStopTimed
		}
		return nil
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Already gone: only the short window is left to wait for.
		if !awaitDone(done, stopKillTime) {
			return errProcStopTimed
		}
		return nil
	}
	if awaitDone(done, stopGraceTime) {
		logger.Infof("sidecar: %s stopped", p.name)
		return nil
	}
	logger.Warningf("sidecar: %s did not stop after SIGTERM, killing", p.name)
	_ = cmd.Process.Kill()
	if !awaitDone(done, stopKillTime) {
		return errProcStopTimed
	}
	return nil
}

// watch reaps the child, publishes the exit and reports an unexpected exit.
func (p *Proc) watch(cmd *exec.Cmd, done chan struct{}) {
	err := cmd.Wait()
	p.out.flush()

	p.mu.Lock()
	if p.cmd == cmd {
		p.running = false
	}
	stopping := p.stopping
	p.mu.Unlock()
	close(done)

	if stopping {
		return
	}
	cause := "exit status 0"
	if err != nil {
		cause = err.Error()
	}
	logger.Errorf("sidecar: %s process exited: %v", p.name, cause)
}

func (p *Proc) pid() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func awaitDone(done <-chan struct{}, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

// lineBuffer keeps the last lineLimit lines of one child and forwards them to
// the panel log. Bytes arrive in arbitrary chunks and are split on newlines.
type lineBuffer struct {
	name string

	mu      sync.Mutex
	lines   []string
	pending []byte
}

func newLineBuffer(name string) *lineBuffer { return &lineBuffer{name: name} }

func (b *lineBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending = append(b.pending, data...)
	for {
		idx := bytes.IndexByte(b.pending, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimSpace(string(b.pending[:idx]))
		b.pending = b.pending[idx+1:]
		b.storeLocked(line)
	}
	return len(data), nil
}

// flush pushes a non-blank trailing fragment as the final line of the run.
func (b *lineBuffer) flush() {
	b.mu.Lock()
	defer b.mu.Unlock()
	rest := strings.TrimSpace(string(b.pending))
	b.pending = nil
	if rest != "" {
		b.storeLocked(rest)
	}
}

func (b *lineBuffer) storeLocked(line string) {
	if len(b.lines) >= lineLimit {
		b.lines = b.lines[1:]
	}
	b.lines = append(b.lines, line)
	if !forwardLogLine(line) {
		return
	}
	logger.Infof("sidecar[%s]: %s", b.name, line)
}

// forwardLogLine drops the two core log levels the panel never mirrors and
// empty lines, which are retained but not forwarded.
func forwardLogLine(line string) bool {
	if line == "" {
		return false
	}
	return !strings.Contains(line, "] TRACE:") && !strings.Contains(line, "] DEBUG:")
}

func (b *lineBuffer) last() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.lines) == 0 {
		return ""
	}
	return b.lines[len(b.lines)-1]
}

func (b *lineBuffer) snapshot(max int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.lines) == 0 {
		return nil
	}
	start := 0
	if max > 0 && max < len(b.lines) {
		start = len(b.lines) - max
	}
	out := make([]string, len(b.lines)-start)
	copy(out, b.lines[start:])
	return out
}
