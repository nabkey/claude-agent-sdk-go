package main

import (
	"bytes"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func handleReadSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		File string `json:"file"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeError(w, 400, "invalid request: "+err.Error())
		return
	}
	path, err := safePath(sourceDir, req.File)
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeError(w, 404, "source file not found: "+err.Error())
		return
	}
	writeJSON(w, map[string]string{"content": string(data)})
}

func handleWriteSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		File    string `json:"file"`
		Content string `json:"content"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeError(w, 400, "invalid request: "+err.Error())
		return
	}
	path, err := safePath(sourceDir, req.File)
	if err != nil {
		writeError(w, 403, err.Error())
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		writeError(w, 500, "failed to create directory: "+err.Error())
		return
	}
	if err := os.WriteFile(path, []byte(req.Content), 0644); err != nil {
		writeError(w, 500, "failed to write source: "+err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func handleRebuildSelf(w http.ResponseWriter, r *http.Request) {
	// 1. Backup current binary
	if err := copyFile(binaryPath, backupPath); err != nil {
		writeError(w, 500, "failed to backup current binary: "+err.Error())
		return
	}

	// 2. List source files for context
	files, _ := walkFiles(sourceDir)
	sourceFiles := strings.Join(files, ", ")

	// 3. Build new binary
	cmd := exec.Command("go", "build", "-o", binaryPath+".new", ".")
	cmd.Dir = sourceDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		writeJSON(w, map[string]any{
			"ok":           false,
			"error":        "compilation failed",
			"compiler_out": stderr.String() + stdout.String(),
			"source_files": sourceFiles,
		})
		return
	}

	// 4. Build succeeded — report success then exit so entrypoint.sh swaps the binary
	writeJSON(w, map[string]any{
		"ok":           true,
		"output":       "build succeeded, restarting with new binary...",
		"source_files": sourceFiles,
	})

	// Flush the response before exiting
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Give the response time to be sent, then exit
	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()
}

func handleReadDockerfile(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(filepath.Join(sourceDir, "Dockerfile"))
	if err != nil {
		writeError(w, 404, "Dockerfile not found: "+err.Error())
		return
	}
	writeJSON(w, map[string]string{"content": string(data)})
}

func handleWriteDockerfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeError(w, 400, "invalid request: "+err.Error())
		return
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "Dockerfile"), []byte(req.Content), 0644); err != nil {
		writeError(w, 500, "failed to write Dockerfile: "+err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// copyFile copies src to dst.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0755)
}
