// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package system

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// execFixture lays out a directory of executables for allowlist tests:
// real/tool, real/mysh, link -> real, and a hardlink to tool outside the dir
func execFixture(t *testing.T) (dir, realDir, linkDir, hardlink string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix executable layout")
	}
	dir = t.TempDir()
	realDir = filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tool", "mysh"} {
		if err := os.WriteFile(filepath.Join(realDir, name), []byte("#!/bin/sh\necho "+name+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	linkDir = filepath.Join(dir, "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatal(err)
	}
	hardlink = filepath.Join(dir, "elsewhere")
	if err := os.Link(filepath.Join(realDir, "tool"), hardlink); err != nil {
		t.Fatal(err)
	}
	return dir, realDir, linkDir, hardlink
}

func resolveOK(t *testing.T, list *ExecAllowlist, command, cwd string) string {
	t.Helper()
	path, err := list.Resolve(command, cwd)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", command, err)
	}
	return path
}

func resolveDenied(t *testing.T, list *ExecAllowlist, command, cwd, want string) {
	t.Helper()
	_, err := list.Resolve(command, cwd)
	if err == nil {
		t.Fatalf("Resolve(%q): expected an error", command)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("Resolve(%q): error %q does not contain %q", command, err, want)
	}
}

func TestExecAllowlistValidation(t *testing.T) {
	bad := map[string]string{
		"":       "empty entry",
		"!":      "empty entry",
		"!*":     "not a valid entry",
		"bin/*":  "must be an absolute path",
		"bin/du": "bare command name or an absolute path",
		"/opt/[": "invalid glob",
	}
	for entry, want := range bad {
		err := ValidateExecAllowlist([]string{entry})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("entry %q: error %v, want %q", entry, err, want)
		}
	}
	good := []string{"*", "du", "/usr/bin/du", "/bin/*", "/opt/tools/**", "!/bin/*sh", " sh ", "$HOME/bin/*"}
	if err := ValidateExecAllowlist(good); err != nil {
		t.Errorf("valid entries rejected: %v", err)
	}
	if err := ValidateExecAllowlist(nil); err != nil {
		t.Errorf("empty list rejected: %v", err)
	}
}

func TestExecAllowlistEmptyAllowsNothing(t *testing.T) {
	list, err := NewExecAllowlist(nil)
	if err != nil {
		t.Fatal(err)
	}
	resolveDenied(t, list, "sh", "", "not in security.allowed_exec_commands")
}

func TestExecAllowlistBareNameAndWildcard(t *testing.T) {
	list, err := NewExecAllowlist([]string{"sh"})
	if err != nil {
		t.Fatal(err)
	}
	path := resolveOK(t, list, "sh", "")
	if !filepath.IsAbs(path) {
		t.Fatalf("resolved path %q is not absolute", path)
	}
	// The same program by its absolute path is the same file
	resolveOK(t, list, path, "")
	resolveDenied(t, list, "ls", "", "not in security.allowed_exec_commands")
	resolveDenied(t, list, "no-such-command-openrun", "", "not found")
	resolveDenied(t, list, "", "", "empty")

	all, err := NewExecAllowlist([]string{"*"})
	if err != nil {
		t.Fatal(err)
	}
	resolveOK(t, all, "ls", "")
}

