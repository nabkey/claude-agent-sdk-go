package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ContainerManager manages the talon-brain Podman container lifecycle.
//
// On first run, it bootstraps by copying the brain/ template from the repo
// into ~/.talon/src/. All builds happen from that copy, so the agent's
// self-modifications never touch the git repo.
type ContainerManager struct {
	imageName     string
	containerName string
	port          int
	templateDir   string // repo's brain/ directory (read-only template)
	srcDir        string // ~/.talon/src/ (mutable copy, used for builds)
	dataDir       string // ~/.talon/data/ (agent state: skills, memory, etc.)
}

// NewContainerManager creates a new container manager.
func NewContainerManager(templateDir string) *ContainerManager {
	homeDir, _ := os.UserHomeDir()
	talonHome := filepath.Join(homeDir, ".talon")
	return &ContainerManager{
		imageName:     "talon-brain",
		containerName: "talon-brain",
		port:          8377,
		templateDir:   templateDir,
		srcDir:        filepath.Join(talonHome, "src"),
		dataDir:       filepath.Join(talonHome, "data"),
	}
}

// BrainURL returns the HTTP URL for the brain server.
func (cm *ContainerManager) BrainURL() string {
	return fmt.Sprintf("http://localhost:%d", cm.port)
}

// EnsureRunning makes sure the container is built, started, and healthy.
func (cm *ContainerManager) EnsureRunning(ctx context.Context) error {
	// Ensure directories exist
	if err := os.MkdirAll(cm.dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Bootstrap: copy brain template to ~/.talon/src/ if not present
	if err := cm.bootstrap(); err != nil {
		return fmt.Errorf("bootstrap failed: %w", err)
	}

	// Check if container is already running and healthy
	if cm.IsRunning(ctx) {
		if err := cm.healthCheck(ctx); err == nil {
			log.Println("Brain container already running and healthy.")
			return nil
		}
	}

	// Stop any existing container
	cm.Stop(ctx)

	// Build image if needed
	if !cm.imageExists(ctx) {
		log.Println("Building brain image...")
		if err := cm.Build(ctx); err != nil {
			return err
		}
	}

	// Start container
	log.Println("Starting brain container...")
	if err := cm.Start(ctx); err != nil {
		return err
	}

	// Wait for health
	return cm.WaitForHealth(ctx, 30*time.Second)
}

// bootstrap copies the brain template from the repo to ~/.talon/src/ on first run.
// If ~/.talon/src/ already exists, it's left alone — the agent owns it.
func (cm *ContainerManager) bootstrap() error {
	if _, err := os.Stat(filepath.Join(cm.srcDir, "go.mod")); err == nil {
		// Already bootstrapped
		return nil
	}

	log.Printf("Bootstrapping brain source from %s to %s...", cm.templateDir, cm.srcDir)

	return filepath.WalkDir(cm.templateDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(cm.templateDir, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(cm.srcDir, rel)

		if d.IsDir() {
			return os.MkdirAll(dest, 0755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		// Preserve executable bit for scripts
		perm := os.FileMode(0644)
		if info, err := d.Info(); err == nil && info.Mode()&0111 != 0 {
			perm = 0755
		}

		return os.WriteFile(dest, data, perm)
	})
}

// Build builds the Podman image from the bootstrapped source directory.
func (cm *ContainerManager) Build(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "podman", "build", "-t", cm.imageName, ".")
	cmd.Dir = cm.srcDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("podman build failed: %w", err)
	}
	return nil
}

// Start starts the container.
func (cm *ContainerManager) Start(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "podman", "run", "-d",
		"--name", cm.containerName,
		"-p", fmt.Sprintf("%d:8377", cm.port),
		"-v", cm.dataDir+":/agent/data",
		cm.imageName,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman run failed: %w (output: %s)", err, string(output))
	}
	return nil
}

