// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"testing"

	"github.com/claceio/clace/internal/testutil"
)

func TestConfig(t *testing.T) {
	c := NewServerConfig()

	// HTTP listen related settings
	testutil.AssertEqualsString(t, "http host", "127.0.0.1", c.HttpHost)
	testutil.AssertEqualsInt(t, "http port", 25223, c.HttpPort)

	// Logging related settings
	testutil.AssertEqualsString(t, "log level", "INFO", c.LogLevel)
	testutil.AssertEqualsBool(t, "console logging", false, c.ConsoleLogging)
	testutil.AssertEqualsBool(t, "file logging", true, c.FileLogging)
	testutil.AssertEqualsInt(t, "max backups", 10, c.MaxBackups)
	testutil.AssertEqualsInt(t, "max size MB", 50, c.MaxSizeMB)

	// Metadata related settings
	testutil.AssertEqualsString(t, "db connection", "sqlite:$CL_ROOT/clace.db", c.DBConnection)
	testutil.AssertEqualsBool(t, "auto upgrade", true, c.AutoUpgrade)
}
