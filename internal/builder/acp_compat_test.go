// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/openrundev/openrun/internal/types"
)

// fakeAgent is an in-process ACP agent; prompt decides each turn's outcome
type fakeAgent struct {
	prompt    func(ctx context.Context, params acp.PromptRequest) (acp.PromptResponse, error)
	cancelled chan struct{}
}

func (a *fakeAgent) Authenticate(context.Context, acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, nil
}
func (a *fakeAgent) Initialize(context.Context, acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersionNumber}, nil
}
func (a *fakeAgent) Logout(context.Context, acp.LogoutRequest) (acp.LogoutResponse, error) {
	return acp.LogoutResponse{}, nil
}
func (a *fakeAgent) Cancel(context.Context, acp.CancelNotification) error {
	close(a.cancelled)
	return nil
}
func (a *fakeAgent) CloseSession(context.Context, acp.CloseSessionRequest) (acp.CloseSessionResponse, error) {
	return acp.CloseSessionResponse{}, nil
}
func (a *fakeAgent) ListSessions(context.Context, acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	return acp.ListSessionsResponse{}, nil
}
func (a *fakeAgent) NewSession(context.Context, acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	return acp.NewSessionResponse{SessionId: "s1"}, nil
}
func (a *fakeAgent) Prompt(ctx context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
	return a.prompt(ctx, params)
}
func (a *fakeAgent) ResumeSession(context.Context, acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	return acp.ResumeSessionResponse{}, nil
}
func (a *fakeAgent) SetSessionConfigOption(context.Context, acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	return acp.SetSessionConfigOptionResponse{}, nil
}
func (a *fakeAgent) SetSessionMode(context.Context, acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, nil
}

// connectFakeAgent wires a live session to an in-process agent over pipes
func connectFakeAgent(t *testing.T, m *Manager, ls *liveSession, agent *fakeAgent) {
	t.Helper()
	agentIn, clientOut := io.Pipe()
	clientIn, agentOut := io.Pipe()
	t.Cleanup(func() {
		clientOut.Close() //nolint:errcheck
		agentOut.Close()  //nolint:errcheck
	})
	acp.NewAgentSideConnection(agent, agentOut, agentIn)
	ls.conn = acp.NewClientSideConnection(&driverClient{manager: m, session: ls}, clientOut, clientIn)
	ls.acpSessionId = "s1"
}

func collectEvents(events <-chan Event) []Event {
	var collected []Event
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return collected
			}
			collected = append(collected, event)
		default:
			return collected
		}
	}
}

func TestCheckProtocolVersion(t *testing.T) {
	if err := checkProtocolVersion("opencode", acp.ProtocolVersionNumber); err != nil {
		t.Fatalf("supported version rejected: %v", err)
	}
	// an agent that does not speak our version answers with its own latest
	// (a pre-1 draft or a newer major): the client must disconnect
	for _, version := range []acp.ProtocolVersion{0, acp.ProtocolVersionNumber + 1} {
		if err := checkProtocolVersion("opencode", version); err == nil ||
			!strings.Contains(err.Error(), "does not support ACP protocol version") {
			t.Fatalf("version %d: expected the unsupported version error, got %v", version, err)
		}
	}
}

// fakeRestorer records the restore calls and fails the configured ones
type fakeRestorer struct {
	calls      []string
	resumeErr  error
	loadErr    error
	options    []acp.SessionConfigOption
	restoring  bool // ls.restoring observed during session/load
	ls         *liveSession
	loadParams acp.LoadSessionRequest
}

func (f *fakeRestorer) ResumeSession(ctx context.Context, params acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	f.calls = append(f.calls, "resume")
	return acp.ResumeSessionResponse{ConfigOptions: f.options}, f.resumeErr
}

func (f *fakeRestorer) LoadSession(ctx context.Context, params acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	f.calls = append(f.calls, "load")
	f.loadParams = params
	f.ls.mu.Lock()
	f.restoring = f.ls.restoring
	f.ls.mu.Unlock()
	return acp.LoadSessionResponse{ConfigOptions: f.options}, f.loadErr
}

