// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/openrundev/openrun/internal/system"
	"github.com/openrundev/openrun/internal/types"
	"github.com/rs/zerolog"
)

// initAccessLogger sets up the HTTP access logger (logs/access.log) when
// log.access_logging is enabled. Entries are structured JSON lines written
// with zerolog, which avoids the per-request fmt formatting and allocations
// of the chi default request logger.
func (s *Server) initAccessLogger(config *types.ServerConfig) {
	if !config.Log.AccessLogging {
		return
	}
	writer := types.RollingFileLogger(&config.Log, "access.log")
	if writer == nil {
		return
	}
	logger := zerolog.New(writer).With().Timestamp().Logger()
	s.accessLogger = &logger
}

// accessLogMiddleware logs one entry per request when access logging is
// enabled; when disabled it is a no-op passthrough. It runs inside the status
// middleware, so the request id is set and the user/app id are filled in on
// the shared request state by the auth code before the entry is written. The
// request id matches the audit events of the request.
func (s *Server) accessLogMiddleware(next http.Handler) http.Handler {
	if s.accessLogger == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)

		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		var userId, appId string
		if cs, ok := r.Context().Value(types.SHARED).(*ContextShared); ok {
			userId, appId = cs.UserId, cs.AppId
		}
		s.accessLogger.Log().
			Str("method", r.Method).
			Str("host", r.Host).
			Str("url", r.RequestURI).
			Str("proto", r.Proto).
			Str("remote", r.RemoteAddr).
			Int("status", status).
			Int("bytes", ww.BytesWritten()).
			Dur("duration", time.Since(start)).
			Str("request_id", system.GetContextRequestId(r.Context())).
			Str("user_id", userId).
			Str("app_id", appId).
			Send()
	})
}
