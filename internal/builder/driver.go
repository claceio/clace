// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

const (
	// maxAutoApprovals bounds permission auto-approval per prompt turn.
	// Agents also use permission requests for "continue?" checks; unlimited
	// approval would let a looping agent burn tokens indefinitely
	maxAutoApprovals = 100

	handshakeTimeout = 120 * time.Second
	turnTimeout      = 30 * time.Minute
)

// Event is one builder session event relayed to console SSE viewers
type Event struct {
	SessionId  string `json:"session_id"`
	Kind       string `json:"kind"` // agent_chunk|thought_chunk|tool_call|tool_call_update|turn_started|turn_done|status|error
	Text       string `json:"text,omitempty"`
	ToolCallId string `json:"tool_call_id,omitempty"`
	Title      string `json:"title,omitempty"`
	ToolKind   string `json:"tool_kind,omitempty"`
	ToolStatus string `json:"tool_status,omitempty"`
	StopReason string `json:"stop_reason,omitempty"`
	Status     string `json:"status,omitempty"`
}

// liveSession is the in-memory state for a session with a running (or
// recently stopped) sandbox
type liveSession struct {
	id      string
	userID  string
	ctx     context.Context
	cancel  context.CancelFunc
	stopped bool // guarded by mu

	mu            sync.Mutex
	sandbox       *sandbox
	conn          *acp.ClientSideConnection
	acpSessionId  acp.SessionId
	turnActive    bool
	turnCancel    context.CancelFunc
	turnCancelled bool // session/cancel was sent for the current turn
	restoring     bool // session/load in progress: replayed history updates are dropped
	approvals     int
	msgBuf        strings.Builder
	chunkBreak    bool              // a non-message event arrived since the last chunk
	toolTitles    map[string]string // tool call id -> last title, fills update events that omit it
	lastActive    time.Time
	subscribers   map[int]chan Event
	nextSubId     int
}

func newLiveSession(id, userID string) *liveSession {
	ctx, cancel := context.WithCancel(context.Background())
	return &liveSession{
		ctx:         ctx,
		cancel:      cancel,
		id:          id,
		userID:      userID,
		lastActive:  time.Now(),
		toolTitles:  map[string]string{},
		subscribers: map[int]chan Event{},
	}
}

// subscribe registers an event channel; the returned func unsubscribes
func (ls *liveSession) subscribe() (<-chan Event, func()) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.stopped {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}
	id := ls.nextSubId
	ls.nextSubId++
	ch := make(chan Event, 256)
	ls.subscribers[id] = ch
	return ch, func() {
		ls.mu.Lock()
		defer ls.mu.Unlock()
		if existing, ok := ls.subscribers[id]; ok {
			delete(ls.subscribers, id)
			close(existing)
		}
	}
}

