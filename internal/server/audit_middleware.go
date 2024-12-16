// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/claceio/clace/internal/system"
	"github.com/claceio/clace/internal/types"
	"github.com/segmentio/ksuid"
)

// CustomResponseWriter wraps http.ResponseWriter to capture the status code.
type CustomResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code.
func (crw *CustomResponseWriter) WriteHeader(code int) {
	crw.statusCode = code
	crw.ResponseWriter.WriteHeader(code)
}

func (s *Server) initAuditDB(connectString string) error {
	var err error
	s.auditDB, err = system.InitDB(connectString)
	if err != nil {
		return err
	}

	if err := s.versionUpgradeAuditDB(); err != nil {
		return err
	}

	cleanupTicker := time.NewTicker(5 * time.Minute)
	go s.auditCleanup(cleanupTicker)
	return nil
}

const CURRENT_AUDIT_DB_VERSION = 1

func (s *Server) versionUpgradeAuditDB() error {
	version := 0
	row := s.auditDB.QueryRow("SELECT version, last_upgraded FROM audit_version")
	var dt time.Time
	row.Scan(&version, &dt)

	if version > CURRENT_AUDIT_DB_VERSION {
		return fmt.Errorf("audit DB version is newer than server version, exiting. Server %d, DB %d", CURRENT_AUDIT_DB_VERSION, version)
	}

	if version == CURRENT_AUDIT_DB_VERSION {
		return nil
	}

	ctx := context.Background()
	tx, err := s.auditDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if version < 1 {
		s.Info().Msg("No audit version, initializing")

		if _, err := tx.ExecContext(ctx, `create table audit_version (version int, last_upgraded datetime)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `insert into audit_version values (1, datetime('now'))`); err != nil {
			return err
		}

		if _, err := tx.Exec(`create table IF NOT EXISTS audit (rid text, app_id text, create_time int,` +
			`user_id text, event_type text, operation text, target text, status text, detail text)`); err != nil {
			return err
		}

		if _, err := tx.Exec(`create index IF NOT EXISTS idx_rid_audit ON audit (rid, create_time DESC)`); err != nil {
			return err

		}
		if _, err := tx.Exec(`create index IF NOT EXISTS idx_misc_audit ON audit (app_id, event_type, operation, target, create_time DESC)`); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (s *Server) InsertAuditEvent(event *types.AuditEvent) error {
	_, err := s.auditDB.Exec(`insert into audit (rid, app_id, create_time, user_id, event_type, operation, target, status, detail) `+
		`values (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.RequestId, event.AppId, event.CreateTime.UnixNano(), event.UserId, event.EventType, event.Operation, event.Target, event.Status, event.Detail)
	return err
}

func (s *Server) cleanupEvents() error {
	// TODO: Implement cleanup
	return nil
}

func (s *Server) auditCleanup(cleanupTicker *time.Ticker) {
	err := s.cleanupEvents()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error cleaning up audit entries %s", err)
		return
	}

	for range cleanupTicker.C {
		err := s.cleanupEvents()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error cleaning up audit entries %s", err)
			break
		}
	}
	fmt.Fprintf(os.Stderr, "background audit cleanup stopped")
}

type ContextShared struct {
	UserId    string
	AppId     string
	Operation string
	Target    string
	DryRun    bool
}

func updateTargetInContext(r *http.Request, target string, dryRun bool) {
	contextShared := r.Context().Value(types.SHARED)
	if contextShared != nil {
		cs := contextShared.(*ContextShared)
		if target != "" {
			cs.Target = target
		}
		cs.DryRun = dryRun
	}
}

func updateOperationInContext(r *http.Request, operation string) {
	contextShared := r.Context().Value(types.SHARED)
	if contextShared != nil {
		cs := contextShared.(*ContextShared)
		cs.Operation = operation
	}
}

func (server *Server) handleStatus(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// Add a request id to the context
		id, err := ksuid.NewRandom()
		if err != nil {
			http.Error(w, "Error generating id"+err.Error(), http.StatusInternalServerError)
			return
		}

		contextShared := ContextShared{
			UserId: types.ADMIN_USER,
		}

		rid := "rid_" + id.String()
		ctx := r.Context()
		ctx = context.WithValue(ctx, types.REQUEST_ID, rid)
		ctx = context.WithValue(ctx, types.USER_ID, types.ADMIN_USER)
		ctx = context.WithValue(ctx, types.SHARED, &contextShared)
		r = r.WithContext(ctx)

		// Wrap the ResponseWriter
		crw := &CustomResponseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK, // Default status
		}

		startTime := time.Now()
		// Call the next handler
		next.ServeHTTP(crw, r)
		duration := time.Since(startTime)

		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			// Don't create audit events for get requests
			return
		}

		redactUrl := false
		if contextShared.AppId != "" {
			appInfo, ok := server.apps.GetAppInfo(types.AppId(contextShared.AppId))
			if !ok {
				return
			}
			app, err := server.apps.GetApp(appInfo.AppPathDomain)
			if err != nil {
				return
			}
			redactUrl = app.AppConfig.Audit.RedactUrl
		}

		path := r.URL.Path
		if redactUrl {
			path = "<REDACTED>"
		}
		event := types.AuditEvent{
			RequestId:  rid,
			CreateTime: time.Now(),
			UserId:     contextShared.UserId,
			AppId:      types.AppId(contextShared.AppId),
			EventType:  types.EventTypeHTTP,
			Operation:  r.Method,
			Target:     r.Host + ":" + path,
			Status:     fmt.Sprintf("%d", crw.statusCode),
			Detail:     fmt.Sprintf("%s %s %s %d %d", r.Method, r.Host, path, crw.statusCode, duration.Milliseconds()),
		}

		if err := server.InsertAuditEvent(&event); err != nil {
			server.Error().Err(err).Msg("error inserting audit event")
		}
	})
}
