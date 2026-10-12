// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"errors"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

func TestRequestPermissionCancelledOutcome(t *testing.T) {
	ls := newLiveSession("bld_ses_test", "user")
	ls.turnCancelled = true
	d := &driverClient{session: ls}

	resp, err := d.RequestPermission(context.Background(), acp.RequestPermissionRequest{
		Options: []acp.PermissionOption{
			{OptionId: "allow", Kind: acp.PermissionOptionKindAllowAlways},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Outcome.Cancelled == nil {
		t.Fatal("expected the cancelled outcome after session/cancel was sent")
	}
	if resp.Outcome.Selected != nil {
		t.Fatal("a cancelled turn must not auto-approve permission requests")
	}
}

func TestRequestPermissionAutoApproves(t *testing.T) {
	ls := newLiveSession("bld_ses_test", "user")
	d := &driverClient{session: ls}

	resp, err := d.RequestPermission(context.Background(), acp.RequestPermissionRequest{
		Options: []acp.PermissionOption{
			{OptionId: "once", Kind: acp.PermissionOptionKindAllowOnce},
			{OptionId: "always", Kind: acp.PermissionOptionKindAllowAlways},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Outcome.Selected == nil || resp.Outcome.Selected.OptionId != "always" {
		t.Fatalf("expected allow-always to be selected, got %+v", resp.Outcome)
	}
}

func TestToolCallUpdateTitleFallback(t *testing.T) {
	ls := newLiveSession("bld_ses_test", "user")
	ls.toolTitles["tc1"] = "Run tests"
	d := &driverClient{session: ls}

	events, cancel := ls.subscribe()
	defer cancel()

	status := acp.ToolCallStatusCompleted
	err := d.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: "s1",
		Update: acp.SessionUpdate{
			ToolCallUpdate: &acp.SessionToolCallUpdate{ToolCallId: "tc1", Status: &status},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	event := <-events
	if event.Kind != "tool_call_update" {
		t.Fatalf("expected tool_call_update event, got %s", event.Kind)
	}
	if event.Title != "Run tests" {
		t.Fatalf("expected the last known title as fallback, got %q", event.Title)
	}
	if event.ToolStatus != "completed" {
		t.Fatalf("expected completed status, got %q", event.ToolStatus)
	}
}

func TestSessionUpdateSuppressedWhileRestoring(t *testing.T) {
	ls := newLiveSession("bld_ses_test", "user")
	ls.restoring = true
	d := &driverClient{session: ls}
	events, cancel := ls.subscribe()
	defer cancel()

	// session/load replays history as updates; while restoring they must
	// not reach subscribers (the transcript already shows them)
	text := "replayed"
	err := d.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: "s1",
		Update: acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: text}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		t.Fatalf("replayed update reached subscribers: %+v", event)
	default:
	}
}

func TestSubscribeAfterSessionStopped(t *testing.T) {
	ls := newLiveSession("stopped", "user")
	defer ls.cancel()
	ls.stopped = true
	ls.closeSubscribers()
	events, unsubscribe := ls.subscribe()
	defer unsubscribe()
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("stopped subscription yielded an event")
		}
	default:
		t.Fatal("subscription after shutdown remains open")
	}
	if len(ls.subscribers) != 0 {
		t.Fatal("stopped session retained subscriber")
	}
}

func TestPickPermissionOptionRejectFallback(t *testing.T) {
	// no allow option: answer with a reject option, not the cancelled
	// outcome (which means the turn was cancelled)
	options := []acp.PermissionOption{
		{OptionId: "never", Kind: acp.PermissionOptionKindRejectAlways},
		{OptionId: "no", Kind: acp.PermissionOptionKindRejectOnce},
	}
	if got := pickPermissionOption(options); got != "no" {
		t.Fatalf("expected reject-once, got %q", got)
	}
	if got := pickPermissionOption(options[:1]); got != "never" {
		t.Fatalf("expected reject-always, got %q", got)
	}
	if got := pickPermissionOption(append(options, acp.PermissionOption{OptionId: "yes", Kind: acp.PermissionOptionKindAllowOnce})); got != "yes" {
		t.Fatalf("expected allow-once over reject, got %q", got)
	}
	if got := pickPermissionOption([]acp.PermissionOption{{OptionId: "x", Kind: "future_kind"}}); got != "" {
		t.Fatalf("expected no selection for unknown kinds, got %q", got)
	}
}

func TestUnadvertisedClientMethodsAreMethodNotFound(t *testing.T) {
	d := &driverClient{}
	_, err := d.ReadTextFile(context.Background(), acp.ReadTextFileRequest{})
	var reqErr *acp.RequestError
	if !errors.As(err, &reqErr) || reqErr.Code != -32601 {
		t.Fatalf("expected the method-not-found error, got %v", err)
	}
	_, err = d.CreateTerminal(context.Background(), acp.CreateTerminalRequest{})
	if !errors.As(err, &reqErr) || reqErr.Code != -32601 {
		t.Fatalf("expected the method-not-found error, got %v", err)
	}
}

func TestContentTextNonTextBlocks(t *testing.T) {
	title := "Readme"
	cases := []struct {
		block acp.ContentBlock
		want  string
	}{
		{acp.TextBlock("hi"), "hi"},
		{acp.ContentBlock{ResourceLink: &acp.ContentBlockResourceLink{Name: "app.star", Uri: "file:///workspace/app.star"}}, "`app.star`"},
		{acp.ContentBlock{ResourceLink: &acp.ContentBlockResourceLink{Name: "readme", Title: &title, Uri: "https://example.com/r"}}, "[Readme](https://example.com/r)"},
		{acp.ContentBlock{Resource: &acp.ContentBlockResource{Resource: acp.EmbeddedResourceResource{
			TextResourceContents: &acp.TextResourceContents{Uri: "file:///workspace/a.txt", Text: "body"}}}}, "`file:///workspace/a.txt`"},
		{acp.ContentBlock{Image: &acp.ContentBlockImage{Data: "xx", MimeType: "image/png"}}, "[image]"},
		{acp.ContentBlock{}, ""},
	}
	for _, tc := range cases {
		if got := contentText(tc.block); got != tc.want {
			t.Errorf("contentText = %q, want %q", got, tc.want)
		}
	}
}

func TestSessionUpdateToleratesOtherKinds(t *testing.T) {
	ls := newLiveSession("bld_ses_test", "user")
	d := &driverClient{session: ls}
	events, cancel := ls.subscribe()
	defer cancel()
	// update kinds the builder does not render must be accepted silently
	updates := []acp.SessionUpdate{
		{Plan: &acp.SessionUpdatePlan{}},
		{AvailableCommandsUpdate: &acp.SessionAvailableCommandsUpdate{}},
		{CurrentModeUpdate: &acp.SessionCurrentModeUpdate{}},
		{ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{}},
		{SessionInfoUpdate: &acp.SessionSessionInfoUpdate{}},
		{UsageUpdate: &acp.SessionUsageUpdate{}},
		{UserMessageChunk: &acp.SessionUpdateUserMessageChunk{Content: acp.TextBlock("echo")}},
		{},
	}
	for _, update := range updates {
		if err := d.SessionUpdate(context.Background(), acp.SessionNotification{SessionId: "s1", Update: update}); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case event := <-events:
		t.Fatalf("unexpected event %+v", event)
	default:
	}
}