func TestExecAllowlistExactPathAndHardlink(t *testing.T) {
	_, realDir, linkDir, hardlink := execFixture(t)
	list, err := NewExecAllowlist([]string{filepath.Join(linkDir, "tool")})
	if err != nil {
		t.Fatal(err)
	}
	// Listed through the symlinked directory, requested by every spelling.
	// The path to execute is the resolved target, never the symlink
	resolveOK(t, list, filepath.Join(realDir, "tool"), "")
	viaLink := resolveOK(t, list, filepath.Join(linkDir, "tool"), "")
	realTool, err := filepath.EvalSymlinks(filepath.Join(realDir, "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if viaLink != realTool {
		t.Fatalf("symlinked command resolved to %q, want the target %q", viaLink, realTool)
	}
	resolveOK(t, list, hardlink, "")
	resolveDenied(t, list, filepath.Join(realDir, "mysh"), "", "not in security.allowed_exec_commands")
}

func TestExecAllowlistGlob(t *testing.T) {
	dir, realDir, linkDir, hardlink := execFixture(t)
	// A glob over the symlinked directory: its prefix resolves to the real
	// directory, as /bin/* must on merged-usr hosts
	list, err := NewExecAllowlist([]string{filepath.Join(linkDir, "*")})
	if err != nil {
		t.Fatal(err)
	}
	resolveOK(t, list, filepath.Join(realDir, "tool"), "")
	resolveOK(t, list, filepath.Join(linkDir, "mysh"), "")
	// A glob matches the real path, so a hardlink outside the directory is
	// not covered, nor is a file in a subdirectory by a single star
	resolveDenied(t, list, hardlink, "", "not in security.allowed_exec_commands")

	sub := filepath.Join(realDir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "deep"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolveDenied(t, list, filepath.Join(sub, "deep"), "", "not in security.allowed_exec_commands")

	tree, err := NewExecAllowlist([]string{filepath.Join(dir, "**")})
	if err != nil {
		t.Fatal(err)
	}
	resolveOK(t, tree, filepath.Join(sub, "deep"), "")
	resolveOK(t, tree, hardlink, "")
}

func TestExecAllowlistNegation(t *testing.T) {
	_, realDir, _, _ := execFixture(t)
	list, err := NewExecAllowlist([]string{filepath.Join(realDir, "*"), "!" + filepath.Join(realDir, "*sh")})
	if err != nil {
		t.Fatal(err)
	}
	resolveOK(t, list, filepath.Join(realDir, "tool"), "")
	resolveDenied(t, list, filepath.Join(realDir, "mysh"), "", "excluded by security.allowed_exec_commands")

	// Exclusions apply to the wildcard too, and by file identity for exact entries
	all, err := NewExecAllowlist([]string{"*", "!" + filepath.Join(realDir, "mysh")})
	if err != nil {
		t.Fatal(err)
	}
	resolveOK(t, all, filepath.Join(realDir, "tool"), "")
	resolveDenied(t, all, filepath.Join(realDir, "mysh"), "", "excluded")
}

func TestExecAllowlistRelativeAndEnv(t *testing.T) {
	_, realDir, _, _ := execFixture(t)
	t.Setenv("EXEC_ALLOW_TEST_DIR", realDir)
	list, err := NewExecAllowlist([]string{"$EXEC_ALLOW_TEST_DIR/tool"})
	if err != nil {
		t.Fatal(err)
	}
	path := resolveOK(t, list, "./tool", realDir)
	want, err := filepath.EvalSymlinks(filepath.Join(realDir, "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if path != want {
		t.Fatalf("relative command resolved to %q, want %q", path, want)
	}
	resolveOK(t, list, "real/tool", filepath.Dir(realDir))
	resolveDenied(t, list, "./mysh", realDir, "not in security.allowed_exec_commands")
	// A relative path is taken from the server's working directory when
	// there is no cwd: the fixture is not there
	resolveDenied(t, list, "./tool", "", "cannot resolve")
}

func TestExecAllowlistRefusesOwnBinary(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	list, err := NewExecAllowlist([]string{"*"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = list.Resolve(self, "")
	if !errors.Is(err, ErrExecSelf) {
		t.Fatalf("running the server binary: %v", err)
	}
	// Listing it explicitly changes nothing
	exact, err := NewExecAllowlist([]string{self})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exact.Resolve(self, ""); !errors.Is(err, ErrExecSelf) {
		t.Fatalf("running the listed server binary: %v", err)
	}
	// Nor does reaching it through a symlink with another name
	if runtime.GOOS != "windows" {
		alias := filepath.Join(t.TempDir(), "harmless")
		if err := os.Symlink(self, alias); err != nil {
			t.Fatal(err)
		}
		if _, err := list.Resolve(alias, ""); !errors.Is(err, ErrExecSelf) {
			t.Fatalf("running the server binary via a symlink: %v", err)
		}
	}
}
