package transport

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// Live subprocesses, so they can be terminated if the parent exits without
// calling Close. Without this, a caller that crashes or exits early leaks
// orphaned `claude` processes. Both reference SDKs do the same via atexit /
// process.on('exit').
//
// Go has no atexit, so this covers the reachable cases: a fatal signal
// (SIGINT/SIGTERM) is intercepted, children are reaped, and the default
// disposition is then restored so the signal is not swallowed. A hard
// os.Exit or SIGKILL cannot be intercepted by any mechanism.
var (
	childrenMu sync.Mutex
	children   = map[*SubprocessTransport]struct{}{}

	reaperOnce sync.Once
)

func registerChild(t *SubprocessTransport) {
	reaperOnce.Do(startSignalReaper)

	childrenMu.Lock()
	defer childrenMu.Unlock()
	children[t] = struct{}{}
}

func unregisterChild(t *SubprocessTransport) {
	childrenMu.Lock()
	defer childrenMu.Unlock()
	delete(children, t)
}

// startSignalReaper installs a handler that terminates live children on a
// fatal signal, then re-raises it with the default disposition so the process
// still dies with the expected status.
func startSignalReaper() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	go func() {
		received := <-ch
		reapChildren()

		// Restore the default disposition and re-raise, so the process still
		// dies with the status the signal implies rather than swallowing it.
		signal.Stop(ch)
		sig, ok := received.(syscall.Signal)
		if !ok {
			return
		}
		signal.Reset(sig)
		_ = syscall.Kill(syscall.Getpid(), sig)
	}()
}

// reapChildren sends SIGTERM to every live child. Best effort: this runs on a
// signal path, so it does not wait for exits.
func reapChildren() {
	childrenMu.Lock()
	live := make([]*SubprocessTransport, 0, len(children))
	for t := range children {
		live = append(live, t)
	}
	children = map[*SubprocessTransport]struct{}{}
	childrenMu.Unlock()

	for _, t := range live {
		if t.cmd != nil && t.cmd.Process != nil {
			_ = t.cmd.Process.Signal(syscall.SIGTERM)
		}
	}
}
