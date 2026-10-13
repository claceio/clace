// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openrundev/openrun/internal/system"
	"github.com/openrundev/openrun/internal/types"
	"github.com/rs/zerolog"
)

// TestAccessLogIdentity checks the access log entry carries the request id
// and the user/app id the auth code records on the shared request state
// after the access log middleware has passed the request on
func TestAccessLogIdentity(t *testing.T) {
	server := newBenchServer("example.com")
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	server.accessLogger = &logger

	var requestId string
	app := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestId = system.GetContextRequestId(r.Context())
		if strings.HasPrefix(r.URL.Path, "/app") {
			cs := r.Context().Value(types.SHARED).(*ContextShared)
			cs.UserId = "builtin:alice"
			cs.AppId = "app_prd_abc"
		}
		w.WriteHeader(http.StatusOK)
	})
	handler := server.handleStatus("")(server.accessLogMiddleware(app))

	tests := []struct {
		path, userId, appId string
	}{
		{"/app/x", "builtin:alice", "app_prd_abc"},
		{"/static/x", "", ""}, // unauthenticated, no app
	}
	for _, tc := range tests {
		buf.Reset()
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://example.com"+tc.path, nil))
		var entry map[string]any
		if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
			t.Fatalf("%s: invalid access log entry %q: %v", tc.path, buf.String(), err)
		}
		if requestId == "" || entry["request_id"] != requestId {
			t.Errorf("%s: request_id = %v, want %q", tc.path, entry["request_id"], requestId)
		}
		if entry["user_id"] != tc.userId {
			t.Errorf("%s: user_id = %v, want %q", tc.path, entry["user_id"], tc.userId)
		}
		if entry["app_id"] != tc.appId {
			t.Errorf("%s: app_id = %v, want %q", tc.path, entry["app_id"], tc.appId)
		}
	}
}
