package transport

import (
	"context"
	"os/exec"
	"testing"
)

// Close nils cmd, stdin, stdout and stderr, and the read loop outlives it: a
// caller that stops reading mid-conversation closes the transport while the
// loop is still draining stdout. The loop must therefore never read those
// fields once it has started.
//
// It used to check `t.cmd != nil` and then dereference `t.cmd` again a line
// later, so a Close landing in between produced:
//
//	panic: runtime error: invalid memory address or nil pointer dereference
//	SubprocessTransport.ReadMessages.func1 subprocess.go:928
//
// Under -race the unsynchronized access is reported even when the interleaving
// does not hit the crash, which is what makes this reliable rather than a
// one-in-many-runs reproduction.
func TestReadMessagesSurvivesConcurrentClose(t *testing.T) {
	for i := 0; i < 40; i++ {
		tr, err := startEchoTransport()
		if err != nil {
			t.Fatalf("starting the fake CLI: %v", err)
		}

		msgs, errs := tr.ReadMessages(context.Background())

		// Close concurrently, as a caller abandoning the stream does.
		closed := make(chan struct{})
		go func() {
			_ = tr.Close()
			close(closed)
		}()

		for range msgs {
		}
		for range errs {
		}
		<-closed
	}
}

// A transport closed before anything reads must not panic either: the read
// loop then finds every field already nil.
func TestReadMessagesAfterClose(t *testing.T) {
	tr, err := startEchoTransport()
	if err != nil {
		t.Fatalf("starting the fake CLI: %v", err)
	}
	_ = tr.Close()

	msgs, errs := tr.ReadMessages(context.Background())
	for range msgs {
	}
	for range errs {
	}
}

// startEchoTransport runs a stand-in for the CLI that writes one message and
// exits, which is enough to drive the read loop to its process-reaping tail.
func startEchoTransport() (*SubprocessTransport, error) {
	cmd := exec.Command("sh", "-c", `printf '{"type":"system","subtype":"init"}\n'`)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &SubprocessTransport{
		options:       &SubprocessOptions{},
		cmd:           cmd,
		stdout:        stdout,
		maxBufferSize: 1 << 20,
	}, nil
}
