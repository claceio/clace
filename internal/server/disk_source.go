// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/openrundev/openrun/internal/system"
	"github.com/openrundev/openrun/internal/types"
)

// Disk sources: an app source, apply file or sync path which is not a git url
// is a path on the server's disk, read as the server user. Over the remote
// management surfaces (REST, MCP, the console's in-process plugins) that is
// a read primitive for any file the server can open, so disk paths are
// accepted from the unix domain socket only unless
// security.unsafe_enable_remote_disk_read is set. The decision travels on
// the context: set by the transport (apiHandler), by server-owned flows over
// directories the server manages (builder publish) and by sync runs from the
// entry's creator (SyncMetadata.RemoteCreated). Only the intake of a new path
// is gated: reloading an app whose disk source was recorded by a local
// creator stays allowed from anywhere.

// diskSourceAllowed reports whether the context may name server disk paths:
// the unsafe flag, else an explicit decision on the context, else the
// trusted marker (unix socket calls carry both; internal background
// contexts carry the marker alone)
func (s *Server) diskSourceAllowed(ctx context.Context) bool {
	if s.Config().Security.UnsafeEnableRemoteDiskRead {
		return true
	}
	if allowed, set := system.GetContextDiskSourceAccess(ctx); set {
		return allowed
	}
	return system.IsTrustedOperation(ctx)
}

// callerHasDiskAccess is diskSourceAllowed without the unsafe flag: whether
// the caller's own standing (transport or server-owned flow) grants disk
// access. Recorded on sync entries, so a later change of the flag applies to
// their runs
func callerHasDiskAccess(ctx context.Context) bool {
	if allowed, set := system.GetContextDiskSourceAccess(ctx); set {
		return allowed
	}
	return system.IsTrustedOperation(ctx)
}

// checkDiskSource refuses a disk path (anything which is not a git url or
// the no-source marker) when the context may not read the server's disk
// and the path is not under one of security.remote_disk_read_paths. An
// empty path is a disk path too: relative paths resolve against the
// server's working directory. what names the path's role in the error (app
// source, apply file, ...)
func (s *Server) checkDiskSource(ctx context.Context, path string, what string) error {
	if path == types.NO_SOURCE || system.IsGit(path) {
		return nil
	}
	if s.diskSourceAllowed(ctx) {
		return nil
	}
	if s.remoteDiskReadAllowed(path) {
		return nil
	}
	return types.CreateRequestError(fmt.Sprintf("%s %q is a path on the server's disk, which is not allowed over the remote API: "+
		"use a git url or a directory under security.remote_disk_read_paths, run the command on the server over the unix socket, "+
		"or set security.unsafe_enable_remote_disk_read", what, path), http.StatusForbidden)
}

// remoteDiskReadAllowed reports whether path is inside one of the
// security.remote_disk_read_paths directories (by default the server's own
// $OPENRUN_HOME/app_src). Both sides are made absolute with symlinks
// resolved, so a link under an allowed directory pointing elsewhere does not
// qualify. A path which cannot be resolved is not allowed
func (s *Server) remoteDiskReadAllowed(path string) bool {
	target, err := system.CleanAbsolutePath(path)
	if err != nil {
		return false
	}
	for _, root := range s.Config().Security.RemoteDiskReadPaths {
		cleanRoot, err := system.CleanAbsolutePath(os.ExpandEnv(root))
		if err != nil {
			s.Warn().Err(err).Str("path", root).Msg("invalid security.remote_disk_read_paths entry")
			continue
		}
		if inside, err := system.PathWithinDir(cleanRoot, target); err == nil && inside {
			return true
		}
	}
	return false
}
