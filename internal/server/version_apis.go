// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"

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
	apps, err := fileStore.GetAppVersions(ctx, tx)
	if err != nil {
		return nil, err
	}

	return &utils.AppVersionListResponse{Versions: apps}, nil
}
