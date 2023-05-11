// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"

	clserver "github.com/claceio/clace/internal/server"
	"github.com/claceio/clace/internal/utils"
)

// ServerConfig is the configuration for the Clace Server
type ServerConfig struct {
	*utils.ServerConfig
}

func NewServerConfig() (*ServerConfig, error) {
	embedConfig, err := utils.NewServerConfigEmbedded()
	if err != nil {
		return nil, err
	}
	return &ServerConfig{embedConfig}, nil
}

// Server is the instance of the Clace Server
type Server struct {
	config *ServerConfig
	server *clserver.Server
}

// NewServer creates a new instance of the Clace Server
func NewServer(config *ServerConfig) (*Server, error) {
	server, err := clserver.NewServer(config.ServerConfig)
	if err != nil {
		return nil, err
	}

	return &Server{
		config: config,
		server: server,
	}, nil
}

// Start starts the Clace Server
func (s *Server) Start() error {
	return s.server.Start()
}

// Stop stops the Clace Server
func (s *Server) Stop(ctx context.Context) error {
	return s.server.Stop(ctx)
}
