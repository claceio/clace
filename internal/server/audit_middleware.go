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

	if _, err := s.auditDB.Exec(`create table IF NOT EXISTS audit (rid text, app_id text, create_time timestamp,` +
		`user_id text, event_type text, operation text, target text, status text, detail text)`); err != nil {
		return err
	}
	if _, err := s.auditDB.Exec(`create index IF NOT EXISTS idx_rid_audit ON audit (rid, create_time DESC)`); err != nil {
		return err
	}
	if _, err := s.auditDB.Exec(`create index IF NOT EXISTS idx_misc_audit ON audit (app_id, event_type, operation, target, create_time DESC)`); err != nil {
		return err
	}

	cleanupTicker := time.NewTicker(5 * time.Minute)
	go s.auditCleanup(cleanupTicker)

	return nil
}

func (s *Server) insertAuditEvent(event *types.AuditEvent) error {
	_, err := s.auditDB.Exec(`insert into audit (rid, app_id, create_time, user_id, event_type, operation, target, status, detail) `+
		`values (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.RequestId, event.AppId, event.CreateTime, event.UserId, event.EventType, event.Operation, event.Target, event.Status, event.Detail)
	return err
}

func (s *Server) InsertAuditEvent(event *types.AuditEvent) error {
	_, err := s.auditDB.Exec(`insert into audit (rid, app_id, create_time, user_id, event_type, operation, target, status, detail) `+
		`values (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.RequestId, event.AppId, event.CreateTime, event.UserId, event.EventType, event.Operation, event.Target, event.Status, event.Detail)
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
	UserId string
	AppId  string
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

		event := types.AuditEvent{
			RequestId:  rid,
			CreateTime: time.Now(),
			UserId:     contextShared.UserId,
			AppId:      types.AppId(contextShared.AppId),
			EventType:  types.EventTypeHTTP,
			Operation:  r.Method,
			Target:     r.Host + ":" + r.URL.Path,
			Status:     fmt.Sprintf("%d", crw.statusCode),
			Detail:     fmt.Sprintf("%s %s %s %d %d", r.Method, r.Host, r.URL.Path, crw.statusCode, duration.Milliseconds()),
		}

		if err := server.insertAuditEvent(&event); err != nil {
			server.Error().Err(err).Msg("error inserting audit event")
		}
	})
}
