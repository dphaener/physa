//go:build linux || darwin

package enginestart

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/dphaener/physa/internal/api"
	"github.com/dphaener/physa/internal/configdir"
	"github.com/dphaener/physa/internal/engine"
)

// Start starts the engine on o.Dir unless one already answers, and returns
// once an engine answers engine.status. started is true only when the
// engine that answers is a child this call spawned. Two Starts at once
// leave one engine: the lock decides, and a child that lost it is spawned
// again, 100ms apart, until an engine answers or 5s pass.
func Start(ctx context.Context, o Options) (started bool, err error) {
	p, err := engine.PathsIn(o.Dir)
	if err != nil {
		return false, err
	}
	if _, ok := answering(ctx, p.Socket, callTimeout); ok {
		return false, nil
	}
	command := o.Command
	if command == nil {
		exe, err := os.Executable()
		if err != nil {
			return false, fmt.Errorf("finding the physa executable: %w", err)
		}
		command = func(dir string, idle time.Duration) *exec.Cmd {
			return exec.Command(exe, "engine", "run", "--dir", dir, "--idle", idle.String())
		}
	}
	idle := o.Idle
	if idle <= 0 {
		idle = engine.DefaultIdle
	}
	if err := configdir.Ensure(p.Dir); err != nil {
		return false, err
	}
	log, err := os.OpenFile(p.Log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return false, err
	}
	defer log.Close()

	spawned := map[int]bool{}
	var exited <-chan int
	spawn := func() error {
		cmd := command(p.Dir, idle)
		if cmd.Dir == "" {
			cmd.Dir = p.Dir // so the engine does not pin the caller's directory
		}
		cmd.Stdin = nil
		cmd.Stdout, cmd.Stderr = log, log
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("starting the engine: %w", err)
		}
		spawned[cmd.Process.Pid] = true
		// Reaped here, so a child that loses the lock race never stays a
		// zombie of this process.
		ch := make(chan int, 1)
		go func() {
			cmd.Wait()
			ch <- cmd.ProcessState.ExitCode()
		}()
		exited = ch
		return nil
	}
	if err := spawn(); err != nil {
		return false, err
	}
	deadline := time.Now().Add(startWait)
	for {
		if s, ok := answering(ctx, p.Socket, pollTimeout); ok {
			return spawned[s.PID], nil
		}
		select {
		case code := <-exited:
			if code != alreadyRunning {
				return false, failed(p.Log, code)
			}
			// It lost the lock to an engine that was going or had just
			// been killed: try again.
			exited = nil
			if err := pause(ctx, respawnEvery); err != nil {
				return false, err
			}
			if time.Now().Before(deadline) {
				if err := spawn(); err != nil {
					return false, err
				}
			}
		default:
		}
		if time.Now().After(deadline) {
			return false, fmt.Errorf("the engine did not answer within %s; see %s", startWait, p.Log)
		}
		if err := pause(ctx, pollEvery); err != nil {
			return false, err
		}
	}
}

// Stop stops the engine on dir and waits until it is gone. stopped is
// false, with no error, when no engine answers.
func Stop(ctx context.Context, dir string) (stopped bool, err error) {
	p, err := engine.PathsIn(dir)
	if err != nil {
		return false, err
	}
	c, s, err := probe(ctx, p.Socket, callTimeout)
	if err != nil {
		return false, ctx.Err()
	}
	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	callErr := c.Call(cctx, "engine.stop", nil, nil)
	cancel()
	c.Close()
	var apiErr *api.Error
	if errors.As(callErr, &apiErr) {
		return false, callErr
	}
	// Any other error is the connection closing, which an engine that
	// is stopping does: whether it stopped is decided by its going.
	deadline := time.Now().Add(stopWait)
	for !gone(ctx, p.Socket, s) {
		if time.Now().After(deadline) {
			if callErr != nil {
				return false, callErr
			}
			return false, fmt.Errorf("the engine (pid %d) did not stop within %s", s.PID, stopWait)
		}
		if err := pause(ctx, pollEvery); err != nil {
			return false, err
		}
	}
	return true, nil
}

// gone reports whether the engine whose status was s has exited and its
// socket no longer answers as it. An engine in this same process, as in
// command tests, is gone once its socket stops answering.
func gone(ctx context.Context, socket string, s engine.Status) bool {
	if s.PID != os.Getpid() {
		err := syscall.Kill(s.PID, 0)
		if err != syscall.ESRCH && err != syscall.EPERM {
			return false
		}
	}
	now, ok := answering(ctx, socket, pollTimeout)
	return !ok || now.PID != s.PID || now.StartedAt != s.StartedAt
}

// failed is Start's error for a child that exited with code, quoting the
// log's last line.
func failed(log string, code int) error {
	last := lastLine(log)
	if last == "" {
		return fmt.Errorf("the engine exited with code %d; see %s", code, log)
	}
	return fmt.Errorf("the engine exited with code %d: %q; see %s", code, last, log)
}

// lastLine returns the last non-empty line of the file's final 4 KiB.
func lastLine(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() > 4096 {
		f.Seek(fi.Size()-4096, io.SeekStart)
	}
	b, _ := io.ReadAll(io.LimitReader(f, 4096))
	b = bytes.TrimRight(b, " \t\r\n")
	if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
		b = b[i+1:]
	}
	return string(bytes.TrimSpace(b))
}

// pause waits for d or until ctx ends.
func pause(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
