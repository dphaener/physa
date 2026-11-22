//go:build linux || darwin

package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dphaener/physa/internal/api"
	"github.com/dphaener/physa/internal/configdir"
)

// Run is the engine: it takes the machine's lock, serves the API on the
// socket, opens the registered repos off the start path, and returns once
// ctx ends, engine.stop is called, or nothing was pending for the idle
// duration. Each of those returns nil. Another engine holding the lock
// returns *RunningError.
func Run(ctx context.Context, o Options) error {
	p, err := PathsIn(o.Dir)
	if err != nil {
		return err
	}
	if err := configdir.Ensure(p.Dir); err != nil {
		return err
	}
	lock, err := takeLock(p.Lock)
	if err != nil {
		return err
	}
	defer releaseLock(lock)
	if err := writePID(lock); err != nil {
		return fmt.Errorf("writing the pid to %s: %w", p.Lock, err)
	}
	// Holding the lock proves no engine is live, so a socket left here is
	// a killed engine's.
	if err := os.Remove(p.Socket); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("clearing the old socket: %w", err)
	}
	ln, err := net.Listen("unix", p.Socket)
	if err != nil {
		return err
	}
	if err := os.Chmod(p.Socket, 0o600); err != nil {
		ln.Close()
		return err
	}

	idle := o.Idle
	if idle <= 0 {
		idle = DefaultIdle
	}
	o.Dir, o.Idle = p.Dir, idle
	e := newEngine(o, time.Now())
	srv := api.NewServer(e.table(), e.hooks())
	hs := &http.Server{Handler: srv, ReadHeaderTimeout: 5 * time.Second}
	served := make(chan error, 1)
	go func() { served <- hs.Serve(ln) }()
	// Stores open after the socket answers; a call naming a repo opens it.
	opening, stopOpening := context.WithTimeout(context.Background(), 30*time.Second)
	defer stopOpening()
	var opened sync.WaitGroup
	opened.Go(func() {
		for _, err := range e.set.OpenRegistered(opening) {
			fmt.Fprintf(os.Stderr, "physa engine: skipping repo %v\n", err)
		}
	})

	why, runErr := e.wait(ctx, idle, served)
	ln.Close() // removes the socket
	if why == idled && e.pending() {
		// A client connected while the engine was going; it is closed
		// going-away below and enginestart starts a new engine.
		fmt.Fprintln(os.Stderr, "physa engine: a client connected while the engine was exiting idle; closing it")
	}
	shut, cancel := context.WithTimeout(context.Background(), time.Second)
	if hs.Shutdown(shut) != nil {
		hs.Close()
	}
	cancel()
	// An error here means some connections were closed without a
	// handshake: the shutdown is still finished.
	closing, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	srv.Close(closing)
	cancel()
	if why != serveFailed {
		<-served
	}
	stopOpening()
	opened.Wait()
	closing, cancel = context.WithTimeout(context.Background(), time.Second)
	if err := e.set.Close(closing); err != nil {
		fmt.Fprintf(os.Stderr, "physa engine: closing the repos: %v\n", err)
	}
	cancel()
	if err := os.Remove(p.Socket); err != nil && !errors.Is(err, fs.ErrNotExist) && runErr == nil {
		runErr = fmt.Errorf("removing the socket: %w", err)
	}
	return runErr
}

// Why the engine stops.
const (
	cancelled = iota
	stopped
	idled
	serveFailed
)

// wait blocks until the engine should stop and says why. Only a failure to
// serve is an error.
func (e *engine) wait(ctx context.Context, idle time.Duration, served <-chan error) (int, error) {
	every := min(idle/10, time.Second)
	if every < 10*time.Millisecond {
		every = 10 * time.Millisecond
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return cancelled, nil
		case <-e.stopped:
			return stopped, nil
		case err := <-served:
			return serveFailed, fmt.Errorf("serving the API: %w", err)
		case <-tick.C:
			if e.idleFor(idle) {
				return idled, nil
			}
		}
	}
}

// takeLock opens the lock file and takes it without waiting. When another
// engine holds it, it returns *RunningError with that engine's pid.
func takeLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err != syscall.EINTR {
			break
		}
	}
	if err == nil {
		return f, nil
	}
	defer f.Close()
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return nil, &RunningError{PID: readPID(path)}
	}
	return nil, fmt.Errorf("taking the lock %s: %w", path, err)
}

// readPID reads the holder's pid, waiting up to 100ms while the file is
// empty: the holder writes it just after taking the lock. 0 means unknown.
func readPID(path string) int {
	deadline := time.Now().Add(100 * time.Millisecond)
	for {
		b, err := os.ReadFile(path)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
				return pid
			}
		}
		if time.Now().After(deadline) {
			return 0
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// writePID replaces the lock file's text with this process's pid.
func writePID(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	_, err := f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	return err
}

// releaseLock unlocks and closes the lock file, leaving it on disk.
func releaseLock(f *os.File) {
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	f.Close()
}
