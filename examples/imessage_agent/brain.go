package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

// The brain is an HTTP server that gives the agent tools for modifying its own
// skills, memory, prompt, and source. It runs wherever the operator puts it —
// most naturally inside the same sandbox as the CLI, so the agent's edits and
// its rebuilds land in the same place.
//
// This file used to build and supervise a Podman container. That job now
// belongs outside the program: the operator starts the sandbox host and the
// brain, and this process just checks it can reach them. What remains is the
// health check, which is worth keeping because a brain that is merely slow to
// start is otherwise indistinguishable from a misconfigured URL.

// waitForBrain blocks until the brain answers a health check or timeout passes.
func waitForBrain(ctx context.Context, brainURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	interval := 250 * time.Millisecond

	var lastErr error
	for time.Now().Before(deadline) {
		if err := brainHealthy(ctx, brainURL); err == nil {
			return nil
		} else {
			lastErr = err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
		// Back off so a brain that is down does not spin the CPU while the
		// operator reads the error.
		if interval < 2*time.Second {
			interval *= 2
		}
	}
	return fmt.Errorf("brain at %s did not become healthy within %s: %w", brainURL, timeout, lastErr)
}

// brainHealthy reports whether the brain answers its health endpoint.
func brainHealthy(ctx context.Context, brainURL string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, brainURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned %d", resp.StatusCode)
	}
	return nil
}

// watchBrain logs when the brain becomes unreachable and when it recovers.
//
// It no longer restarts anything: the process that owns the brain's lifecycle
// owns restarting it. Reporting the transition is still useful, because the
// agent's self-modification tools fail in confusing ways when the brain is
// down and nothing else says so.
func watchBrain(ctx context.Context, brainURL string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	healthy := true
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := brainHealthy(ctx, brainURL)
			switch {
			case err != nil && healthy:
				healthy = false
				log.Printf("brain at %s is unreachable: %v", brainURL, err)
			case err == nil && !healthy:
				healthy = true
				log.Printf("brain at %s recovered", brainURL)
			}
		}
	}
}
