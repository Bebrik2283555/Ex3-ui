package extra

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

const (
	gracefulStopTimeout = 5 * time.Second
	forceStopTimeout    = 2 * time.Second
	maxLogLines         = 500
)

// ring is a bounded, concurrency-safe line buffer.
type ring struct {
	mu   sync.Mutex
	rows []string
}

func (r *ring) push(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.rows) >= maxLogLines {
		copy(r.rows, r.rows[len(r.rows)-maxLogLines+1:])
		r.rows = r.rows[:maxLogLines-1]
	}
	r.rows = append(r.rows, line)
}

func (r *ring) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.rows) == 0 {
		return ""
	}
	return r.rows[len(r.rows)-1]
}

func (r *ring) all(max int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if max <= 0 || len(r.rows) <= max {
		out := make([]string, len(r.rows))
		copy(out, r.rows)
		return out
	}
	out := make([]string, max)
	copy(out, r.rows[len(r.rows)-max:])
	return out
}

// logWriter funnels the child's stdout/stderr into the ring buffer.
type logWriter struct {
	ring *ring
	buf  string
	mu   sync.Mutex
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf += string(p)
	for {
		i := strings.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.ring.push(strings.TrimRight(strings.TrimSpace(w.buf[:i]), "\r"))
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

func (w *logWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if trimmed := strings.TrimSpace(w.buf); trimmed != "" {
		w.ring.push(trimmed)
		w.buf = ""
	}
}

// Proc supervises one extra-core process.
type Proc struct {
	name Name

	mu    sync.RWMutex
	cmd   *exec.Cmd
	done  chan struct{}
	log   *logWriter
	lines *ring

	exitErr         error
	intentionalStop atomic.Bool
}

// NewProc returns an idle supervised process for the given core.
func NewProc(name Name) *Proc {
	lines := &ring{}
	return &Proc{
		name:  name,
		log:   &logWriter{ring: lines},
		lines: lines,
	}
}

// IsRunning reports whether the process is currently alive.
func (p *Proc) IsRunning() bool {
	p.mu.RLock()
	cmd, done := p.cmd, p.done
	p.mu.RUnlock()
	if cmd == nil || cmd.Process == nil {
		return false
	}
	if done != nil {
		select {
		case <-done:
			return false
		default:
		}
	}
	return true
}

// LastLine returns the most recent captured log line.
func (p *Proc) LastLine() string { return p.lines.last() }

// Lines returns up to max recent captured log lines.
func (p *Proc) Lines(max int) []string { return p.lines.all(max) }

// Signal delivers a Unix signal to the running process.
func (p *Proc) Signal(sig syscall.Signal) error {
	p.mu.RLock()
	cmd := p.cmd
	p.mu.RUnlock()
	if cmd == nil || cmd.Process == nil {
		return errors.New("service is not running")
	}
	if err := cmd.Process.Signal(sig); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return errors.New("service is not running")
		}
		return err
	}
	return nil
}

// Start launches the binary with the given arguments.
func (p *Proc) Start(bin string, args []string) error {
	if p.IsRunning() {
		return errors.New("service is already running")
	}
	cmd := exec.CommandContext(context.Background(), bin, args...)
	cmd.Stdout = p.log
	cmd.Stderr = p.log
	done := make(chan struct{})
	p.mu.Lock()
	p.cmd = cmd
	p.done = done
	p.exitErr = nil
	p.mu.Unlock()
	p.intentionalStop.Store(false)
	if err := cmd.Start(); err != nil {
		close(done)
		p.mu.Lock()
		p.cmd = nil
		p.mu.Unlock()
		return err
	}
	logger.Infof("extra: %s started (pid %d)", p.name, cmd.Process.Pid)
	go p.wait(cmd, done)
	return nil
}

func (p *Proc) wait(cmd *exec.Cmd, done chan struct{}) {
	defer close(done)
	err := cmd.Wait()
	p.log.flush()
	if err == nil || p.intentionalStop.Load() {
		return
	}
	if runtime.GOOS == "windows" && strings.Contains(strings.ToLower(err.Error()), "exit status 1") {
		p.setExitErr(err)
		return
	}
	logger.Errorf("extra: %s process exited: %v", p.name, err)
	p.setExitErr(err)
}

func (p *Proc) setExitErr(err error) {
	p.mu.Lock()
	p.exitErr = err
	p.mu.Unlock()
}

// Stop terminates the process gracefully, falling back to a kill.
func (p *Proc) Stop() error {
	if !p.IsRunning() {
		return errors.New("service is not running")
	}
	p.intentionalStop.Store(true)
	p.mu.RLock()
	cmd, done := p.cmd, p.done
	p.mu.RUnlock()
	if cmd == nil || cmd.Process == nil {
		return errors.New("service is not running")
	}

	if runtime.GOOS == "windows" {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		return waitForExit(done, forceStopTimeout)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return waitForExit(done, forceStopTimeout)
		}
		return err
	}
	if err := waitForExit(done, gracefulStopTimeout); err == nil {
		logger.Infof("extra: %s stopped", p.name)
		return nil
	}

	logger.Warningf("extra: %s did not stop after SIGTERM, killing", p.name)
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return waitForExit(done, forceStopTimeout)
}

func waitForExit(done <-chan struct{}, timeout time.Duration) error {
	if done == nil {
		return nil
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return errors.New("timed out waiting for process to stop")
	}
}
