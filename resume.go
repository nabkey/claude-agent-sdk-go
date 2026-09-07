package claude

// Materializing a SessionStore-backed resume into a temporary
// CLAUDE_CONFIG_DIR.
//
// When Resume (or ContinueConversation) is paired with a SessionStore, the
// session's JSONL almost certainly does not exist on local disk -- it lives in
// the external store. The CLI subprocess only knows how to resume from a local
// file. This bridges the gap: the session is loaded from the store, written to
// a temporary directory laid out exactly like ~/.claude/, and the subprocess is
// pointed at it through CLAUDE_CONFIG_DIR.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/nabkey/claude-agent-sdk-go/sessions"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// defaultLoadTimeout bounds each SessionStore call made during resume
// materialization.
const defaultLoadTimeout = 60 * time.Second

// keychainServiceName is the macOS Keychain service holding OAuth credentials
// when CLAUDE_CONFIG_DIR is unset.
const keychainServiceName = "Claude Code-credentials"

// resumeSettingsStrippedKeys are user-settings keys that only misbehave under
// a redirected CLAUDE_CONFIG_DIR: plugin declarations reconcile against the
// always-empty temp plugin cache and would network-install each declared
// marketplace on every resume.
var resumeSettingsStrippedKeys = []string{"enabledPlugins", "extraKnownMarketplaces"}

// uuidPattern matches a canonical UUID. Session IDs become path components, so
// anything else is refused rather than sanitized.
var uuidPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// materializedResume is the temporary config directory a store-backed resume
// runs against.
type materializedResume struct {
	// configDir is laid out like ~/.claude/. The subprocess is pointed at it
	// through CLAUDE_CONFIG_DIR.
	configDir string
	// sessionID is what to pass as --resume. For ContinueConversation this is
	// the most recent session resolved from the store.
	sessionID string
}

// cleanup removes the temporary directory. Best effort: it holds a copy of
// the caller's credentials, so a failure is retried before giving up.
func (m *materializedResume) cleanup() {
	if m == nil || m.configDir == "" {
		return
	}
	removeWithRetry(m.configDir)
}

// materializeResumeSession loads a session from the store and writes it to a
// temporary config directory.
//
// It returns nil when no materialization is needed -- no store, no
// resume/continue, an empty store, or a session ID that is not a UUID -- and
// the caller then falls through to the ordinary resume path: a fresh session
// for ContinueConversation, or the CLI receiving an explicit Resume unchanged.
func materializeResumeSession(ctx context.Context, o *AgentOptions) (*materializedResume, error) {
	if o.SessionStore == nil {
		return nil, nil
	}
	if o.Resume == nil && !o.ContinueConversation {
		return nil, nil
	}

	timeout := defaultLoadTimeout
	if o.LoadTimeoutMS > 0 {
		timeout = time.Duration(o.LoadTimeoutMS) * time.Millisecond
	}

	cwd := "."
	if o.Cwd != nil {
		cwd = *o.Cwd
	}
	projectKey := ProjectKeyForDirectory(cwd)

	var (
		sessionID string
		entries   []types.SessionStoreEntry
		err       error
	)
	if o.Resume != nil {
		// The session ID becomes a path component below, so anything that is
		// not a UUID is refused rather than sanitized.
		if !uuidPattern.MatchString(*o.Resume) {
			return nil, nil
		}
		sessionID = *o.Resume
		entries, err = loadSession(ctx, o.SessionStore, projectKey, sessionID, timeout)
	} else {
		sessionID, entries, err = resolveContinueCandidate(ctx, o.SessionStore, projectKey, timeout)
	}
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}

	base, err := os.MkdirTemp("", "claude-resume-")
	if err != nil {
		return nil, fmt.Errorf("creating resume config dir: %w", err)
	}

	// Any failure past this point leaves a directory behind that may already
	// hold a copy of the caller's credentials, and the caller has no handle
	// to clean it up. Remove it before returning the error.
	materialized, err := populateResumeDir(ctx, o, base, projectKey, sessionID, entries, timeout)
	if err != nil {
		removeWithRetry(base)
		return nil, err
	}
	return materialized, nil
}

