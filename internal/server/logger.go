// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"io"
	"os"
	"path"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

type Logger struct {
	*zerolog.Logger
}

func NewLogger(config *LogConfig) *Logger {
	var writers []io.Writer
	if config.ConsoleLogging {
		writers = append(writers, zerolog.ConsoleWriter{Out: os.Stderr})
	}
	if config.FileLogging {
		writers = append(writers, rollingFileLogger(config))
	}
	mw := io.MultiWriter(writers...)

	level := strings.ToUpper(config.LogLevel)
	logLevel := zerolog.InfoLevel
	switch level {
	case "DEBUG":
		logLevel = zerolog.DebugLevel
	case "WARN":
		logLevel = zerolog.WarnLevel
	case "TRACE":
		logLevel = zerolog.TraceLevel
	default:
		log.Warn().Str("level", level).Msg("Unknown log level, defaulting to INFO")
		logLevel = zerolog.InfoLevel
	}

	logger := zerolog.New(mw).Level(logLevel).With().Caller().Timestamp().Logger()
	logger.Info().Str("level", logger.GetLevel().String()).Int("maxSizeMB",
		config.MaxSizeMB).Int("backups", config.MaxBackups).Msg("Logger initialized")
	return &Logger{&logger}
}

func rollingFileLogger(config *LogConfig) io.Writer {
	dir := os.ExpandEnv("$CL_ROOT/logs")
	if err := os.MkdirAll(dir, 0744); err != nil {
		log.Error().Err(err).Str("path", dir).Msg("cannot create logging directory")
		return nil
	}

	return &lumberjack.Logger{
		Filename:   path.Join(dir, "clace.json"),
		MaxBackups: config.MaxBackups,
		MaxSize:    config.MaxSizeMB,
	}
}