// emit sends an event to all subscribers, dropping events for slow consumers
// rather than blocking the ACP read loop
func (ls *liveSession) emit(event Event) {
	event.SessionId = ls.id
	ls.mu.Lock()
	defer ls.mu.Unlock()
	for _, ch := range ls.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

func (ls *liveSession) closeSubscribers() {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	for id, ch := range ls.subscribers {
		delete(ls.subscribers, id)
		close(ch)
	}
}

// driverClient implements acp.Client for one session. The fs and terminal
// capabilities are not advertised (agents fall back to their own filesystem
// inside the sandbox), so those methods only reject stray calls
type driverClient struct {
	manager *Manager
	session *liveSession
}

var _ acp.Client = (*driverClient)(nil)

// The fs/* and terminal/* methods are only callable when the matching client
// capability was advertised; a stray call gets the JSON-RPC method-not-found
// error (the standard answer for an unsupported method) rather than an
// internal error the agent may retry
func (d *driverClient) ReadTextFile(context.Context, acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	return acp.ReadTextFileResponse{}, acp.NewMethodNotFound(acp.ClientMethodFsReadTextFile)
}

func (d *driverClient) WriteTextFile(context.Context, acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	return acp.WriteTextFileResponse{}, acp.NewMethodNotFound(acp.ClientMethodFsWriteTextFile)
}

func (d *driverClient) CreateTerminal(context.Context, acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{}, acp.NewMethodNotFound(acp.ClientMethodTerminalCreate)
}

func (d *driverClient) KillTerminal(context.Context, acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	return acp.KillTerminalResponse{}, acp.NewMethodNotFound(acp.ClientMethodTerminalKill)
}

func (d *driverClient) TerminalOutput(context.Context, acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{}, acp.NewMethodNotFound(acp.ClientMethodTerminalOutput)
}

func (d *driverClient) ReleaseTerminal(context.Context, acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, acp.NewMethodNotFound(acp.ClientMethodTerminalRelease)
}

func (d *driverClient) WaitForTerminalExit(context.Context, acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, acp.NewMethodNotFound(acp.ClientMethodTerminalWaitForExit)
}

// RequestPermission auto-approves: the container is the safety boundary and
// interactive prompts would stall the chat. Approvals are capped per turn so
// a looping agent ("continue?" style requests) cannot run away
func (d *driverClient) RequestPermission(ctx context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	ls := d.session
	ls.mu.Lock()
	if ls.turnCancelled {
		// session/cancel was sent for this turn: the protocol requires
		// pending permission requests to resolve as cancelled, and nothing
		// may be approved after the user (or a timeout) stopped the turn
		ls.mu.Unlock()
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{Cancelled: &acp.RequestPermissionOutcomeCancelled{Outcome: "cancelled"}},
		}, nil
	}
	ls.approvals++
	approvals := ls.approvals
	ls.mu.Unlock()

	if approvals > maxAutoApprovals {
		d.manager.Warn().Str("session", ls.id).Int("approvals", approvals).Msg("Builder auto-approval cap reached, cancelling turn")
		ls.emit(Event{Kind: "error", Text: fmt.Sprintf("Auto-approval limit (%d) reached; stopping the agent. Send a new message to continue.", maxAutoApprovals)})
		// async: the agent expects this response before processing the cancel
		go func() {
			if err := d.manager.cancelTurn(ls); err != nil {
				d.manager.Warn().Err(err).Str("session", ls.id).Msg("Error cancelling turn at approval cap")
			}
		}()
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{Cancelled: &acp.RequestPermissionOutcomeCancelled{Outcome: "cancelled"}},
		}, nil
	}

	selected := pickPermissionOption(params.Options)
	if selected == "" {
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{Cancelled: &acp.RequestPermissionOutcomeCancelled{Outcome: "cancelled"}},
		}, nil
	}
	return acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{Selected: &acp.RequestPermissionOutcomeSelected{Outcome: "selected", OptionId: selected}},
	}, nil
}

// pickPermissionOption prefers allow-always, then allow-once. A request
// offering no allow option is answered with a reject option: the cancelled
// outcome means the prompt turn was cancelled, which it was not. Empty only
// when no option has a known kind
func pickPermissionOption(options []acp.PermissionOption) acp.PermissionOptionId {
	var allowOnce, rejectOnce, rejectAlways acp.PermissionOptionId
	for _, option := range options {
		switch option.Kind {
		case acp.PermissionOptionKindAllowAlways:
			return option.OptionId
		case acp.PermissionOptionKindAllowOnce:
			if allowOnce == "" {
				allowOnce = option.OptionId
			}
		case acp.PermissionOptionKindRejectOnce:
			if rejectOnce == "" {
				rejectOnce = option.OptionId
			}
		case acp.PermissionOptionKindRejectAlways:
			if rejectAlways == "" {
				rejectAlways = option.OptionId
			}
		}
	}
	switch {
	case allowOnce != "":
		return allowOnce
	case rejectOnce != "":
		return rejectOnce
	}
	return rejectAlways
}