// Stop stops and removes the container.
func (cm *ContainerManager) Stop(ctx context.Context) {
	exec.CommandContext(ctx, "podman", "rm", "-f", cm.containerName).Run()
}

// IsRunning checks if the container is running.
func (cm *ContainerManager) IsRunning(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "podman", "inspect", "-f", "{{.State.Running}}", cm.containerName)
	output, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(output)) == "true"
}

// WaitForHealth polls the brain's health endpoint until it responds or times out.
func (cm *ContainerManager) WaitForHealth(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	interval := 500 * time.Millisecond

	for time.Now().Before(deadline) {
		if err := cm.healthCheck(ctx); err == nil {
			log.Println("Brain is healthy.")
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
		if interval < 2*time.Second {
			interval = interval * 2
		}
	}
	return fmt.Errorf("brain did not become healthy within %v", timeout)
}

// RestoreBackup restores the brain binary from backup inside the container.
func (cm *ContainerManager) RestoreBackup(ctx context.Context) error {
	log.Println("Restoring brain from backup...")
	cmd := exec.CommandContext(ctx, "podman", "exec", cm.containerName,
		"sh", "-c", "cp /agent/brain.backup /agent/brain && kill 1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restore failed: %w (output: %s)", err, string(output))
	}
	return nil
}

// RebuildImage rebuilds the Podman image after Dockerfile changes.
// Copies the updated Dockerfile from the container to ~/.talon/src/ (not the repo),
// then rebuilds the image from there.
func (cm *ContainerManager) RebuildImage(ctx context.Context) error {
	log.Println("Rebuilding brain image...")

	// Copy updated Dockerfile from container to the mutable source dir
	cmd := exec.CommandContext(ctx, "podman", "cp",
		cm.containerName+":/agent/src/Dockerfile",
		filepath.Join(cm.srcDir, "Dockerfile"))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to copy Dockerfile from container: %w", err)
	}

	// Tag current image as backup
	exec.CommandContext(ctx, "podman", "tag", cm.imageName, cm.imageName+":backup").Run()

	// Stop container
	cm.Stop(ctx)

	// Build new image
	if err := cm.Build(ctx); err != nil {
		log.Println("Build failed, restoring backup image...")
		exec.CommandContext(ctx, "podman", "tag", cm.imageName+":backup", cm.imageName).Run()
		cm.Start(ctx)
		cm.WaitForHealth(ctx, 30*time.Second)
		return fmt.Errorf("image rebuild failed: %w", err)
	}

	// Start new container
	if err := cm.Start(ctx); err != nil {
		return err
	}

	return cm.WaitForHealth(ctx, 30*time.Second)
}

// WatchAndRecover monitors brain health and restores from backup if needed.
func (cm *ContainerManager) WatchAndRecover(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	failCount := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := cm.healthCheck(ctx); err != nil {
				failCount++
				log.Printf("Brain health check failed (%d): %v", failCount, err)
				if failCount >= 3 {
					log.Println("Brain unresponsive, attempting recovery...")
					if err := cm.RestoreBackup(ctx); err != nil {
						log.Printf("Recovery failed: %v", err)
						cm.Stop(ctx)
						cm.Start(ctx)
					}
					cm.WaitForHealth(ctx, 30*time.Second)
					failCount = 0
				}
			} else {
				failCount = 0
			}
		}
	}
}

// Reset deletes ~/.talon/src/ so the next run re-bootstraps from the repo template.
// Useful for resetting to a clean state.
func (cm *ContainerManager) Reset(ctx context.Context) error {
	cm.Stop(ctx)
	log.Println("Removing bootstrapped source...")
	return os.RemoveAll(cm.srcDir)
}

func (cm *ContainerManager) healthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", cm.BrainURL()+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("health check returned %d", resp.StatusCode)
	}
	return nil
}

func (cm *ContainerManager) imageExists(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "podman", "image", "inspect", cm.imageName)
	return cmd.Run() == nil
}