// populateResumeDir writes the transcript, auth files, and subagent
// transcripts into an already-created temporary config directory.
func populateResumeDir(
	ctx context.Context,
	o *AgentOptions,
	base, projectKey, sessionID string,
	entries []types.SessionStoreEntry,
	timeout time.Duration,
) (*materializedResume, error) {
	projectDir := filepath.Join(base, "projects", projectKey)
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		return nil, fmt.Errorf("creating resume project dir: %w", err)
	}
	if err := writeJSONL(filepath.Join(projectDir, sessionID+".jsonl"), entries); err != nil {
		return nil, err
	}

	// The subprocess runs with CLAUDE_CONFIG_DIR pointed here, so it needs the
	// caller's auth configuration to authenticate. Missing files are fine:
	// API-key auth needs none of them.
	copyAuthFiles(base, o.Env)

	if lister, ok := o.SessionStore.(SessionSubkeyLister); ok {
		if err := materializeSubkeys(
			ctx, o.SessionStore, lister, projectDir, projectKey, sessionID, timeout,
		); err != nil {
			return nil, err
		}
	}

	return &materializedResume{configDir: base, sessionID: sessionID}, nil
}

// applyMaterializedOptions repoints options at a materialized config dir.
func applyMaterializedOptions(o *AgentOptions, m *materializedResume) {
	if m == nil {
		return
	}
	if o.Env == nil {
		o.Env = make(map[string]string, 1)
	}
	o.Env["CLAUDE_CONFIG_DIR"] = m.configDir
	sessionID := m.sessionID
	o.Resume = &sessionID
	// The continue was already resolved to a concrete session id.
	o.ContinueConversation = false
}

// loadSession loads one session's entries, bounding the store call.
func loadSession(
	ctx context.Context, store SessionStore, projectKey, sessionID string, timeout time.Duration,
) ([]types.SessionStoreEntry, error) {
	loadCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	entries, err := store.Load(loadCtx, types.SessionKey{
		ProjectKey: projectKey,
		SessionID:  sessionID,
	})
	if err != nil {
		return nil, resumeStoreError(loadCtx, fmt.Sprintf("SessionStore.Load() for session %s", sessionID), timeout, err)
	}
	return entries, nil
}

// resolveContinueCandidate picks the most recently modified non-sidechain
// session to continue.
//
// Sidechain transcripts are mirrored as ordinary top-level keys and often
// carry the highest mtime, since their append lands after the main session's
// in the same flush. Walking newest to oldest and skipping sidechains makes
// ContinueConversation resume the user's conversation rather than a subagent's,
// matching the CLI's own --continue filter.
func resolveContinueCandidate(
	ctx context.Context, store SessionStore, projectKey string, timeout time.Duration,
) (string, []types.SessionStoreEntry, error) {
	lister, ok := store.(SessionLister)
	if !ok {
		return "", nil, nil
	}

	listCtx, cancel := context.WithTimeout(ctx, timeout)
	listed, err := lister.ListSessions(listCtx, projectKey)
	cancel()
	if err != nil {
		return "", nil, resumeStoreError(listCtx, "SessionStore.ListSessions()", timeout, err)
	}
	if len(listed) == 0 {
		return "", nil, nil
	}

	candidates := append([]types.SessionStoreListEntry(nil), listed...)
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].MTime > candidates[j].MTime
	})

	for _, candidate := range candidates {
		if !uuidPattern.MatchString(candidate.SessionID) {
			continue
		}
		entries, err := loadSession(ctx, store, projectKey, candidate.SessionID, timeout)
		if err != nil {
			return "", nil, err
		}
		if len(entries) == 0 {
			continue
		}
		if sidechain, _ := entries[0]["isSidechain"].(bool); sidechain {
			continue
		}
		return candidate.SessionID, entries, nil
	}
	return "", nil, nil
}