// SessionUpdate relays typed agent updates to SSE subscribers and records
// durable rows (tool calls) in the activity log. Unknown update kinds are
// ignored: the protocol grows kinds over time
func (d *driverClient) SessionUpdate(ctx context.Context, params acp.SessionNotification) error {
	ls := d.session
	update := params.Update

	// session/load replays the stored conversation as updates; the console
	// transcript already has it (builder_activity), so replays are dropped
	ls.mu.Lock()
	restoring := ls.restoring
	ls.mu.Unlock()
	if restoring {
		return nil
	}

	switch {
	case update.AgentMessageChunk != nil:
		text := contentText(update.AgentMessageChunk.Content)
		if text != "" {
			ls.mu.Lock()
			// Separate message parts: agents stream distinct text parts
			// around tool calls with no delimiter, which would glue
			// sentences together in the transcript
			if ls.chunkBreak && ls.msgBuf.Len() > 0 && !strings.HasSuffix(ls.msgBuf.String(), "\n") {
				text = "\n\n" + text
			}
			ls.chunkBreak = false
			ls.msgBuf.WriteString(text)
			ls.mu.Unlock()
			ls.emit(Event{Kind: "agent_chunk", Text: text})
		}
	case update.AgentThoughtChunk != nil:
		ls.mu.Lock()
		ls.chunkBreak = true
		ls.mu.Unlock()
		// relayed for the "working" indicator, not persisted
		if text := contentText(update.AgentThoughtChunk.Content); text != "" {
			ls.emit(Event{Kind: "thought_chunk", Text: text})
		}
	case update.ToolCall != nil:
		toolCall := update.ToolCall
		ls.mu.Lock()
		ls.toolTitles[string(toolCall.ToolCallId)] = toolCall.Title
		ls.chunkBreak = true
		ls.mu.Unlock()
		ls.emit(Event{Kind: "tool_call", ToolCallId: string(toolCall.ToolCallId), Title: toolCall.Title,
			ToolKind: string(toolCall.Kind), ToolStatus: string(toolCall.Status)})
		d.manager.appendActivity(ls.id, ls.userID, "tool_call", toolCall.Title,
			map[string]any{"tool_call_id": string(toolCall.ToolCallId), "tool_kind": string(toolCall.Kind)})
	case update.ToolCallUpdate != nil:
		toolUpdate := update.ToolCallUpdate
		event := Event{Kind: "tool_call_update", ToolCallId: string(toolUpdate.ToolCallId)}
		ls.mu.Lock()
		if toolUpdate.Title != nil {
			ls.toolTitles[string(toolUpdate.ToolCallId)] = *toolUpdate.Title
		}
		// updates usually omit the title; fall back to the last known one
		// so SSE viewers can render the update standalone
		event.Title = ls.toolTitles[string(toolUpdate.ToolCallId)]
		ls.mu.Unlock()
		if toolUpdate.Status != nil {
			event.ToolStatus = string(*toolUpdate.Status)
		}
		ls.emit(event)
	case update.UsageUpdate != nil:
		// recorded once per turn via PromptResponse.Usage; the streaming
		// updates only feed UIs that show live context/cost
	}
	return nil
}

// contentText renders a message content block as transcript markdown. Text
// is passed through; the other block kinds an agent may stream are rendered
// as a short reference so they are not silently lost (a link to a file the
// agent created, an embedded resource) - binary media is only noted, the
// transcript does not store it
func contentText(block acp.ContentBlock) string {
	switch {
	case block.Text != nil:
		return block.Text.Text
	case block.ResourceLink != nil:
		link := block.ResourceLink
		name := link.Name
		if link.Title != nil && *link.Title != "" {
			name = *link.Title
		}
		return resourceRef(name, link.Uri)
	case block.Resource != nil:
		resource := block.Resource.Resource
		switch {
		case resource.TextResourceContents != nil:
			return resourceRef("", resource.TextResourceContents.Uri)
		case resource.BlobResourceContents != nil:
			return resourceRef("", resource.BlobResourceContents.Uri)
		}
	case block.Image != nil:
		return "[image]"
	case block.Audio != nil:
		return "[audio]"
	}
	return ""
}

// resourceRef renders a resource reference: web links stay clickable, other
// URIs (file:// paths inside the sandbox) are shown as code
func resourceRef(name, uri string) string {
	if name == "" {
		name = uri
	}
	if strings.HasPrefix(uri, "https://") || strings.HasPrefix(uri, "http://") {
		return fmt.Sprintf("[%s](%s)", name, uri)
	}
	if name == "" {
		return ""
	}
	return "`" + name + "`"
}
