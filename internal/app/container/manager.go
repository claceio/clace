// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package container

import (
	"fmt"

	"github.com/claceio/clace/internal/types"
)

type Manager struct {
	*types.Logger
	appEntry      *types.AppEntry
	systemConfig  *types.SystemConfig
	containerFile string
	Port          int64
	HostPort      int
	lifetime      string // todo add lifetime
}

func NewContainerManager(logger *types.Logger, appEntry *types.AppEntry, containerFile string,
	systemConfig *types.SystemConfig, port int64, lifetime string) *Manager {
	return &Manager{
		Logger:        logger,
		appEntry:      appEntry,
		containerFile: containerFile,
		systemConfig:  systemConfig,
		Port:          port,
		lifetime:      lifetime,
	}
}

func (m *Manager) DevReload() error {
	id := string(m.appEntry.Id)
	containers, err := GetRunningContainers(m.systemConfig, id)
	if err != nil {
		return fmt.Errorf("error getting running containers: %w", err)
	}

	if len(containers) != 0 {
		err := StopContainer(m.systemConfig, id)
		if err != nil {
			return fmt.Errorf("error stopping container: %w", err)
		}
	}

	_ = RemoveImage(m.systemConfig, string(m.appEntry.Id))

	err = BuildImage(m.systemConfig, string(m.appEntry.Id), m.appEntry.SourceUrl, m.containerFile)
	if err != nil {
		return fmt.Errorf("error building image: %w", err)
	}

	_ = RemoveContainer(m.systemConfig, string(m.appEntry.Id))

	err = RunContainer(m.systemConfig, string(m.appEntry.Id), m.Port)
	if err != nil {
		return fmt.Errorf("error building image: %w", err)
	}

	containers, err = GetRunningContainers(m.systemConfig, id)
	if err != nil {
		return fmt.Errorf("error getting running containers: %w", err)
	}
	if len(containers) == 0 {
		return fmt.Errorf("container not running") // todo add logs
	}

	m.HostPort = containers[0].Port
	return nil
}

func (m *Manager) ProdReload() error {
	return nil
}
