// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/claceio/clace/internal/metadata"
	"github.com/claceio/clace/internal/utils"
)

// CL_HOME is the root directory for Clace logs and temp files
var CL_HOME = os.ExpandEnv("$CL_HOME")

func init() {
	if len(CL_HOME) == 0 {
		// Default to current directory if CL_HOME is not set
		CL_HOME = "."
		os.Setenv("CL_HOME", CL_HOME)
	}
}

type Server struct {
	*utils.Logger
	config     *utils.ServerConfig
	db         *metadata.Metadata
	httpServer *http.Server
	handler    *Handler
}

// NewServer creates a new instance of the Clace Server
func NewServer(config *utils.ServerConfig) (*Server, error) {
	logger := utils.NewLogger(&config.Log)
	db, err := metadata.NewMetadata(logger, config)
	if err != nil {
		return nil, err
	}

	return &Server{
		Logger: logger,
		config: config,
		db:     db,
	}, nil
}

// Start starts the Clace Server
func (s *Server) Start() error {
	s.Info().Str("host", s.config.Http.Host).Int("port", s.config.Http.Port).Msg("Starting HTTP server")
	s.handler = NewHandler(s.Logger, s.config, s)
	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", s.config.Http.Host, s.config.Http.Port),
		WriteTimeout: 180 * time.Second,
		ReadTimeout:  180 * time.Second,
		IdleTimeout:  30 * time.Second,
		Handler:      s.handler.router,
	}
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil {
			s.Trace().Err(err).Msg("server")
		}
	}()
	return nil
}

// Stop stops the Clace Server
func (s *Server) Stop(ctx context.Context) error {
	s.Info().Msg("Stopping service")
	return s.httpServer.Shutdown(ctx)
}