// resumeStoreError wraps an adapter failure with the context a caller needs to
// tell a timeout from a genuine error.
func resumeStoreError(ctx context.Context, what string, timeout time.Duration, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%s timed out after %s during resume materialization", what, timeout)
	}
	return fmt.Errorf("%s failed during resume materialization: %w", what, err)
}

// materializeSubkeys writes every subagent transcript and metadata sidecar
// stored under a session.
func materializeSubkeys(
	ctx context.Context,
	store SessionStore,
	lister SessionSubkeyLister,
	projectDir, projectKey, sessionID string,
	timeout time.Duration,
) error {
	sessionDir := filepath.Join(projectDir, sessionID)

	listCtx, cancel := context.WithTimeout(ctx, timeout)
	subkeys, err := lister.ListSubkeys(listCtx, projectKey, sessionID)
	cancel()
	if err != nil {
		return resumeStoreError(listCtx,
			fmt.Sprintf("SessionStore.ListSubkeys() for session %s", sessionID), timeout, err)
	}

	for _, subpath := range subkeys {
		// Subpaths come from an external store and become filesystem path
		// components, so one that would escape the session directory is
		// skipped rather than trusted.
		if !isSafeSubpath(subpath, sessionDir) {
			log.Printf("claude-agent-sdk: skipping unsafe subpath from ListSubkeys: %q", subpath)
			continue
		}

		loadCtx, cancel := context.WithTimeout(ctx, timeout)
		entries, err := store.Load(loadCtx, types.SessionKey{
			ProjectKey: projectKey,
			SessionID:  sessionID,
			Subpath:    subpath,
		})
		cancel()
		if err != nil {
			return resumeStoreError(loadCtx,
				fmt.Sprintf("SessionStore.Load() for session %s subpath %s", sessionID, subpath),
				timeout, err)
		}
		if len(entries) == 0 {
			continue
		}

		metadata, transcript := splitAgentMetadata(entries)
		subFile := filepath.Join(sessionDir, filepath.FromSlash(subpath)) + ".jsonl"

		if len(transcript) > 0 {
			if err := writeJSONL(subFile, transcript); err != nil {
				return err
			}
		}
		if metadata != nil {
			// The synthetic type discriminator is the store's, not the
			// sidecar's.
			content := make(map[string]any, len(metadata))
			for k, v := range metadata {
				if k != "type" {
					content[k] = v
				}
			}
			if err := writeJSONFile(sessions.AgentMetadataSidecarPath(subFile), content); err != nil {
				return err
			}
		}
	}
	return nil
}

