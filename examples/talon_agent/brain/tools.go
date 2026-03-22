package main

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// safePath resolves a path and ensures it's under the allowed root.
func safePath(root, requested string) (string, error) {
	cleaned := filepath.Clean(requested)
	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Join(root, cleaned)
	}
	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(abs, rootAbs+string(filepath.Separator)) && abs != rootAbs {
		return "", fmt.Errorf("path %q is outside allowed directory", requested)
	}
	return abs, nil
}

func handleReadFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeError(w, 400, "invalid request: "+err.Error())
		return
	}
	path, err := safePath("/agent", req.Path)
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeError(w, 404, "file not found: "+err.Error())
		return
	}
	writeJSON(w, map[string]string{"content": string(data)})
}

func handleWriteFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeError(w, 400, "invalid request: "+err.Error())
		return
	}
	path, err := safePath("/agent", req.Path)
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		writeError(w, 500, "failed to create directory: "+err.Error())
		return
	}
	if err := os.WriteFile(path, []byte(req.Content), 0644); err != nil {
		writeError(w, 500, "failed to write file: "+err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func handleListDir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeError(w, 400, "invalid request: "+err.Error())
		return
	}
	path, err := safePath("/agent", req.Path)
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		writeError(w, 404, "directory not found: "+err.Error())
		return
	}
	type entry struct {
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
		Size  int64  `json:"size"`
	}
	result := make([]entry, 0, len(entries))
	for _, e := range entries {
		info, _ := e.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		result = append(result, entry{
			Name:  e.Name(),
			IsDir: e.IsDir(),
			Size:  size,
		})
	}
	writeJSON(w, map[string]any{"entries": result})
}

func handleRunBash(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeError(w, 400, "invalid request: "+err.Error())
		return
	}

	timeout := 30 * time.Second
	if req.TimeoutSeconds > 0 {
		timeout = time.Duration(req.TimeoutSeconds) * time.Second
	}
	if timeout > 120*time.Second {
		timeout = 120 * time.Second
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", req.Command)
	cmd.Dir = agentDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			writeError(w, 500, "exec failed: "+err.Error())
			return
		}
	}

	writeJSON(w, map[string]any{
		"stdout":    stdout.String(),
		"stderr":    stderr.String(),
		"exit_code": exitCode,
	})
}

// walkFiles collects file paths under a directory, used by other handlers.
func walkFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			files = append(files, rel)
		}
		return nil
	})
	return files, err
}