func TestRestoreSessionCapabilityGating(t *testing.T) {
	manager, _, _ := newManagerTestStore(t)
	resumeCaps := acp.AgentCapabilities{SessionCapabilities: acp.SessionCapabilities{Resume: &acp.SessionResumeCapabilities{}}}
	loadCaps := acp.AgentCapabilities{LoadSession: true}
	bothCaps := acp.AgentCapabilities{LoadSession: true, SessionCapabilities: resumeCaps.SessionCapabilities}
	failed := errors.New("unknown session")
	modelOption := selectOption("model", nil, "m1")

	cases := []struct {
		name      string
		caps      acp.AgentCapabilities
		resumeErr error
		loadErr   error
		restored  bool
		calls     string
	}{
		// agents advertising neither (older agents) must get no restore call
		{name: "none", caps: acp.AgentCapabilities{}, restored: false, calls: ""},
		{name: "resume", caps: bothCaps, restored: true, calls: "resume"},
		{name: "load only", caps: loadCaps, restored: true, calls: "load"},
		// resume was an unstable extension: a failing resume falls back to load
		{name: "resume fails", caps: bothCaps, resumeErr: failed, restored: true, calls: "resume,load"},
		{name: "resume only fails", caps: resumeCaps, resumeErr: failed, restored: false, calls: "resume"},
		{name: "load fails", caps: loadCaps, loadErr: failed, restored: false, calls: "load"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ls := newLiveSession("bld_ses_restore", "user")
			defer ls.cancel()
			restorer := &fakeRestorer{resumeErr: tc.resumeErr, loadErr: tc.loadErr, ls: ls,
				options: []acp.SessionConfigOption{modelOption}}
			restored, options := manager.restoreSession(context.Background(), ls, restorer, tc.caps, "/workspace", "s1")
			if restored != tc.restored {
				t.Fatalf("restored = %v, want %v", restored, tc.restored)
			}
			if got := strings.Join(restorer.calls, ","); got != tc.calls {
				t.Fatalf("calls = %q, want %q", got, tc.calls)
			}
			if restored != (len(options) == 1) {
				t.Fatalf("config options = %v for restored=%v", options, restored)
			}
			if strings.Contains(tc.calls, "load") {
				if !restorer.restoring {
					t.Fatal("history replayed by session/load must be suppressed while loading")
				}
				if restorer.loadParams.McpServers == nil {
					t.Fatal("session/load requires mcpServers (an empty list, not omitted)")
				}
			}
			if ls.restoring {
				t.Fatal("restoring flag left set")
			}
		})
	}
}

func TestRunTurnCancelErrorIsCancellation(t *testing.T) {
	manager, db, _ := newManagerTestStore(t)
	session := &types.BuilderSession{Id: "bld_ses_cancel_err", UserID: "user", Status: types.BuilderSessionReady}
	createManagerTestSession(t, db, session)
	ls := newLiveSession(session.Id, session.UserID)
	defer ls.cancel()

	started := make(chan struct{})
	agent := &fakeAgent{cancelled: make(chan struct{})}
	agent.prompt = func(ctx context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
		close(started)
		<-agent.cancelled
		// an older agent: the abort surfaces as an error instead of the
		// cancelled stop reason the protocol requires
		return acp.PromptResponse{}, errors.New("operation aborted")
	}
	connectFakeAgent(t, manager, ls, agent)
	events, unsubscribe := ls.subscribe()
	defer unsubscribe()

	done := make(chan struct{})
	go func() {
		manager.runTurn(ls, "build it", false)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("prompt did not reach the agent")
	}
	if err := manager.cancelTurn(ls); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("turn did not finish after cancel")
	}

	var turnDone *Event
	for _, event := range collectEvents(events) {
		if event.Kind == "error" {
			t.Fatalf("a cancelled turn must not be reported as a failure: %+v", event)
		}
		if event.Kind == "turn_done" {
			turnDone = &event
		}
	}
	if turnDone == nil || turnDone.StopReason != string(acp.StopReasonCancelled) {
		t.Fatalf("expected turn_done with the cancelled stop reason, got %+v", turnDone)
	}
	activity, err := manager.ListActivity(context.Background(), session.Id, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range activity {
		if row.Kind == "error" {
			t.Fatalf("unexpected error activity: %s", row.Content)
		}
		found = found || (row.Kind == "lifecycle" && row.Content == "turn stopped")
	}
	if !found {
		t.Fatal("expected a durable turn stopped mark")
	}
}

func TestRunTurnStoppedSessionKeepsStatus(t *testing.T) {
	manager, db, _ := newManagerTestStore(t)
	session := &types.BuilderSession{Id: "bld_ses_stopped_turn", UserID: "user", Status: types.BuilderSessionReady}
	createManagerTestSession(t, db, session)
	ls := newLiveSession(session.Id, session.UserID)

	started := make(chan struct{})
	agent := &fakeAgent{cancelled: make(chan struct{})}
	agent.prompt = func(ctx context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
		close(started)
		<-ctx.Done()
		return acp.PromptResponse{}, ctx.Err()
	}
	connectFakeAgent(t, manager, ls, agent)

	done := make(chan struct{})
	go func() {
		manager.runTurn(ls, "build it", false)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("prompt did not reach the agent")
	}
	manager.stopLive(ls, types.BuilderSessionDetached)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("turn did not finish after stop")
	}
	persisted, err := manager.GetSession(context.Background(), session.Id)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != types.BuilderSessionDetached {
		t.Fatalf("status after stopping mid-turn = %q, want detached", persisted.Status)
	}
}

func TestStopReasonNote(t *testing.T) {
	if note := stopReasonNote(acp.StopReasonEndTurn); note != "" {
		t.Fatalf("end_turn note = %q", note)
	}
	for _, reason := range []acp.StopReason{acp.StopReasonCancelled, acp.StopReasonRefusal,
		acp.StopReasonMaxTokens, acp.StopReasonMaxTurnRequests} {
		if stopReasonNote(reason) == "" {
			t.Fatalf("no note for %s", reason)
		}
	}
	// stop reasons added by newer protocol revisions are tolerated
	if note := stopReasonNote("future_reason"); note != "" {
		t.Fatalf("unknown stop reason note = %q", note)
	}
}
