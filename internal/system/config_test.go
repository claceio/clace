// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package system

import (
	"testing"

	"github.com/claceio/clace/internal/testutil"
)

func TestServerConfig(t *testing.T) {
	c, err := NewServerConfigEmbedded()
	if err != nil {
		t.Fatalf("failed to load embedded config: %v", err)
	}

	// The default value are read from the embedded clace.default.toml file,
	// verify if the expected values are read correctly

	// HTTP listen related settings
	testutil.AssertEqualsString(t, "http host", "127.0.0.1", c.Http.Host)
	testutil.AssertEqualsInt(t, "http port", 25222, c.Http.Port)

	// Logging related settings
	testutil.AssertEqualsString(t, "log level", "INFO", c.Log.Level)
	testutil.AssertEqualsBool(t, "console logging", false, c.Log.Console)
	testutil.AssertEqualsBool(t, "file logging", true, c.Log.File)
	testutil.AssertEqualsInt(t, "max backups", 10, c.Log.MaxBackups)
	testutil.AssertEqualsInt(t, "max size MB", 50, c.Log.MaxSizeMB)

	// Metadata related settings
	testutil.AssertEqualsString(t, "db connection", "sqlite:$CL_HOME/clace.db", c.Metadata.DBConnection)
	testutil.AssertEqualsBool(t, "auto upgrade", true, c.Metadata.AutoUpgrade)

	// HTTPS listen related settings
	testutil.AssertEqualsString(t, "https host", "0.0.0.0", c.Https.Host)
	testutil.AssertEqualsInt(t, "https port", 25223, c.Https.Port)
	testutil.AssertEqualsBool(t, "https cert lookup", true, c.Https.EnableCertLookup)
	testutil.AssertEqualsString(t, "email", "", c.Https.ServiceEmail)
	testutil.AssertEqualsBool(t, "https staging", true, c.Https.UseStaging)
	testutil.AssertEqualsString(t, "storage", "$CL_HOME/run/certmagic", c.Https.StorageLocation)
	testutil.AssertEqualsString(t, "cache", "$CL_HOME/config/certificates", c.Https.CertLocation)

	// System settings
	testutil.AssertEqualsString(t, "tailwind command", "tailwindcss", c.System.TailwindCSSCommand)
	testutil.AssertEqualsInt(t, "file debounce", 300, c.System.FileWatcherDebounceMillis)
	testutil.AssertEqualsString(t, "node path", "", c.System.NodePath)

	// Global Settings
	testutil.AssertEqualsString(t, "server uri", "$CL_HOME/run/clace.sock", c.ServerUri)
	testutil.AssertEqualsString(t, "admin user", "admin", c.AdminUser)

	// Security Settings
	testutil.AssertEqualsBool(t, "admin tcp", false, c.Security.AdminOverTCP)
	testutil.AssertEqualsString(t, "admin password bcrypt", "", c.Security.AdminPasswordBcrypt)

	// Container Settings
	testutil.AssertEqualsString(t, "command", "auto", c.System.ContainerCommand)

	// App default Settings
	testutil.AssertEqualsString(t, "cors setting", "strict", c.AppDefaults.CORS.Setting)
}

func TestClientConfig(t *testing.T) {
	c, err := NewClientConfigEmbedded()
	if err != nil {
		t.Fatalf("failed to load embedded config: %v", err)
	}
	testutil.AssertEqualsBool(t, "cert check", false, c.Client.SkipCertCheck)
	testutil.AssertEqualsString(t, "admin password", "", c.Client.AdminPassword)
	testutil.AssertEqualsString(t, "server uri", "$CL_HOME/run/clace.sock", c.ServerUri)
	testutil.AssertEqualsString(t, "admin user", "admin", c.AdminUser)
	testutil.AssertEqualsString(t, "default format", "table", c.Client.DefaultFormat)
}
