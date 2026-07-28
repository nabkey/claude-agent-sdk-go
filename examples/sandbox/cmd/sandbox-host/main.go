// Command sandbox-host runs beside the Claude Code CLI inside a sandbox and
// serves SDK sessions over a socket.
//
// Run this in the container, VM, or remote box that should execute the agent's
// tool calls. The process driving the conversation — a chat bot, a web service
// — connects with sandbox.Transport and never needs the CLI locally.
//
// A unix socket is the safest default when both halves share a machine:
//
//	sandbox-host -network unix -listen /run/claude-sandbox.sock -cwd /work
//
// Across a network, set a token and terminate TLS (or tunnel it):
//
//	sandbox-host -listen :8377 -token "$SANDBOX_TOKEN" -cwd /work \
//	    -tls-cert /etc/tls/host.pem -tls-key /etc/tls/host.key \
//	    -permission-mode plan -bash-sandbox
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/nabkey/claude-agent-sdk-go/examples/sandbox"
)

// repeatedFlag collects a flag given more than once.
type repeatedFlag []string

func (r *repeatedFlag) String() string { return strings.Join(*r, ",") }

func (r *repeatedFlag) Set(v string) error {
	*r = append(*r, v)
	return nil
}

func main() {
	var (
		network   = flag.String("network", "tcp", `listen network: "tcp" or "unix"`)
		listen    = flag.String("listen", "127.0.0.1:8377", "listen address, or socket path for -network unix")
		tlsCert   = flag.String("tls-cert", "", "TLS certificate (enables TLS)")
		tlsKey    = flag.String("tls-key", "", "TLS private key")
		cliPath   = flag.String("cli", "", "path to the claude binary (default: resolve on PATH)")
		cwd       = flag.String("cwd", ".", "working directory for the agent")
		permMode  = flag.String("permission-mode", "", "CLI --permission-mode")
		model     = flag.String("model", "", "pin the model")
		allowed   = flag.String("allowed-tools", "", "comma-separated tool allowlist")
		disallow  = flag.String("disallowed-tools", "", "comma-separated tool denylist")
		sources   = flag.String("setting-sources", "\x00", "CLI --setting-sources; omit the flag to load all")
		settings  = flag.String("settings", "", "raw JSON settings blob")
		bashBox   = flag.Bool("bash-sandbox", false, `shorthand for -settings '{"sandbox":{"enabled":true}}'`)
		maxTurns  = flag.Int("max-turns", 0, "cap agent turns per session")
		resume    = flag.Bool("allow-resume", false, "let clients resume sessions by ID")
		cliPrompt = flag.Bool("cli-system-prompt", false,
			"use the CLI's stock system prompt instead of blanking it as the SDK does")
		addDirs repeatedFlag
	)
	flag.Var(&addDirs, "add-dir", "extra directory the agent may reach (repeatable)")
	flag.Parse()

	token := os.Getenv("SANDBOX_TOKEN")
	if token == "" && *network == "tcp" {
		log.Println("warning: SANDBOX_TOKEN is unset; this host accepts any client")
	}

	policy := sandbox.DefaultPolicy(*cwd)
	policy.CLIPath = *cliPath
	policy.PermissionMode = *permMode
	policy.Model = *model
	policy.AddDirs = addDirs
	policy.MaxTurns = *maxTurns
	policy.AllowResume = *resume
	policy.BlankSystemPrompt = !*cliPrompt
	policy.Settings = *settings
	if *bashBox && policy.Settings == "" {
		policy.Settings = `{"sandbox":{"enabled":true}}`
	}
	if *allowed != "" {
		policy.AllowedTools = strings.Split(*allowed, ",")
	}
	if *disallow != "" {
		policy.DisallowedTools = strings.Split(*disallow, ",")
	}
	// The sentinel distinguishes "flag absent" (load every source) from an
	// explicit empty value (load none) — the two mean different things.
	if *sources != "\x00" {
		policy.SettingSources = sources
	}

	host := &sandbox.Host{
		Policy: policy,
		Token:  token,
		Log:    func(format string, args ...any) { log.Printf(format, args...) },
	}

	ln, err := listenOn(*network, *listen, *tlsCert, *tlsKey)
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("sandbox host listening on %s/%s (cwd %s)", *network, *listen, *cwd)
	if err := host.Serve(ctx, ln); err != nil {
		log.Fatal(err)
	}
	log.Println("shutting down")
}

func listenOn(network, addr, certFile, keyFile string) (net.Listener, error) {
	if network == "unix" {
		// A stale socket from an unclean exit would otherwise block binding.
		if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("clear stale socket %s: %w", addr, err)
		}
	}

	ln, err := net.Listen(network, addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s/%s: %w", network, addr, err)
	}

	if network == "unix" {
		// Owner-only: with no token, the socket mode is the access control.
		if err := os.Chmod(addr, 0600); err != nil {
			ln.Close()
			return nil, fmt.Errorf("restrict socket permissions: %w", err)
		}
	}

	if certFile == "" && keyFile == "" {
		return ln, nil
	}
	if certFile == "" || keyFile == "" {
		ln.Close()
		return nil, fmt.Errorf("-tls-cert and -tls-key must be set together")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		ln.Close()
		return nil, fmt.Errorf("load TLS keypair: %w", err)
	}
	return tls.NewListener(ln, &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}), nil
}
