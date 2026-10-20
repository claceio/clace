// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package system

import (
	"errors"
	"os"
	"os/exec"
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
		err := ValidateExecAllowlist([]string{entry}, nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("entry %q: error %v, want %q", entry, err, want)
		}
	}
	good := []string{"*", "du", "/usr/bin/du", "/bin/*", "/opt/tools/**", "!/bin/*sh", " sh ", "$HOME/bin/*"}
	if err := ValidateExecAllowlist(good, nil); err != nil {
		t.Errorf("valid entries rejected: %v", err)
	}
	if err := ValidateExecAllowlist(nil, nil); err != nil {
		t.Errorf("empty list rejected: %v", err)
	}
}

func TestExecAllowlistEmptyAllowsNothing(t *testing.T) {
	list, err := NewExecAllowlist(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolveDenied(t, list, "sh", "", "not in security.allowed_exec_commands")
}

func TestExecAllowlistBareNameAndWildcard(t *testing.T) {
	list, err := NewExecAllowlist([]string{"sh"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := resolveOK(t, list, "sh", "")
	if !filepath.IsAbs(path) {
		t.Fatalf("resolved path %q is not absolute", path)
	}
	// The same program by its absolute path is the same file under the same
	// name. The resolved path may carry another name (sh -> bash on some
	// distributions), which an entry of "sh" does not cover
	lookedUp, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	resolveOK(t, list, lookedUp, "")
	if filepath.Base(path) != "sh" {
		resolveDenied(t, list, path, "", "not in security.allowed_exec_commands")
	}
	resolveDenied(t, list, "ls", "", "not in security.allowed_exec_commands")
	resolveDenied(t, list, "no-such-command-openrun", "", "not found")
	resolveDenied(t, list, "", "", "empty")

	all, err := NewExecAllowlist([]string{"*"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolveOK(t, all, "ls", "")
}

func TestExecAllowlistExactPathAndHardlink(t *testing.T) {
	_, realDir, linkDir, hardlink := execFixture(t)
	list, err := NewExecAllowlist([]string{filepath.Join(linkDir, "tool")}, nil)
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
	// The same file under another name is another command of a multi-call
	// binary as far as the allowlist can tell: not covered by an exact entry
	resolveDenied(t, list, hardlink, "", "not in security.allowed_exec_commands")
	resolveDenied(t, list, filepath.Join(realDir, "mysh"), "", "not in security.allowed_exec_commands")
}

// A multi-call binary (busybox, uutils coreutils) is one file reached under
// many names, each name selecting a program. Listing one name must not admit
// the others, whichever way the entry and the request are spelled
func TestExecAllowlistMultiCallBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix executable layout")
	}
	dir := t.TempDir()
	box := filepath.Join(dir, "box")
	if err := os.WriteFile(box, []byte("#!/bin/sh\necho $0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// ls -> box (symlink, as on Alpine); sh is a hardlink (as uutils lays
	// out /usr/lib/cargo/bin/coreutils); env a symlink in another directory
	if err := os.Symlink(box, filepath.Join(dir, "ls")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(box, filepath.Join(dir, "sh")); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(box, filepath.Join(other, "env")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+other)

	for _, entry := range []string{"ls", filepath.Join(dir, "ls")} {
		list, err := NewExecAllowlist([]string{entry}, nil)
		if err != nil {
			t.Fatal(err)
		}
		resolveOK(t, list, "ls", "")
		resolveOK(t, list, filepath.Join(dir, "ls"), "")
		resolveOK(t, list, "./ls", dir)
		for _, request := range []string{"sh", "env", "box", filepath.Join(dir, "sh"), filepath.Join(other, "env"), "./sh"} {
			resolveDenied(t, list, request, dir, "not in security.allowed_exec_commands")
		}
	}

	// An exclusion is broad, matching the file or the name: excluding one
	// name of a multi-call binary excludes the file, so every name of it.
	// That is the safe direction; a separate program of another name is
	// unaffected
	solo := filepath.Join(dir, "solo")
	if err := os.WriteFile(solo, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	excluded, err := NewExecAllowlist([]string{"ls", "solo", "!sh"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolveDenied(t, excluded, "sh", "", "not in security.allowed_exec_commands")
	resolveDenied(t, excluded, "ls", "", "excluded by security.allowed_exec_commands")
	resolveOK(t, excluded, "solo", "")
	byFile, err := NewExecAllowlist([]string{"ls", "solo", "!" + box}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolveDenied(t, byFile, "ls", "", "excluded by security.allowed_exec_commands")
	resolveOK(t, byFile, "solo", "")
	// An exclusion by name needs no installed program of that name, and
	// one that matches nothing changes nothing
	byName, err := NewExecAllowlist([]string{"ls", "solo", "!no-such-command-openrun", "!solo"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolveOK(t, byName, "ls", "")
	resolveDenied(t, byName, solo, "", "excluded")
}

func TestExecEnvAllowlist(t *testing.T) {
	bad := map[string]string{
		"":        "empty entry",
		"!":       "empty entry",
		"!*":      "not a valid entry",
		"FOO=bar": "not a variable name",
		"a/b":     "not a variable name",
		"[":       "invalid pattern",
	}
	for entry, want := range bad {
		err := ValidateExecAllowlist(nil, []string{entry})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("env entry %q: error %v, want %q", entry, err, want)
		}
	}

	none, err := NewExecAllowlist(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := none.CheckEnv(nil); err != nil {
		t.Fatalf("empty env with empty list: %v", err)
	}
	if err := none.CheckEnv([]string{"FOO=1"}); err == nil || !strings.Contains(err.Error(), `"FOO" is not in security.allowed_exec_env`) {
		t.Fatalf("unlisted variable: %v", err)
	}

	list, err := NewExecAllowlist(nil, []string{"HOME", " MYAPP_* ", "!MYAPP_SECRET"})
	if err != nil {
		t.Fatal(err)
	}
	if err := list.CheckEnv([]string{"HOME=/tmp", "MYAPP_MODE=fast", "MYAPP_EMPTY=", "MYAPP_EQ=a=b"}); err != nil {
		t.Fatalf("listed variables: %v", err)
	}
	for _, env := range []string{"LD_PRELOAD=/tmp/x.so", "BASH_FUNC_date%%=() { :; }", "MYAPP=1", "=1", ""} {
		if err := list.CheckEnv([]string{"HOME=/", env}); err == nil {
			t.Errorf("env %q passed", env)
		}
	}
	if err := list.CheckEnv([]string{"MYAPP_SECRET=x"}); err == nil || !strings.Contains(err.Error(), "excluded by security.allowed_exec_env") {
		t.Fatalf("excluded variable: %v", err)
	}

	all, err := NewExecAllowlist(nil, []string{"*", "!LD_*", "!BASH_FUNC_*"})
	if err != nil {
		t.Fatal(err)
	}
	if err := all.CheckEnv([]string{"ANYTHING=1", "PATH=/bin"}); err != nil {
		t.Fatalf("wildcard: %v", err)
	}
	for _, env := range []string{"LD_PRELOAD=x", "BASH_FUNC_ls%%=x"} {
		if err := all.CheckEnv([]string{env}); err == nil || !strings.Contains(err.Error(), "excluded") {
			t.Errorf("env %q: %v", env, err)
		}
	}

	// Windows variable names are case-insensitive: an exclusion must not be
	// spelled around, and an allow entry matches any spelling. Unix names
	// are distinct
	if runtime.GOOS == "windows" {
		if err := all.CheckEnv([]string{"Ld_Preload=x"}); err == nil || !strings.Contains(err.Error(), "excluded") {
			t.Errorf("windows exclusion by other case: %v", err)
		}
		if err := list.CheckEnv([]string{"home=/", "myapp_x=1"}); err != nil {
			t.Errorf("windows allow by other case: %v", err)
		}
		if err := list.CheckEnv([]string{"myapp_secret=1"}); err == nil || !strings.Contains(err.Error(), "excluded") {
			t.Errorf("windows exclusion by other case: %v", err)
		}
	} else {
		if err := all.CheckEnv([]string{"Ld_Preload=x"}); err != nil {
			t.Errorf("unix names are case-sensitive: %v", err)
		}
		if err := list.CheckEnv([]string{"myapp_x=1"}); err == nil || !strings.Contains(err.Error(), "not in security.allowed_exec_env") {
			t.Errorf("unix allow entry by other case: %v", err)
		}
	}
	for _, name := range []string{"Path", "path", "PATH"} {
		if matchesAnyEnvPattern([]string{"PATH"}, name, false) != (name == "PATH") {
			t.Errorf("case-sensitive match of %q", name)
		}
		if !matchesAnyEnvPattern([]string{"PATH"}, name, true) || !matchesAnyEnvPattern([]string{"p*"}, name, true) {
			t.Errorf("case-folded match of %q", name)
		}
	}
}

func TestExecAllowlistGlob(t *testing.T) {
	dir, realDir, linkDir, hardlink := execFixture(t)
	// A glob over the symlinked directory: its prefix resolves to the real
	// directory, as /bin/* must on merged-usr hosts
	list, err := NewExecAllowlist([]string{filepath.Join(linkDir, "*")}, nil)
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

	tree, err := NewExecAllowlist([]string{filepath.Join(dir, "**")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolveOK(t, tree, filepath.Join(sub, "deep"), "")
	resolveOK(t, tree, hardlink, "")
}

func TestExecAllowlistNegation(t *testing.T) {
	_, realDir, _, _ := execFixture(t)
	list, err := NewExecAllowlist([]string{filepath.Join(realDir, "*"), "!" + filepath.Join(realDir, "*sh")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolveOK(t, list, filepath.Join(realDir, "tool"), "")
	resolveDenied(t, list, filepath.Join(realDir, "mysh"), "", "excluded by security.allowed_exec_commands")

	// Exclusions apply to the wildcard too, and by file identity for exact entries
	all, err := NewExecAllowlist([]string{"*", "!" + filepath.Join(realDir, "mysh")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolveOK(t, all, filepath.Join(realDir, "tool"), "")
	resolveDenied(t, all, filepath.Join(realDir, "mysh"), "", "excluded")
}

func TestExecAllowlistRelativeAndEnv(t *testing.T) {
	_, realDir, _, _ := execFixture(t)
	t.Setenv("EXEC_ALLOW_TEST_DIR", realDir)
	list, err := NewExecAllowlist([]string{"$EXEC_ALLOW_TEST_DIR/tool"}, nil)
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
	list, err := NewExecAllowlist([]string{"*"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = list.Resolve(self, "")
	if !errors.Is(err, ErrExecSelf) {
		t.Fatalf("running the server binary: %v", err)
	}
	// Listing it explicitly changes nothing
	exact, err := NewExecAllowlist([]string{self}, nil)
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
