package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	listenAddr = ":8377"
	agentDir   = "/agent/data"
	sourceDir  = "/agent/src"
	binaryPath = "/agent/brain"
	backupPath = "/agent/brain.backup"
)

var startTime = time.Now()

func main() {
	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("GET /health", handleHealth)

	// File operations
	mux.HandleFunc("POST /tools/read_file", handleReadFile)
	mux.HandleFunc("POST /tools/write_file", handleWriteFile)
	mux.HandleFunc("POST /tools/list_dir", handleListDir)

	// Skills
	mux.HandleFunc("POST /tools/list_skills", handleListSkills)
	mux.HandleFunc("POST /tools/read_skill", handleReadSkill)
	mux.HandleFunc("POST /tools/write_skill", handleWriteSkill)
	mux.HandleFunc("POST /tools/delete_skill", handleDeleteSkill)

	// Prompt
	mux.HandleFunc("POST /tools/get_prompt", handleGetPrompt)
	mux.HandleFunc("POST /tools/set_prompt", handleSetPrompt)

	// Memory
	mux.HandleFunc("POST /tools/read_memory", handleReadMemory)
	mux.HandleFunc("POST /tools/update_memory", handleUpdateMemory)

	// Conversation
	mux.HandleFunc("POST /tools/save_conversation", handleSaveConversation)
	mux.HandleFunc("POST /tools/get_conversation_summary", handleGetConversationSummary)
	mux.HandleFunc("POST /tools/update_summary", handleUpdateSummary)

	// Execution
	mux.HandleFunc("POST /tools/run_bash", handleRunBash)

	// Self-modification
	mux.HandleFunc("POST /tools/read_source", handleReadSource)
	mux.HandleFunc("POST /tools/write_source", handleWriteSource)
	mux.HandleFunc("POST /tools/rebuild_self", handleRebuildSelf)
	mux.HandleFunc("POST /tools/read_dockerfile", handleReadDockerfile)
	mux.HandleFunc("POST /tools/write_dockerfile", handleWriteDockerfile)

	server := &http.Server{Addr: listenAddr, Handler: mux}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		<-sigCh
		log.Println("Shutting down...")
		server.Close()
	}()

	log.Printf("Brain server listening on %s", listenAddr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"status":         "ok",
		"uptime_seconds": int(time.Since(startTime).Seconds()),
	})
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// decodeBody decodes a JSON request body into dst.
func decodeBody(r *http.Request, dst any) error {
	return json.NewDecoder(r.Body).Decode(dst)
}
