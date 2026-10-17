// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openrundev/openrun/internal/app/apptype"
	"github.com/openrundev/openrun/internal/system"
	"github.com/openrundev/openrun/internal/types"
)

// Destructive confirmation for MCP tools (multi round-trip requests,
// SEP-2322): an action declared destructive=True is not run on the first
// call from a capable client. The call runs the validate pass instead and
// answers input_required with an elicitation; the client retries the same
// call with the user's answer, and the action runs only on an accepted
// answer. Shared by the per-app tools (mcp.go) and the management run_action
// tool (server/mcp.go). Clients on an older protocol, or without an
// elicitation capability, cannot complete the round trip and run at once, as
// before confirmation existed

// MCPConfirmKey is the input request id of the confirmation
const MCPConfirmKey = "confirm"

// MCPConfirmSupported reports whether the calling client can complete a
// confirmation round trip in stateless mode: protocol 2026-07-28 or later
// (the input_required retry is client-driven; older clients would need the
// server-side live elicitation bridge, which stateless mode cannot serve)
// and a declared form elicitation capability, the mode the confirmation
// uses. An elicitation capability naming no mode means form (the shape of
// clients before modes existed); a client naming url only cannot answer a
// form and runs at once, as a client without elicitation does. In stateless
// mode the SDK fills the initialize params from the _meta of each request
func MCPConfirmSupported(req *mcp.CallToolRequest) bool {
	supported, _ := MCPConfirmCapability(req)
	return supported
}

// MCPConfirmCapability is MCPConfirmSupported with a description of what
// the request declared, for the refusal message and the status tool: the
// protocol version (the _meta of a 2026-07-28 request, otherwise the
// version of the legacy initialize flow, which the server answers with
// 2025-11-25 whatever the client asked) and the elicitation capability
func MCPConfirmCapability(req *mcp.CallToolRequest) (supported bool, seen string) {
	if req == nil || req.Session == nil {
		return false, "no MCP request metadata"
	}
	iparams := req.Session.InitializeParams()
	if iparams == nil {
		return false, "no MCP request metadata"
	}
	version := "protocol " + cmp.Or(iparams.ProtocolVersion, "unknown")
	if iparams.ProtocolVersion < "2026-07-28" {
		return false, version + " through the legacy initialize flow"
	}
	if iparams.Capabilities == nil || iparams.Capabilities.Elicitation == nil {
		return false, version + " and no elicitation capability"
	}
	elicitation := iparams.Capabilities.Elicitation
	if elicitation.Form == nil && elicitation.URL != nil {
		return false, version + " and a url-only elicitation capability (the confirmation is a form)"
	}
	return true, version + " with form elicitation"
}

// MCPConfirmAnswer reports whether the call carries the answer to the
// confirmation (the retry) and whether the user accepted
func MCPConfirmAnswer(req *mcp.CallToolRequest) (answered, accepted bool) {
	if req == nil || req.Params == nil {
		return false, false
	}
	response, ok := req.Params.InputResponses[MCPConfirmKey]
	if !ok {
		return false, false
	}
	elicit, ok := response.(*mcp.ElicitResult)
	return true, ok && elicit.Action == "accept"
}

// MCPConfirmRequest is the input_required result asking the user to confirm
func MCPConfirmRequest(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		InputRequests: mcp.InputRequestMap{
			MCPConfirmKey: &mcp.ElicitParams{
				Mode:            "form",
				Message:         message,
				RequestedSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			},
		},
	}
}

// MCPDeclinedStatus is the status of a declined confirmation
const MCPDeclinedStatus = "declined by the user, no changes were made"

// MCPDeclinedResult is the result of a declined confirmation: not an error,
// nothing ran
func MCPDeclinedResult() *mcp.CallToolResult {
	return &mcp.CallToolResult{
		StructuredContent: map[string]any{"status": MCPDeclinedStatus, "confirmed": false},
		Content:           []mcp.Content{&mcp.TextContent{Text: MCPDeclinedStatus + "\n"}},
	}
}

// MCPConfirmMessage builds the confirmation prompt of an action: the action,
// its app and description, the args as name=value lines (password params
// hidden) and the status the validate pass reported
func (a *Action) MCPConfirmMessage(args map[string]jsontext.Value, previewStatus string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Confirm %s on %s", a.name, a.appPath)
	if desc := strings.TrimSpace(a.description); desc != "" {
		b.WriteString(": " + desc)
	}
	b.WriteString("\n")
	if len(args) > 0 {
		b.WriteString("Arguments:\n")
		for _, name := range slices.Sorted(maps.Keys(args)) {
			value := strings.TrimSpace(string(args[name]))
			if a.isPasswordParam(name) {
				value = "<hidden>"
			}
			fmt.Fprintf(&b, "  %s=%s\n", name, value)
		}
	}
	if previewStatus = strings.TrimSpace(previewStatus); previewStatus != "" {
		b.WriteString(previewStatus + "\n")
	}
	b.WriteString("Accept to run the action, decline to leave everything unchanged.")
	return b.String()
}

func (a *Action) isPasswordParam(name string) bool {
	for _, p := range a.params {
		if p.Name == name {
			return p.DisplayType == apptype.DisplayTypePassword
		}
	}
	return false
}

