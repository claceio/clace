// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package container

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/claceio/clace/internal/app/appfs"
	"github.com/claceio/clace/internal/types"
)

type Manager struct {
	*types.Logger
	appEntry      *types.AppEntry
	systemConfig  *types.SystemConfig
	containerFile string
	port          int64
	hostPort      int
	lifetime      string
	scheme        string
	health        string
	sourceFS      appfs.ReadableFS
}

func NewContainerManager(logger *types.Logger, appEntry *types.AppEntry, containerFile string,
	systemConfig *types.SystemConfig, port int64, lifetime, scheme, health string, sourceFS appfs.ReadableFS) *Manager {
	return &Manager{
		Logger:        logger,
		appEntry:      appEntry,
		containerFile: containerFile,
		systemConfig:  systemConfig,
		port:          port,
		lifetime:      lifetime,
		scheme:        scheme,
		health:        health,
		sourceFS:      sourceFS,
	}
}

func (m *Manager) GetProxyUrl() string {
	return fmt.Sprintf("%s://127.0.0.1:%d", m.scheme, m.hostPort)
}

func (m *Manager) GetHealthUrl() string {
	return fmt.Sprintf("%s://127.0.0.1:%d%s", m.scheme, m.hostPort, m.health)
}

func (m *Manager) DevReload() error {
	imageName := GenImageName(string(m.appEntry.Id))
	containerName := GenContainerName(string(m.appEntry.Id))

	containers, err := GetRunningContainers(m.systemConfig, containerName)
	if err != nil {
		return fmt.Errorf("error getting running containers: %w", err)
	}

	if len(containers) != 0 {
		err := StopContainer(m.systemConfig, containerName)
		if err != nil {
			return fmt.Errorf("error stopping container: %w", err)
		}
	}

	_ = RemoveImage(m.systemConfig, imageName)

	err = BuildImage(m.systemConfig, imageName, m.appEntry.SourceUrl, m.containerFile)
	if err != nil {
		return fmt.Errorf("error building image: %w", err)
	}

	_ = RemoveContainer(m.systemConfig, containerName)

	err = RunContainer(m.systemConfig, containerName, imageName, m.port)
	if err != nil {
		return fmt.Errorf("error building image: %w", err)
	}

	containers, err = GetRunningContainers(m.systemConfig, containerName)
	if err != nil {
		return fmt.Errorf("error getting running containers: %w", err)
	}
	if len(containers) == 0 {
		return fmt.Errorf("container not running") // todo add logs
	}
	m.hostPort = containers[0].Port

	if m.health != "" {
		err = m.WaitForHealth(m.GetHealthUrl())
		if err != nil {
			return fmt.Errorf("error waiting for health: %w", err)
		}
	}

	return nil
}

func (m *Manager) WaitForHealth(url string) error {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	var err error
	var resp *http.Response
	for attempt := 1; attempt <= 15; attempt++ {
		resp, err = client.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			return nil
		}

		if resp != nil {
			resp.Body.Close()
		}

		m.Debug().Msgf("Attempt %d failed: %s", attempt, err)
		time.Sleep(1 * time.Second)
	}
	return err
}

func (m *Manager) ProdReload() error {
	sourceHash, err := m.sourceFS.FileHash()
	if err != nil {
		return fmt.Errorf("error getting file hash: %w", err)
	}
	imageName := GenImageName(sourceHash)
	containerName := GenContainerName(sourceHash)

	containers, err := GetRunningContainers(m.systemConfig, containerName)
	if err != nil {
		return fmt.Errorf("error getting running containers: %w", err)
	}

	if len(containers) != 0 {
		m.hostPort = containers[0].Port
		m.Debug().Msg("container already running, ignoring")
		return nil
	}

	images, err := GetImages(m.systemConfig, imageName)
	if err != nil {
		return fmt.Errorf("error getting images: %w", err)
	}

	if len(images) == 0 {
		tempDir, err := m.sourceFS.CreateTempSourceDir()
		if err != nil {
			return fmt.Errorf("error creating temp source dir: %w", err)
		}

		buildErr := BuildImage(m.systemConfig, imageName, tempDir, m.containerFile)

		// Cleanup temp dir after image has been built (even if build failed)
		if err = os.RemoveAll(tempDir); err != nil {
			return fmt.Errorf("error removing temp source dir: %w", err)
		}

		if buildErr != nil {
			return fmt.Errorf("error building image: %w", buildErr)
		}
	}

	// Start the container with newly built image
	err = RunContainer(m.systemConfig, containerName, imageName, m.port)
	if err != nil {
		return fmt.Errorf("error building image: %w", err)
	}

	containers, err = GetRunningContainers(m.systemConfig, containerName)
	if err != nil {
		return fmt.Errorf("error getting running containers: %w", err)
	}
	if len(containers) == 0 {
		return fmt.Errorf("container not running") // todo add logs
	}
	m.hostPort = containers[0].Port

	if m.health != "" {
		err = m.WaitForHealth(m.GetHealthUrl())
		if err != nil {
			return fmt.Errorf("error waiting for health: %w", err)
		}
	}

	return nil
}
