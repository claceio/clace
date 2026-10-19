// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package system

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
)

// ExecAllowlist decides which host executables the exec plugin may run, from
// the security.allowed_exec_commands server config. The requested command is
// resolved to the real file (PATH lookup, then symlinks) and every decision is
// made on that file, never on the string the app passed, so a renamed symlink
// or a relative path cannot stand in for a different program.
//
// Entry forms:
//   - "*" allows every executable
//   - a bare name ("du") is looked up on the server's PATH
//   - an absolute path ("/usr/local/bin/ffmpeg")
//   - a glob ("/bin/*", "/opt/tools/**"): doublestar syntax, where * does
//     not cross a path separator and ** does. The literal directory prefix is
//     resolved through symlinks, so "/bin/*" also covers /usr/bin on
//     merged-usr Linux where /bin is a symlink
//   - a "!" prefix makes an exclusion ("!/bin/*sh"), applied after the allow
//     entries, so a broad glob can carve interpreters back out
//
// Exact entries match the real file (os.SameFile, so hardlinks count); globs
// match the real path. Environment variables in entries are expanded. The
// OpenRun server binary itself is always refused, whatever the list says.
type ExecAllowlist struct {
	allowAll bool
	allow    []execAllowEntry
	deny     []execAllowEntry
}

type execAllowEntry struct {
	value  string // expanded entry text, without the "!" prefix
	isGlob bool
}

// isGlobPattern reports whether the entry uses glob syntax
func isGlobPattern(entry string) bool {
	return strings.ContainsAny(entry, "*?[{")
}

// NewExecAllowlist parses and validates the security.allowed_exec_commands
// entries. An empty list allows nothing
func NewExecAllowlist(entries []string) (*ExecAllowlist, error) {
	list := &ExecAllowlist{}
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		negate := strings.HasPrefix(entry, "!")
		if negate {
			entry = strings.TrimSpace(entry[1:])
		}
		if entry == "" {
			return nil, fmt.Errorf("security.allowed_exec_commands: empty entry %q", raw)
		}
		if entry == "*" {
			if negate {
				return nil, fmt.Errorf("security.allowed_exec_commands: %q is not a valid entry", raw)
			}
			list.allowAll = true
			continue
		}
		entry = os.ExpandEnv(entry)
		parsed := execAllowEntry{value: entry, isGlob: isGlobPattern(entry)}
		if parsed.isGlob {
			if !filepath.IsAbs(entry) {
				return nil, fmt.Errorf("security.allowed_exec_commands: glob %q must be an absolute path", raw)
			}
			if !doublestar.ValidatePathPattern(entry) {
				return nil, fmt.Errorf("security.allowed_exec_commands: invalid glob %q", raw)
			}
		} else if strings.ContainsRune(entry, '/') || strings.ContainsRune(entry, os.PathSeparator) {
			if !filepath.IsAbs(entry) {
				return nil, fmt.Errorf("security.allowed_exec_commands: %q must be a bare command name or an absolute path", raw)
			}
		}
		if negate {
			list.deny = append(list.deny, parsed)
		} else {
			list.allow = append(list.allow, parsed)
		}
	}
	return list, nil
}

// ValidateExecAllowlist reports whether the entries parse, for config
// validation at startup and on dynamic config updates
func ValidateExecAllowlist(entries []string) error {
	_, err := NewExecAllowlist(entries)
	return err
}

var (
	selfExecutableOnce sync.Once
	selfExecutableInfo os.FileInfo
)

// selfExecutable returns the file info of the running binary (symlinks
// resolved), nil if it cannot be determined
func selfExecutable() os.FileInfo {
	selfExecutableOnce.Do(func() {
		path, err := os.Executable()
		if err != nil {
			return
		}
		if real, err := filepath.EvalSymlinks(path); err == nil {
			path = real
		}
		info, err := os.Stat(path)
		if err != nil {
			return
		}
		selfExecutableInfo = info
	})
	return selfExecutableInfo
}