// MCPConfirmMode resolves the destructive_confirm mode of a call of this
// action: the [api.mcp] setting (read from the effective config at call
// time, the setting is dynamic and the config the action was loaded with may
// be stale), overridden by the app's action.destructive_confirm, relaxed to
// skip by a credential created with --confirm skip unless the mode is
// require. byKey reports that the key did the relaxing, for the audit detail
func (a *Action) MCPConfirmMode(ctx context.Context) (mode string, byKey bool) {
	config := a.serverConfig
	if a.configSource != nil {
		config = a.configSource()
	}
	mode = types.DestructiveConfirmRequire
	if config != nil {
		mode = config.Api.MCP.DestructiveConfirmMode()
	}
	if a.config.DestructiveConfirm != "" {
		mode = a.config.DestructiveConfirm
	}
	return MCPConfirmModeForCredential(ctx, mode)
}

// MCPConfirmModeForCredential applies the --confirm skip of the calling
// credential, if any, to the mode the config resolved to: a key relaxes ask
// to skip and never overrides require
func MCPConfirmModeForCredential(ctx context.Context, mode string) (string, bool) {
	if mode == types.DestructiveConfirmRequire || mode == types.DestructiveConfirmSkip {
		return mode, false
	}
	if cred := system.GetContextApiCredential(ctx); cred != nil && cred.DestructiveConfirm == types.DestructiveConfirmSkip {
		return types.DestructiveConfirmSkip, true
	}
	return mode, false
}

// MCPConfirmAuditDetail is the confirm= audit detail of a destructive call
// which ran without a prompt: skipped by the config or by the key, or
// unsupported by the client (the ask mode)
func MCPConfirmAuditDetail(mode string, byKey bool) string {
	switch {
	case mode == types.DestructiveConfirmSkip && byKey:
		return "confirm=skipped_by_key"
	case mode == types.DestructiveConfirmSkip:
		return "confirm=skipped"
	}
	return "confirm=unsupported"
}

// MCPConfirmRefusedDetail is the audit detail of a destructive call refused
// because the client cannot confirm (the require mode)
const MCPConfirmRefusedDetail = "confirm=refused"

// MCPConfirmNeeds says what a request must carry to confirm, for the
// refusal message and the status tool
const MCPConfirmNeeds = "confirmation needs the MCP 2026-07-28 per-request metadata " +
	"(_meta io.modelcontextprotocol/protocolVersion and io.modelcontextprotocol/clientCapabilities) with a form elicitation capability"

// MCPConfirmRefusedMessage is the tool error of a destructive call refused
// in the require mode: it names the action, says what the request declared
// (seen, from MCPConfirmCapability) and what the caller can do instead
func MCPConfirmRefusedMessage(what, seen string) string {
	return what + " is destructive and this request cannot confirm it: it declared " + seen + "; " + MCPConfirmNeeds +
		". The server will not run it (destructive_confirm=require). " +
		"Pass dry_run=true to validate the arguments, use a client which can confirm, or run it from the CLI or the UI."
}

// MCPConfirmStatus describes, for the status tool of an endpoint, the
// destructive_confirm mode of the connection and whether its calls can
// confirm: the structured fields and a sentence
func MCPConfirmStatus(req *mcp.CallToolRequest, mode string, byKey bool) (map[string]any, string) {
	supported, seen := MCPConfirmCapability(req)
	fields := map[string]any{"destructive_confirm": mode, "can_confirm": supported, "confirm_capability": seen}
	if byKey {
		fields["destructive_confirm_by_key"] = true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Destructive tools: destructive_confirm=%s", mode)
	if byKey {
		b.WriteString(" (skip from the API key)")
	}
	switch {
	case mode == types.DestructiveConfirmSkip:
		b.WriteString("; they run without a confirmation prompt.")
	case supported:
		fmt.Fprintf(&b, "; this connection can confirm them (%s), each call is confirmed by the user first.", seen)
	case mode == types.DestructiveConfirmRequire:
		fmt.Fprintf(&b, "; this connection cannot confirm them (it declared %s; %s), so their calls are refused. "+
			"Pass dry_run=true to validate arguments, or use a client which can confirm.", seen, MCPConfirmNeeds)
	default:
		fmt.Fprintf(&b, "; this connection cannot confirm them (it declared %s; %s), so they run at once.", seen, MCPConfirmNeeds)
	}
	return fields, b.String()
}

// mcpAnnotations maps the hints an action declares to its tool annotations;
// nil when the action declares none, so that its tool carries no annotations
// block (clients then assume the worst, as they did before hints existed).
// read_only implies not destructive and idempotent unless declared otherwise
func mcpAnnotations(hints *types.ActionHints) *mcp.ToolAnnotations {
	if hints == nil {
		return nil
	}
	annotations := &mcp.ToolAnnotations{}
	if hints.IsReadOnly() {
		notDestructive := false
		annotations.ReadOnlyHint = true
		annotations.DestructiveHint = &notDestructive
		annotations.IdempotentHint = true
	}
	if hints.Destructive != nil {
		destructive := *hints.Destructive
		annotations.DestructiveHint = &destructive
	}
	if hints.Idempotent != nil {
		annotations.IdempotentHint = *hints.Idempotent
	}
	if hints.OpenWorld != nil {
		openWorld := *hints.OpenWorld
		annotations.OpenWorldHint = &openWorld
	}
	return annotations
}
