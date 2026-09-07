package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

const testSessionID = "11111111-2222-3333-4444-555555555555"

// readJSONL reads back a materialized transcript.
func readJSONL(t *testing.T, path string) []map[string]any {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer func() { _ = file.Close() }()

	var out []map[string]any
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %q is not JSON: %v", line, err)
		}
		out = append(out, entry)
	}
	return out
}

// isolateHome points HOME and CLAUDE_CONFIG_DIR at a temp directory so a test
// never reads or writes the developer's real credentials.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	return home
}

// A store-backed resume has no local transcript for the CLI to read, so the
// session is written into a temp config dir the subprocess is pointed at.
func TestMaterializeResumeWritesTranscript(t *testing.T) {
	isolateHome(t)
	ctx := context.Background()

	store := NewInMemorySessionStore()
	key := types.SessionKey{ProjectKey: ProjectKeyForDirectory("."), SessionID: testSessionID}
	entries := []types.SessionStoreEntry{userEntry("u1", "hello"), userEntry("u2", "world")}
	if err := store.Append(ctx, key, entries); err != nil {
		t.Fatal(err)
	}

	opts := &AgentOptions{SessionStore: store, Resume: String(testSessionID)}
	materialized, err := materializeResumeSession(ctx, opts)
	if err != nil {
		t.Fatalf("materializeResumeSession: %v", err)
	}
	if materialized == nil {
		t.Fatal("expected a materialized resume")
	}
	defer materialized.cleanup()

	transcript := filepath.Join(materialized.configDir, "projects",
		ProjectKeyForDirectory("."), testSessionID+".jsonl")
	lines := readJSONL(t, transcript)
	if len(lines) != 2 {
		t.Fatalf("expected two transcript lines, got %d", len(lines))
	}
	if lines[0]["uuid"] != "u1" || lines[1]["uuid"] != "u2" {
		t.Errorf("entries are out of order: %v", lines)
	}

	// The transcript holds conversation content, so it must not be
	// world-readable in a shared temp directory.
	info, err := os.Stat(transcript)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("transcript mode = %o, want 600", perm)
	}
}

// The subprocess reads the materialized copy, and the resume is a concrete
// session id by then.
func TestApplyMaterializedOptions(t *testing.T) {
	opts := &AgentOptions{ContinueConversation: true}
	applyMaterializedOptions(opts, &materializedResume{
		configDir: "/tmp/claude-resume-x", sessionID: testSessionID,
	})

	if opts.Env["CLAUDE_CONFIG_DIR"] != "/tmp/claude-resume-x" {
		t.Errorf("CLAUDE_CONFIG_DIR = %q", opts.Env["CLAUDE_CONFIG_DIR"])
	}
	if opts.Resume == nil || *opts.Resume != testSessionID {
		t.Errorf("Resume = %v, want the materialized session", opts.Resume)
	}
	if opts.ContinueConversation {
		t.Error("ContinueConversation must be cleared once it resolves to a session id")
	}
}

func TestApplyMaterializedOptionsNoopWithoutResume(t *testing.T) {
	opts := &AgentOptions{ContinueConversation: true}
	applyMaterializedOptions(opts, nil)

	if opts.Resume != nil || !opts.ContinueConversation {
		t.Errorf("options must be untouched without a materialized resume: %+v", opts)
	}
}

// Materialization is skipped whenever the ordinary resume path is correct, so
// a caller without a store, or with an empty one, behaves as before.
func TestMaterializeResumeSkips(t *testing.T) {
	isolateHome(t)
	ctx := context.Background()
	populated := NewInMemorySessionStore()
	if err := populated.Append(ctx,
		types.SessionKey{ProjectKey: ProjectKeyForDirectory("."), SessionID: testSessionID},
		[]types.SessionStoreEntry{userEntry("u1", "hi")}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		opts *AgentOptions
	}{
		{"no store", &AgentOptions{Resume: String(testSessionID)}},
		{"no resume or continue", &AgentOptions{SessionStore: populated}},
		{"empty store", &AgentOptions{
			SessionStore: NewInMemorySessionStore(), Resume: String(testSessionID),
		}},
		{"resume is not a uuid", &AgentOptions{
			SessionStore: populated, Resume: String("../../etc/passwd"),
		}},
		{"continue with an empty store", &AgentOptions{
			SessionStore: NewInMemorySessionStore(), ContinueConversation: true,
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			materialized, err := materializeResumeSession(ctx, tc.opts)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if materialized != nil {
				materialized.cleanup()
				t.Error("expected the ordinary resume path to be used")
			}
		})
	}
}

