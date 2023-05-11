// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/claceio/clace/internal/utils"
	"github.com/go-chi/chi"
	"golang.org/x/crypto/bcrypt"
)

const (
	REALM               = "clace"
	INTERNAL_URL_PREFIX = "/_clace"
)

type Handler struct {
	*utils.Logger
	config *utils.ServerConfig
	server *Server
	router *chi.Mux
}

// NewHandler creates a new handler
func NewHandler(logger *utils.Logger, config *utils.ServerConfig, server *Server) *Handler {
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rvr := recover(); rvr != nil && rvr != http.ErrAbortHandler {
					msg := fmt.Sprint(rvr)
					logger.Error().Str("recover", msg).Str("trace", string(debug.Stack())).Msg("Error during request processing")
					http.Error(w, msg, http.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(w, r)
		}
		return http.HandlerFunc(fn)
	})

	handler := &Handler{
		Logger: logger,
		config: config,
		server: server,
		router: router,
	}

	router.Use(handler.createAuthMiddleware)
	router.Mount(INTERNAL_URL_PREFIX, handler.serveInternal())
	router.Get("/*", handler.matchApp)
	return handler
}

func (h *Handler) createAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok {
			h.basicAuthFailed(w)
			return
		}

		if h.config.AdminUser == "" {
			h.Warn().Msg("No admin username specified, basic auth not available")
			h.basicAuthFailed(w)
			return
		}

		if subtle.ConstantTimeCompare([]byte(h.config.AdminUser), []byte(user)) != 1 {
			h.Warn().Msg("Admin username does not match")
			h.basicAuthFailed(w)
			return
		}

		err := bcrypt.CompareHashAndPassword([]byte(h.config.AdminPasswordBcrypt), []byte(pass))
		if err != nil {
			h.Warn().Err(err).Msg("Password match failed")
			h.basicAuthFailed(w)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (h *Handler) basicAuthFailed(w http.ResponseWriter) {
	w.Header().Add("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s"`, REALM))
	w.WriteHeader(http.StatusUnauthorized)
}

func (h *Handler) matchApp(w http.ResponseWriter, r *http.Request) {
	h.Debug().Str("method", r.Method).Str("url", r.URL.String()).Msg("App Received request")

	// TODO : handle domain based routing
	domain := ""
	paths, err := h.server.db.GetAllApps(domain)
	if err != nil {
		h.Error().Err(err).Msg("Error getting apps")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	requestPath := strings.TrimRight(r.URL.Path, "/")
	matchedPath := ""
	for _, path := range paths {
		if strings.HasPrefix(requestPath, path) {
			if len(path) == len(requestPath) || requestPath[len(path)] == '/' {
				h.Info().Str("path", path).Msg("Matched app")
				matchedPath = path
				break
			}
		}
	}

	if matchedPath == "" {
		h.Error().Msg("No app matched request")
		http.Error(w, "No matching app found", http.StatusNotFound)
		return
	}

	h.server.serveApp(w, r, matchedPath, domain)
}

func (h *Handler) serveInternal() http.Handler {
	r := chi.NewRouter()
	r.Get("/app/*", h.getApp)
	r.Post("/app/*", h.createApp)
	r.Delete("/app/*", h.deleteApp)
	return r
}

func normalizePath(path string) string {
	if len(path) == 0 || path[0] != '/' {
		path = "/" + path
	}
	return path
}

func (h *Handler) getApp(w http.ResponseWriter, r *http.Request) {
	appPath := chi.URLParam(r, "*")
	domain := r.URL.Query().Get("domain")

	appPath = normalizePath(appPath)
	app, err := h.server.GetApp(utils.CreateAppPathDomain(appPath, domain))
	if err != nil {
		h.Error().Err(err).Msg("error getting App")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	err = json.NewEncoder(w).Encode(app.AppEntry)
	if err != nil {
		h.Error().Err(err).Msg("error enoding app")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *Handler) createApp(w http.ResponseWriter, r *http.Request) {
	appPath := chi.URLParam(r, "*")
	domain := r.URL.Query().Get("domain")

	appPath = normalizePath(appPath)
	var app utils.AppEntry
	err := json.NewDecoder(r.Body).Decode(&app)
	if err != nil {
		h.Error().Err(err).Msg("Error parsing App body")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	app.Path = appPath
	app.Domain = domain
	_, err = h.server.AddApp(&app)
	if err != nil {
		h.Error().Err(err).Msg("Error adding App")
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (h *Handler) deleteApp(w http.ResponseWriter, r *http.Request) {
	appPath := chi.URLParam(r, "*")
	domain := r.URL.Query().Get("domain")

	appPath = normalizePath(appPath)
	err := h.server.DeleteApp(utils.CreateAppPathDomain(appPath, domain))
	if err != nil {
		h.Error().Err(err).Msg("Error deleting App")
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