// isSafeSubpath reports whether a store-supplied subpath stays inside the
// session directory.
//
// Empty is rejected explicitly: "" + ".jsonl" is a hidden dotfile that would
// pass a naive prefix check. Both separators are checked regardless of host
// OS, since a store may have been populated on the other platform.
func isSafeSubpath(subpath, sessionDir string) bool {
	if subpath == "" || strings.ContainsRune(subpath, 0) {
		return false
	}
	if strings.HasPrefix(subpath, "/") || strings.HasPrefix(subpath, `\`) {
		return false
	}
	// A drive prefix ("C:foo") or UNC path is never a legitimate store key.
	if len(subpath) >= 2 && subpath[1] == ':' {
		return false
	}
	for _, part := range strings.FieldsFunc(subpath, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == "." || part == ".." {
			return false
		}
	}

	// Resolve the target the writer will use, so the validated path cannot
	// drift from the written one, and confirm it stays under sessionDir.
	target := filepath.Join(sessionDir, filepath.FromSlash(subpath)) + ".jsonl"
	rel, err := filepath.Rel(sessionDir, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// writeJSONL writes entries as one compact JSON object per line, mode 0600.
func writeJSONL(path string, entries []types.SessionStoreEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	encoder := json.NewEncoder(file)
	for _, entry := range entries {
		if err := encoder.Encode(entry); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	return file.Close()
}

// writeJSONFile writes one JSON object, mode 0600.
func writeJSONFile(path string, content map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	data, err := json.Marshal(content)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// copyAuthFiles seeds the temporary config dir with the caller's auth and user
// configuration: .credentials.json (with the refresh token redacted),
// .claude.json, and the user settings files.
//
// Source resolution mirrors the CLI: .credentials.json, settings.json and
// cowork_settings.json live under the config dir (default ~/.claude/), while
// .claude.json lives at $CLAUDE_CONFIG_DIR/.claude.json when set and
// ~/.claude.json otherwise -- not ~/.claude/.claude.json.
func copyAuthFiles(base string, optEnv map[string]string) {
	callerConfigDir := optEnv["CLAUDE_CONFIG_DIR"]
	if callerConfigDir == "" {
		callerConfigDir = os.Getenv("CLAUDE_CONFIG_DIR")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	sourceConfigDir := callerConfigDir
	if sourceConfigDir == "" {
		sourceConfigDir = filepath.Join(home, ".claude")
	}

	creds := readIfPresent(filepath.Join(sourceConfigDir, ".credentials.json"))

	// The macOS default keeps OAuth tokens in the Keychain rather than a
	// file. Redirecting CLAUDE_CONFIG_DIR changes the Keychain service-name
	// suffix, so the subprocess's lookup misses and falls back to the plain
	// file in the temp dir. Populate it from the parent's Keychain so the
	// resumed subprocess can authenticate. Skipped when env-based auth or a
	// custom config dir is already in play.
	if callerConfigDir == "" &&
		envOr(optEnv, "ANTHROPIC_API_KEY") == "" &&
		envOr(optEnv, "CLAUDE_CODE_OAUTH_TOKEN") == "" {
		if keychain := readKeychainCredentials(); keychain != nil {
			creds = keychain
		}
	}
	writeRedactedCredentials(creds, filepath.Join(base, ".credentials.json"))

	claudeJSON := filepath.Join(home, ".claude.json")
	if callerConfigDir != "" {
		claudeJSON = filepath.Join(callerConfigDir, ".claude.json")
	}
	copyIfPresent(claudeJSON, filepath.Join(base, ".claude.json"), nil)

	// User settings carry apiKeyHelper -- a fourth auth mechanism alongside
	// the credentials file, the Keychain, and the environment -- plus env,
	// hooks, and permissions. Without them a host authenticating solely
	// through apiKeyHelper fails with "Not logged in". cowork_settings.json is
	// the alternate filename the CLI reads in cowork-plugins mode.
	for _, name := range []string{"settings.json", "cowork_settings.json"} {
		copyIfPresent(
			filepath.Join(sourceConfigDir, name),
			filepath.Join(base, name),
			stripSettingsForResume,
		)
	}
}

// envOr reads a variable from the options env, falling back to the process
// environment.
func envOr(optEnv map[string]string, key string) string {
	if v, ok := optEnv[key]; ok && v != "" {
		return v
	}
	return os.Getenv(key)
}

// stripSettingsForResume drops settings keys that misbehave under a redirected
// config dir.
//
// Content that does not parse as a JSON object is returned untouched, so the
// subprocess sees exactly what the CLI would have read.
func stripSettingsForResume(content []byte) []byte {
	// The CLI's settings reader tolerates a UTF-8 BOM, which PowerShell
	// writes; a plain unmarshal would reject it.
	trimmed := strings.TrimPrefix(string(content), "\ufeff")

	var parsed map[string]any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil || parsed == nil {
		return content
	}

	stripped := false
	for _, key := range resumeSettingsStrippedKeys {
		if _, ok := parsed[key]; ok {
			delete(parsed, key)
			stripped = true
		}
	}
	if env, ok := parsed["env"].(map[string]any); ok {
		if _, ok := env["CLAUDE_CONFIG_DIR"]; ok {
			delete(env, "CLAUDE_CONFIG_DIR")
			stripped = true
		}
	}
	if !stripped {
		return content
	}

	out, err := json.Marshal(parsed)
	if err != nil {
		return content
	}
	return out
}

// writeRedactedCredentials writes credentials with claudeAiOauth.refreshToken
// removed.
//
// The resumed subprocess runs under a redirected config dir. If it refreshed,
// the single-use refresh token would be consumed server-side and the new
// tokens written where the parent never reads them back, leaving the parent's
// stored credentials revoked. With no refresh token the subprocess's refresh
// check short-circuits.
func writeRedactedCredentials(creds []byte, dst string) {
	if creds == nil {
		return
	}
	out := creds

	var parsed map[string]any
	if err := json.Unmarshal(creds, &parsed); err == nil {
		if oauth, ok := parsed["claudeAiOauth"].(map[string]any); ok {
			if _, ok := oauth["refreshToken"]; ok {
				delete(oauth, "refreshToken")
				if redacted, err := json.Marshal(parsed); err == nil {
					out = redacted
				}
			}
		}
	}

	if err := os.WriteFile(dst, out, 0o600); err != nil {
		log.Printf("claude-agent-sdk: resume: skipping %s (%v)", dst, err)
	}
}

// readIfPresent reads a regular file, or returns nil.
//
// A missing source is skipped silently. Any other reason it cannot be read --
// a permission error, or a directory or FIFO where a file was expected -- is
// logged and skipped: these files are best-effort enrichment of the temp
// config dir, so an unreadable one must not abort, or in the FIFO case hang,
// the resume.
func readIfPresent(src string) []byte {
	info, err := os.Stat(src)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("claude-agent-sdk: resume: skipping %s (%v)", src, err)
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		log.Printf("claude-agent-sdk: resume: skipping %s (not a regular file)", src)
		return nil
	}
	content, err := os.ReadFile(src)
	if err != nil {
		log.Printf("claude-agent-sdk: resume: skipping %s (%v)", src, err)
		return nil
	}
	return content
}

// copyIfPresent copies src to dst through an optional transform, mode 0600.
func copyIfPresent(src, dst string, transform func([]byte) []byte) {
	content := readIfPresent(src)
	if content == nil {
		return
	}
	if transform != nil {
		content = transform(content)
	}
	if err := os.WriteFile(dst, content, 0o600); err != nil {
		// Do not leave a truncated destination behind for the subprocess to
		// misparse.
		_ = os.Remove(dst)
		log.Printf("claude-agent-sdk: resume: skipping %s (%v)", src, err)
	}
}

// readKeychainCredentials reads the OAuth credentials JSON from the macOS
// Keychain. Best effort: it returns nil on any error, and on every other
// platform.
func readKeychainCredentials() []byte {
	if runtime.GOOS != "darwin" {
		return nil
	}

	account := os.Getenv("USER")
	if account == "" {
		if u, err := user.Current(); err == nil {
			account = u.Username
		} else {
			account = "claude-code-user"
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "security", "find-generic-password",
		"-a", account, "-w", "-s", keychainServiceName).Output()
	if err != nil {
		return nil
	}
	if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
		return []byte(trimmed)
	}
	return nil
}

// removeWithRetry deletes a directory tree, retrying transient lock errors.
//
// On Windows an antivirus scanner or the indexer can briefly hold a handle on
// a freshly written file -- notably .credentials.json -- so a single attempt
// can fail. Retrying a few times gives the handle a chance to release rather
// than leaving an access token in temp. Never returns an error: there is
// nothing a caller could do with one.
func removeWithRetry(path string) {
	for attempt := 0; attempt < 4; attempt++ {
		if err := os.RemoveAll(path); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = os.RemoveAll(path)
}