// Sidechain transcripts are mirrored as ordinary top-level keys and often have
// the highest mtime, so --continue must skip past them to the user's own
// conversation.
func TestMaterializeResumeContinueSkipsSidechains(t *testing.T) {
	isolateHome(t)
	ctx := context.Background()
	projectKey := ProjectKeyForDirectory(".")
	store := NewInMemorySessionStore()

	main := "aaaaaaaa-2222-3333-4444-555555555555"
	sidechain := "bbbbbbbb-2222-3333-4444-555555555555"

	if err := store.Append(ctx,
		types.SessionKey{ProjectKey: projectKey, SessionID: main},
		[]types.SessionStoreEntry{userEntry("u1", "the real conversation")}); err != nil {
		t.Fatal(err)
	}
	// Appended second, so it carries the later mtime.
	sidechainEntry := userEntry("u2", "subagent chatter")
	sidechainEntry["isSidechain"] = true
	if err := store.Append(ctx,
		types.SessionKey{ProjectKey: projectKey, SessionID: sidechain},
		[]types.SessionStoreEntry{sidechainEntry}); err != nil {
		t.Fatal(err)
	}

	materialized, err := materializeResumeSession(ctx,
		&AgentOptions{SessionStore: store, ContinueConversation: true})
	if err != nil {
		t.Fatalf("materializeResumeSession: %v", err)
	}
	if materialized == nil {
		t.Fatal("expected a materialized resume")
	}
	defer materialized.cleanup()

	if materialized.sessionID != main {
		t.Errorf("resolved session = %q, want the non-sidechain session %q",
			materialized.sessionID, main)
	}
}

// Subagent transcripts and their metadata sidecars have to land beside the
// main transcript, or a resumed session loses its subagent history.
func TestMaterializeResumeWritesSubagents(t *testing.T) {
	isolateHome(t)
	ctx := context.Background()
	projectKey := ProjectKeyForDirectory(".")
	store := NewInMemorySessionStore()

	if err := store.Append(ctx,
		types.SessionKey{ProjectKey: projectKey, SessionID: testSessionID},
		[]types.SessionStoreEntry{userEntry("u1", "hi")}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(ctx,
		types.SessionKey{ProjectKey: projectKey, SessionID: testSessionID, Subpath: "subagents/agent-7"},
		[]types.SessionStoreEntry{
			{"type": "agent_metadata", "toolUseId": "tool-1", "parentAgentId": "agent-parent"},
			userEntry("s1", "subagent turn"),
		}); err != nil {
		t.Fatal(err)
	}

	materialized, err := materializeResumeSession(ctx,
		&AgentOptions{SessionStore: store, Resume: String(testSessionID)})
	if err != nil {
		t.Fatalf("materializeResumeSession: %v", err)
	}
	defer materialized.cleanup()

	sessionDir := filepath.Join(materialized.configDir, "projects", projectKey, testSessionID)
	transcript := filepath.Join(sessionDir, "subagents", "agent-7.jsonl")
	lines := readJSONL(t, transcript)
	if len(lines) != 1 || lines[0]["uuid"] != "s1" {
		t.Errorf("unexpected subagent transcript: %v", lines)
	}

	sidecar := filepath.Join(sessionDir, "subagents", "agent-7.meta.json")
	raw, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatalf("reading sidecar: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("sidecar is not JSON: %v", err)
	}
	if meta["toolUseId"] != "tool-1" || meta["parentAgentId"] != "agent-parent" {
		t.Errorf("unexpected sidecar: %v", meta)
	}
	// The type discriminator is the store's framing, not part of the sidecar.
	if _, ok := meta["type"]; ok {
		t.Error("the synthetic type field must be stripped from the sidecar")
	}
}

// Subpaths come from an external store and become filesystem paths, so one
// that would escape the session directory is skipped rather than trusted.
func TestIsSafeSubpath(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), "session")

	safe := []string{"subagents/agent-1", "subagents/nested/agent-2", "agent-3"}
	for _, subpath := range safe {
		if !isSafeSubpath(subpath, sessionDir) {
			t.Errorf("isSafeSubpath(%q) = false, want true", subpath)
		}
	}

	unsafe := []string{
		"",
		"..",
		"../escape",
		"subagents/../../escape",
		`subagents\..\..\escape`,
		"/etc/passwd",
		`\etc\passwd`,
		"C:evil",
		"agent\x00",
	}
	for _, subpath := range unsafe {
		if isSafeSubpath(subpath, sessionDir) {
			t.Errorf("isSafeSubpath(%q) = true, want false", subpath)
		}
	}
}

