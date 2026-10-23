// Copyright (c) OpenRun Authors
// SPDX-License-Identifier: Apache-2.0

// Package testenv boots an openrun server from the working tree for the
// browser-driven suites under tests/ui. Each test binary starts its own
// instance on its own ports in a temp OPENRUN_HOME. With GOCOVERDIR set the
// binary is built coverage-instrumented and the server writes its coverage
// data there, the CLI invocations to the sibling client directory, matching
// tests/run_cli_tests.sh so covtest merges all of it. OPENRUN_TEST_BIN
// names a prebuilt binary to use instead of building one.
package testenv

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Env is a running openrun server
type Env struct {
	Port     int
	RepoRoot string
	HomeDir  string
	BinPath  string

	serverCmd *exec.Cmd
}

// Options customize the generated openrun.toml. The base config sets the
// admin password (AdminPass) and the http port
type Options struct {
	// HTTPSPort enables the HTTPS listener on that port (off by default)
	HTTPSPort int
	// SecurityExtra and SystemExtra are lines appended inside the [security]
	// and [system] tables; ExtraConfig is appended after them for further
	// tables ([api], [auth.*], ...)
	SecurityExtra string
	SystemExtra   string
	ExtraConfig   string
}

const AdminPass = "qwerty" // matches the bcrypt hash in the generated config

// RepoRoot returns the openrun repo root, derived from this file's location
// (tests/ui/testenv)
func RepoRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(thisFile))))
}

// TestappSource is the standard test app (tests/testapp)
func TestappSource() string {
	return filepath.Join(RepoRoot(), "tests", "testapp")
}

// Start boots a fresh server on the port. The caller must call Stop
func Start(port int, opts Options) (*Env, error) {
	if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second); err == nil {
		conn.Close() //nolint:errcheck
		return nil, fmt.Errorf("port %d is already in use, is a leftover test server running?", port)
	}
	env := &Env{Port: port, RepoRoot: RepoRoot()}
	var err error
	if env.HomeDir, err = os.MkdirTemp("", "openrun-ui-test-"); err != nil {
		return nil, err
	}
	cleanupHome := func() {
		if os.Getenv("OPENRUN_TEST_KEEP") == "" {
			os.RemoveAll(env.HomeDir) //nolint:errcheck
		} else {
			fmt.Println("keeping test home:", env.HomeDir)
		}
	}

	httpsPort := -1
	if opts.HTTPSPort > 0 {
		httpsPort = opts.HTTPSPort
	}
	config := fmt.Sprintf(`[security]
admin_password_bcrypt = "$2a$10$PMaPsOVMBfKuDG04RsqJbeKIOJjlYi1Ie1KQbPCZRQx38bqYfernm"
%s

[http]
port = %d

[https]
port = %d

[system]
container_command = "disable"
stale_container_cleanup_interval_mins = 0
%s

%s
`, opts.SecurityExtra, port, httpsPort, opts.SystemExtra, opts.ExtraConfig)
	if err := os.WriteFile(filepath.Join(env.HomeDir, "openrun.toml"), []byte(config), 0600); err != nil {
		cleanupHome()
		return nil, err
	}

	if env.BinPath = os.Getenv("OPENRUN_TEST_BIN"); env.BinPath != "" {
		if _, err := os.Stat(env.BinPath); err != nil {
			cleanupHome()
			return nil, fmt.Errorf("OPENRUN_TEST_BIN: %w", err)
		}
	} else {
		env.BinPath = filepath.Join(env.HomeDir, "openrun")
		args := []string{"build", "-o", env.BinPath}
		if os.Getenv("GOCOVERDIR") != "" {
			// atomic like the unit tests (covunit runs -race); covdata
			// rejects mixed counter modes
			args = append(args, "-cover", "-covermode=atomic")
		}
		build := exec.Command("go", append(args, "./cmd/openrun")...)
		build.Dir = env.RepoRoot
		if out, err := build.CombinedOutput(); err != nil {
			cleanupHome()
			return nil, fmt.Errorf("building openrun: %v\n%s", err, out)
		}
	}

	logFile, err := os.Create(filepath.Join(env.HomeDir, "server.log"))
	if err != nil {
		cleanupHome()
		return nil, err
	}
	env.serverCmd = exec.Command(env.BinPath, "server", "start")
	env.serverCmd.Env = env.ServerEnv()
	env.serverCmd.Stdout = logFile
	env.serverCmd.Stderr = logFile
	if err := env.serverCmd.Start(); err != nil {
		cleanupHome()
		return nil, err
	}
	if err := env.waitForServer(); err != nil {
		log, _ := os.ReadFile(filepath.Join(env.HomeDir, "server.log"))
		env.Stop()
		return nil, fmt.Errorf("%w\nserver log:\n%s", err, log)
	}
	return env, nil
}

// Stop ends the server (gracefully, so coverage data is flushed) and
// removes the temp home (kept with OPENRUN_TEST_KEEP)
func (e *Env) Stop() {
	if e.serverCmd != nil && e.serverCmd.Process != nil {
		stop := exec.Command(e.BinPath, "server", "stop")
		stop.Env = e.CLIEnv()
		_ = stop.Run()
		done := make(chan struct{})
		go func() { _ = e.serverCmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			_ = e.serverCmd.Process.Kill()
			<-done
		}
	}
	if os.Getenv("OPENRUN_TEST_KEEP") == "" {
		os.RemoveAll(e.HomeDir) //nolint:errcheck
	} else {
		fmt.Println("keeping test home:", e.HomeDir)
	}
}

// ServerEnv is the process environment of the server: the test home and
// config, coverage data to GOCOVERDIR when set
func (e *Env) ServerEnv() []string {
	return append(os.Environ(),
		"OPENRUN_HOME="+e.HomeDir,
		"CL_CONFIG_FILE="+filepath.Join(e.HomeDir, "openrun.toml"))
}

// CLIEnv is the environment for CLI invocations: like ServerEnv, with the
// client coverage data going to the sibling client directory of
// GOCOVERDIR (the run_cli_tests.sh convention) and the OS keyring off so
// stored logins land in the test home
func (e *Env) CLIEnv() []string {
	env := append(e.ServerEnv(), "OPENRUN_NO_KEYRING=1")
	if coverDir := os.Getenv("GOCOVERDIR"); coverDir != "" {
		clientDir := filepath.Join(filepath.Dir(coverDir), "client")
		_ = os.MkdirAll(clientDir, 0755)
		// Later entries win for duplicate keys
		env = append(env, "GOCOVERDIR="+clientDir)
	}
	return env
}

// CLI runs the openrun CLI against the test server (over the unix socket)
func (e *Env) CLI(args ...string) (string, error) {
	cmd := exec.Command(e.BinPath, args...)
	cmd.Env = e.CLIEnv()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (e *Env) waitForServer() error {
	for i := 0; i < 100; i++ {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", e.Port))
		if err == nil {
			resp.Body.Close() //nolint:errcheck
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for server on port %d", e.Port)
}
