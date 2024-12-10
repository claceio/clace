// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"net/http"

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

func (server *Server) handleStatus(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// Add a request id to the context
		id, err := ksuid.NewRandom()
		if err != nil {
			http.Error(w, "Error generating id"+err.Error(), http.StatusInternalServerError)
			return
		}

		ctx := r.Context()
		ctx = context.WithValue(ctx, types.REQUEST_ID, "rid_"+id.String())
		r = r.WithContext(ctx)

		// Wrap the ResponseWriter
		crw := &CustomResponseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK, // Default status
		}

		// Call the next handler
		next.ServeHTTP(crw, r)
	})
}
