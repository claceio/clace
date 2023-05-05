// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"os"

	"github.com/claceio/clace/internal/utils"
	"github.com/rs/zerolog"
)

func TestLogger() *utils.Logger {
	consoleWriter := zerolog.ConsoleWriter{Out: os.Stderr}
	logger := zerolog.New(consoleWriter).Level(zerolog.DebugLevel).With().Caller().Timestamp().Logger()
	return &utils.Logger{Logger: &logger}
}
