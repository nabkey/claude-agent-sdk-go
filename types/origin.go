package types

// MessageOriginKind discriminates the provenance of a user-role message.
//
// Newer CLI versions may report kinds not listed here; treat anything
// unrecognized as "not human".
type MessageOriginKind string

const (
	// MessageOriginKindHuman is a prompt the application submitted.
	MessageOriginKindHuman MessageOriginKind = "human"
	// MessageOriginKindChannel is a message pushed by a channel MCP server.
	MessageOriginKindChannel MessageOriginKind = "channel"
	// MessageOriginKindPeer is a message relayed from another session.
	MessageOriginKindPeer MessageOriginKind = "peer"
	// MessageOriginKindTaskNotification is a background-task notification or
	// a scheduled trigger's stored prompt.
	MessageOriginKindTaskNotification MessageOriginKind = "task-notification"
	// MessageOriginKindCoordinator is a turn injected by the coordinator.
	MessageOriginKindCoordinator MessageOriginKind = "coordinator"
	// MessageOriginKindUnclassified is a turn the CLI could not attribute.
	MessageOriginKindUnclassified MessageOriginKind = "unclassified"
	// MessageOriginKindObserver is a message from a background observer agent.
	MessageOriginKindObserver MessageOriginKind = "observer"
	// MessageOriginKindAutoContinuation is a turn the CLI continued on its own.
	MessageOriginKindAutoContinuation MessageOriginKind = "auto-continuation"
	// MessageOriginKindObserverActivity is an activity digest for an observer.
	MessageOriginKindObserverActivity MessageOriginKind = "observer-activity"
)

// TaskNotificationOriginSubkind refines a task-notification origin.
type TaskNotificationOriginSubkind string

const (
	// TaskNotificationOriginScheduledTrigger is a scheduled task's fired prompt.
	TaskNotificationOriginScheduledTrigger TaskNotificationOriginSubkind = "scheduled-trigger"
	// TaskNotificationOriginPeerSendMessage is a SendMessage delivery from
	// another of the user's sessions.
	TaskNotificationOriginPeerSendMessage TaskNotificationOriginSubkind = "peer-send-message"
	// TaskNotificationOriginProjectsRelay is a projects relay delivery.
	TaskNotificationOriginProjectsRelay TaskNotificationOriginSubkind = "projects-relay"
)

// PeerOriginMode is the sending session's permission class, as declared by the
// host that injected a peer message.
type PeerOriginMode string

const (
	// PeerOriginModeBypass means the sender runs tools without asking.
	PeerOriginModeBypass PeerOriginMode = "bypass"
	// PeerOriginModePrompting means the sender prompts for permission.
	PeerOriginModePrompting PeerOriginMode = "prompting"
)

// MessageOrigin is the provenance of a user-role message and, on a
// ResultMessage, of the message that triggered that turn.
//
// In streaming-input mode a single connection interleaves the turns the caller
// sends with turns the session injects on its own -- background-task
// notifications, scheduled-task prompts, MCP channel messages, peer messages.
// Origin tells them apart, so a consumer can decide whether a result answers
// its own prompt:
//
//	if msg.Origin == nil || msg.Origin.IsHuman() {
//	    // a turn this application submitted
//	}
//
// Only Kind is always populated; the remaining fields depend on Kind. A nil
// Origin means the CLI did not attribute the message -- prompts sent through
// Query or Client arrive that way unless the caller stamps
// {"kind": "human"} on the message itself.
//
// Fields sourced from a peer are sender-asserted. Use From for reply routing
// and display, never as proof of identity; VerifiedPeerPID is the only
// kernel-verified field, and it names the connecting process, which for
// relayed traffic is the relay rather than the author.
type MessageOrigin struct {
	// Kind is the discriminator.
	Kind MessageOriginKind `json:"kind"`
	// Server names the MCP server a channel message arrived on.
	Server string `json:"server,omitempty"`
	// From is the sender address of a peer or observer message.
	From string `json:"from,omitempty"`
	// FromMode is the sending session's permission class, when the injecting
	// host declared one.
	FromMode PeerOriginMode `json:"fromMode,omitempty"`
	// Name is the peer's display name, already normalized by the CLI.
	Name string `json:"name,omitempty"`
	// FromSession is the sender's host-openable session id. A navigation
	// target only.
	FromSession string `json:"fromSession,omitempty"`
	// SenderTaskID is the task id of the in-process background subagent that
	// sent the message. Empty for cross-session peers.
	SenderTaskID string `json:"senderTaskId,omitempty"`
	// Body is the decoded peer message with the envelope stripped, byte-exact
	// with what the model saw. Render this instead of re-parsing the text.
	Body string `json:"body,omitempty"`
	// VerifiedPeerPID is the kernel-verified pid of the process that connected
	// to this session's messaging socket. Nil when unverifiable.
	VerifiedPeerPID *int `json:"verifiedPeerPid,omitempty"`
	// Subkind refines a task-notification origin.
	Subkind TaskNotificationOriginSubkind `json:"subkind,omitempty"`
	// Raw is the full payload, including keys not modeled here.
	Raw map[string]any `json:"-"`
}

// IsHuman reports whether the origin is an application-submitted prompt.
//
// A nil origin is not human: the CLI did not attribute the message. Callers
// that stamp their own sends should compare against nil explicitly.
func (o *MessageOrigin) IsHuman() bool {
	return o != nil && o.Kind == MessageOriginKindHuman
}

// MessageOriginFromAny decodes an origin payload, returning nil when absent or
// not an object.
func MessageOriginFromAny(raw any) *MessageOrigin {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	origin := &MessageOrigin{
		Kind:         MessageOriginKind(mapString(m, "kind")),
		Server:       mapString(m, "server"),
		From:         mapString(m, "from"),
		FromMode:     PeerOriginMode(mapString(m, "fromMode")),
		Name:         mapString(m, "name"),
		FromSession:  mapString(m, "fromSession"),
		SenderTaskID: mapString(m, "senderTaskId"),
		Body:         mapString(m, "body"),
		Subkind:      TaskNotificationOriginSubkind(mapString(m, "subkind")),
		Raw:          m,
	}
	if pid, ok := m["verifiedPeerPid"].(float64); ok {
		v := int(pid)
		origin.VerifiedPeerPID = &v
	}
	return origin
}
