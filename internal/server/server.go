// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package server

import "os"

// ServerConfig is the configuration for the Clace Server
type ServerConfig struct {
	Host string
	Port int
	LogConfig
}

// LogConfig is the configuration for the Logger
type LogConfig struct {
	LogLevel       string
	MaxBackups     int
	MaxSizeMB      int
	ConsoleLogging bool
	FileLogging    bool
}

// CL_ROOT is the root directory for Clace logs and temp files
var CL_ROOT = os.ExpandEnv("$CL_ROOT")

func init() {
	if len(CL_ROOT) == 0 {
		// Default to current directory if CL_ROOT is not set
		CL_ROOT = "./"
	}
}

type Server struct {
	*Logger
	config *ServerConfig
}

// NewServer creates a new instance of the Clace Server
func NewServer(config *ServerConfig) *Server {
	logger := NewLogger(&config.LogConfig)
	return &Server{
		Logger: logger,
		config: config,
	}
}

// Start starts the Clace Server
func (s *Server) Start() error {
	return nil
}

// Stop stops the Clace Server
func (s *Server) Stop() error {
	return nil
}