// The resumed subprocess runs under a redirected config dir. If it refreshed,
// the single-use refresh token would be consumed server-side and the new
// tokens written where the parent never reads them, revoking the parent's
// credentials.
func TestWriteRedactedCredentials(t *testing.T) {
	dst := filepath.Join(t.TempDir(), ".credentials.json")
	writeRedactedCredentials([]byte(`{"claudeAiOauth":{"accessToken":"at","refreshToken":"rt"}}`), dst)

	raw, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading credentials: %v", err)
	}
	if strings.Contains(string(raw), "refreshToken") {
		t.Errorf("refresh token was not redacted: %s", raw)
	}
	if !strings.Contains(string(raw), "accessToken") {
		t.Errorf("access token must survive: %s", raw)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("credentials mode = %o, want 600", perm)
	}
}

// Unparseable credentials are written through: the subprocess will fail to
// parse them too, and rewriting them would only hide the cause.
func TestWriteRedactedCredentialsPassesThroughUnparseable(t *testing.T) {
	dst := filepath.Join(t.TempDir(), ".credentials.json")
	writeRedactedCredentials([]byte("not json"), dst)

	raw, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading credentials: %v", err)
	}
	if string(raw) != "not json" {
		t.Errorf("content = %q, want it written through", raw)
	}
}

func TestWriteRedactedCredentialsSkipsNil(t *testing.T) {
	dst := filepath.Join(t.TempDir(), ".credentials.json")
	writeRedactedCredentials(nil, dst)

	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Error("no credentials file must be written when there are none to copy")
	}
}

// Plugin declarations reconcile against the always-empty temp plugin cache and
// would network-install each declared marketplace on every resume; an env
// CLAUDE_CONFIG_DIR would point the subprocess's reads away from the temp dir.
func TestStripSettingsForResume(t *testing.T) {
	out := stripSettingsForResume([]byte(`{
		"apiKeyHelper": "/bin/get-key",
		"enabledPlugins": {"a": true},
		"extraKnownMarketplaces": {"m": {}},
		"env": {"CLAUDE_CONFIG_DIR": "/elsewhere", "FOO": "bar"}
	}`))

	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	for _, key := range []string{"enabledPlugins", "extraKnownMarketplaces"} {
		if _, ok := parsed[key]; ok {
			t.Errorf("%s must be stripped", key)
		}
	}
	// apiKeyHelper is a whole auth mechanism; stripping it would break hosts
	// that authenticate only that way.
	if parsed["apiKeyHelper"] != "/bin/get-key" {
		t.Error("apiKeyHelper must survive")
	}
	env, _ := parsed["env"].(map[string]any)
	if _, ok := env["CLAUDE_CONFIG_DIR"]; ok {
		t.Error("env.CLAUDE_CONFIG_DIR must be stripped")
	}
	if env["FOO"] != "bar" {
		t.Error("other env entries must survive")
	}
}

// Settings with nothing to strip are passed through byte for byte, so the
// subprocess sees exactly what the CLI would have read.
func TestStripSettingsForResumePassesThroughUntouched(t *testing.T) {
	input := []byte(`{"apiKeyHelper": "/bin/get-key"}`)
	if out := stripSettingsForResume(input); string(out) != string(input) {
		t.Errorf("output = %q, want the input unchanged", out)
	}

	notAnObject := []byte(`[1, 2, 3]`)
	if out := stripSettingsForResume(notAnObject); string(out) != string(notAnObject) {
		t.Errorf("output = %q, want the input unchanged", out)
	}

	notJSON := []byte("// a comment\n{}")
	if out := stripSettingsForResume(notJSON); string(out) != string(notJSON) {
		t.Errorf("output = %q, want the input unchanged", out)
	}
}

// PowerShell writes settings.json with a UTF-8 BOM, which the CLI's reader
// tolerates.
func TestStripSettingsForResumeHandlesBOM(t *testing.T) {
	out := stripSettingsForResume([]byte("\ufeff" + `{"enabledPlugins":{"a":true},"model":"opus"}`))

	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if _, ok := parsed["enabledPlugins"]; ok {
		t.Error("enabledPlugins must be stripped from BOM-prefixed settings")
	}
	if parsed["model"] != "opus" {
		t.Error("other settings must survive")
	}
}