// ErrExecSelf is returned when an app tries to run the OpenRun server binary
var ErrExecSelf = errors.New("the OpenRun binary cannot be run through exec.in; use the openrun_admin plugin for management operations")

// Resolve checks that the command may run and returns the path to execute. A
// bare name is looked up on the server's PATH; a relative path is taken
// relative to cwd (the server's working directory when cwd is empty). The
// returned path is the symlink-resolved file the decision was made on, so a
// link retargeted between the check and the exec cannot swap in another
// program; the caller keeps the requested name as argv[0]
func (l *ExecAllowlist) Resolve(command, cwd string) (string, error) {
	if command == "" {
		return "", errors.New("exec: command path is empty")
	}
	lookedUp, err := lookupCommand(command, cwd)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(lookedUp)
	if err != nil {
		return "", fmt.Errorf("exec: cannot resolve command %q: %w", command, err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", fmt.Errorf("exec: cannot stat command %q: %w", command, err)
	}

	if self := selfExecutable(); self != nil && os.SameFile(self, info) {
		return "", ErrExecSelf
	}

	if !l.allowAll && !matchesAnyExecEntry(l.allow, real, info) {
		return "", fmt.Errorf("exec: command %q (%s) is not in security.allowed_exec_commands", command, real)
	}
	if matchesAnyExecEntry(l.deny, real, info) {
		return "", fmt.Errorf("exec: command %q (%s) is excluded by security.allowed_exec_commands", command, real)
	}
	return real, nil
}

// lookupCommand turns the requested command into an absolute path: PATH
// lookup for a bare name, cwd-relative resolution for a relative path
func lookupCommand(command, cwd string) (string, error) {
	if filepath.Base(command) == command {
		path, err := exec.LookPath(command)
		if err != nil {
			return "", fmt.Errorf("exec: command %q not found: %w", command, err)
		}
		return path, nil
	}
	if filepath.IsAbs(command) {
		return filepath.Clean(command), nil
	}
	base := cwd
	if base == "" {
		var err error
		if base, err = os.Getwd(); err != nil {
			return "", fmt.Errorf("exec: cannot resolve relative command %q: %w", command, err)
		}
	}
	return filepath.Abs(filepath.Join(base, command))
}

// matchesAnyExecEntry reports whether the real path (and its file info) matches
// any entry: by file identity for exact entries, by glob on the real path for
// patterns. An exact entry naming a program that is not installed matches
// nothing
func matchesAnyExecEntry(entries []execAllowEntry, real string, info os.FileInfo) bool {
	for _, entry := range entries {
		if entry.isGlob {
			if globMatchesExec(entry.value, real) {
				return true
			}
			continue
		}
		target := entry.value
		if filepath.Base(target) == target {
			found, err := exec.LookPath(target)
			if err != nil {
				continue
			}
			target = found
		}
		targetReal, err := filepath.EvalSymlinks(target)
		if err != nil {
			continue
		}
		targetInfo, err := os.Stat(targetReal)
		if err != nil {
			continue
		}
		if os.SameFile(targetInfo, info) {
			return true
		}
	}
	return false
}

// globMatchesExec matches a glob entry against the real path of a command. The
// literal directory prefix of the pattern (before the first glob character) is
// resolved through symlinks first, so a pattern written against a symlinked
// directory (/bin on merged-usr Linux) matches the files it really holds
func globMatchesExec(pattern, real string) bool {
	pattern = resolveGlobPrefix(pattern)
	matched, err := doublestar.PathMatch(pattern, real)
	return err == nil && matched
}

// resolveGlobPrefix replaces the literal directory prefix of a glob with its
// symlink-resolved form
func resolveGlobPrefix(pattern string) string {
	metaIndex := strings.IndexAny(pattern, "*?[{")
	if metaIndex < 0 {
		return pattern
	}
	sep := strings.LastIndexAny(pattern[:metaIndex], `/\`)
	if sep <= 0 {
		return pattern
	}
	prefix := pattern[:sep]
	resolved, err := filepath.EvalSymlinks(prefix)
	if err != nil || resolved == prefix {
		return pattern
	}
	return resolved + pattern[sep:]
}
