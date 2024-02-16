// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"strconv"

	"github.com/claceio/clace/internal/metadata"
	"github.com/claceio/clace/internal/utils"
)

func (s *Server) VersionList(ctx context.Context, mainAppPath string) (*utils.AppVersionListResponse, error) {
	appPathDomain, err := parseAppPath(mainAppPath)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	appEntry, err := s.db.GetAppTx(ctx, tx, appPathDomain)
	if err != nil {
		return nil, err
	}

	fileStore := metadata.NewFileStore(appEntry.Id, appEntry.Metadata.VersionMetadata.Version, s.db, tx)
	versions, err := fileStore.GetAppVersions(ctx, tx)
	if err != nil {
		return nil, err
	}

	return &utils.AppVersionListResponse{Versions: versions}, nil
}

func (s *Server) VersionFiles(ctx context.Context, mainAppPath, version string) (*utils.AppVersionFilesResponse, error) {
	appPathDomain, err := parseAppPath(mainAppPath)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	appEntry, err := s.db.GetAppTx(ctx, tx, appPathDomain)
	if err != nil {
		return nil, err
	}
	var versionInt int

	if version == "" {
		versionInt = appEntry.Metadata.VersionMetadata.Version
	} else {
		versionInt, err = strconv.Atoi(version)
		if err != nil {
			return nil, err
		}
	}

	fileStore := metadata.NewFileStore(appEntry.Id, versionInt, s.db, tx)
	files, err := fileStore.GetAppFiles(ctx, tx)
	if err != nil {
		return nil, err
	}

	return &utils.AppVersionFilesResponse{Files: files}, nil
}