// Without user settings an apiKeyHelper-only host fails with "Not logged in".
func TestCopyAuthFilesSeedsSettings(t *testing.T) {
	home := isolateHome(t)
	configDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}

	write := func(path, content string) {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(configDir, "settings.json"), `{"apiKeyHelper":"/bin/key","enabledPlugins":{"a":true}}`)
	write(filepath.Join(configDir, "cowork_settings.json"), `{"apiKeyHelper":"/bin/key2"}`)
	write(filepath.Join(configDir, ".credentials.json"), `{"claudeAiOauth":{"accessToken":"at","refreshToken":"rt"}}`)
	// .claude.json lives beside the config dir, not inside it.
	write(filepath.Join(home, ".claude.json"), `{"userID":"abc"}`)

	base := t.TempDir()
	copyAuthFiles(base, nil)

	for _, name := range []string{"settings.json", "cowork_settings.json", ".credentials.json", ".claude.json"} {
		if _, err := os.Stat(filepath.Join(base, name)); err != nil {
			t.Errorf("%s was not seeded: %v", name, err)
		}
	}

	settings, err := os.ReadFile(filepath.Join(base, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(settings), "apiKeyHelper") {
		t.Error("apiKeyHelper must reach the subprocess")
	}
	if strings.Contains(string(settings), "enabledPlugins") {
		t.Error("plugin declarations must be stripped")
	}

	creds, err := os.ReadFile(filepath.Join(base, ".credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(creds), "refreshToken") {
		t.Error("the refresh token must be redacted")
	}
}

// Nothing to copy is the normal case for API-key auth, and must not fail.
func TestCopyAuthFilesToleratesMissingSources(t *testing.T) {
	isolateHome(t)
	base := t.TempDir()
	copyAuthFiles(base, nil)

	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected nothing to be seeded, got %v", entries)
	}
}

// A custom config dir is honored, and it also suppresses the Keychain
// fallback, since the caller has already chosen where credentials live.
func TestCopyAuthFilesHonorsCustomConfigDir(t *testing.T) {
	isolateHome(t)
	custom := t.TempDir()
	if err := os.WriteFile(filepath.Join(custom, "settings.json"), []byte(`{"model":"opus"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	base := t.TempDir()
	copyAuthFiles(base, map[string]string{"CLAUDE_CONFIG_DIR": custom})

	settings, err := os.ReadFile(filepath.Join(base, "settings.json"))
	if err != nil {
		t.Fatalf("settings were not seeded from the custom config dir: %v", err)
	}
	if !strings.Contains(string(settings), "opus") {
		t.Errorf("settings = %s", settings)
	}
}

// The temp dir holds a copy of the caller's credentials, so it must not
// outlive the session.
func TestMaterializedResumeCleanup(t *testing.T) {
	isolateHome(t)
	ctx := context.Background()

	store := NewInMemorySessionStore()
	if err := store.Append(ctx,
		types.SessionKey{ProjectKey: ProjectKeyForDirectory("."), SessionID: testSessionID},
		[]types.SessionStoreEntry{userEntry("u1", "hi")}); err != nil {
		t.Fatal(err)
	}

	materialized, err := materializeResumeSession(ctx,
		&AgentOptions{SessionStore: store, Resume: String(testSessionID)})
	if err != nil {
		t.Fatalf("materializeResumeSession: %v", err)
	}

	dir := materialized.configDir
	materialized.cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("temp config dir %s survived cleanup", dir)
	}

	// Cleanup runs on every teardown path, so it has to be idempotent.
	materialized.cleanup()
	(*materializedResume)(nil).cleanup()
}

// A store failure has to surface with enough context to tell it from a
// missing session, and must not leave a temp dir holding credentials behind.
func TestMaterializeResumeSurfacesStoreErrors(t *testing.T) {
	isolateHome(t)

	store := &failingStore{err: errFailingStore}
	_, err := materializeResumeSession(context.Background(),
		&AgentOptions{SessionStore: store, Resume: String(testSessionID)})
	if err == nil {
		t.Fatal("expected the store failure to surface")
	}
	if !strings.Contains(err.Error(), "resume materialization") {
		t.Errorf("error %q does not say what was being attempted", err)
	}
}

var errFailingStore = &storeError{"backend unavailable"}

type storeError struct{ msg string }

func (e *storeError) Error() string { return e.msg }

type failingStore struct{ err error }

func (s *failingStore) Append(context.Context, types.SessionKey, []types.SessionStoreEntry) error {
	return s.err
}

func (s *failingStore) Load(context.Context, types.SessionKey) ([]types.SessionStoreEntry, error) {
	return nil, s.err
}
