// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package container

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/claceio/clace/internal/app/appfs"
	"github.com/claceio/clace/internal/types"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

type Manager struct {
	*types.Logger
	command       ContainerCommand
	appEntry      *types.AppEntry
	systemConfig  *types.SystemConfig
	containerFile string
	image         string
	port          int64
	hostPort      int
	lifetime      string
	scheme        string
	health        string
	sourceFS      appfs.ReadableFS
}

func NewContainerManager(logger *types.Logger, appEntry *types.AppEntry, containerFile string,
	systemConfig *types.SystemConfig, configPort int64, lifetime, scheme, health string, sourceFS appfs.ReadableFS) (*Manager, error) {

	image := ""
	if strings.HasPrefix(containerFile, types.CONTAINER_SOURCE_IMAGE_PREFIX) {
		// Using an image
		image = containerFile[len(types.CONTAINER_SOURCE_IMAGE_PREFIX):]
	} else {
		// Using a container file
		data, err := sourceFS.ReadFile(containerFile)
		if err != nil {
			return nil, fmt.Errorf("error reading container file %s : %w", containerFile, err)
		}

		result, err := parser.Parse(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("error parsing container file %s : %w", containerFile, err)
		}

		var filePort int64
		volumes := []string{}
		// Loop through the parsed result to find the EXPOSE and VOLUME instructions
		for _, child := range result.AST.Children {
			switch strings.ToUpper(child.Value) {
			case "EXPOSE":
				portVal, err := strconv.Atoi(strings.TrimSpace(child.Next.Value))
				if err != nil {
					return nil, fmt.Errorf("error parsing port: %w", err)
				}
				filePort = int64(portVal)
				logger.Debug().Msgf("Found EXPOSE port %d in container file %s", filePort, containerFile)
			case "VOLUME":
				v := extractVolumes(child)
				volumes = append(volumes, v...)
			}
		}

		logger.Debug().Msgf("Found volumes %v in container file %s", volumes, containerFile)

		if configPort == 0 {
			// No port configured in app config, use the one from the container file
			configPort = filePort
		}
	}

	if configPort == 0 {
		return nil, fmt.Errorf("port not specified in app config and in container file %s. Either "+
			"add a EXPOSE directive in %s or add port number in app config", containerFile, containerFile)
	}

	return &Manager{
		Logger:        logger,
		appEntry:      appEntry,
		containerFile: containerFile,
		image:         image,
		systemConfig:  systemConfig,
		port:          configPort,
		lifetime:      lifetime,
		scheme:        scheme,
		health:        health,
		sourceFS:      sourceFS,
		command:       ContainerCommand{logger},
	}, nil
}

func extractVolumes(node *parser.Node) []string {
	ret := []string{}
	for node.Next != nil {
		node = node.Next
		ret = append(ret, stripQuotes(node.Value))
	}
	return ret
}

func stripQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1]
	}
	return s
}

func (m *Manager) GetProxyUrl() string {
	return fmt.Sprintf("%s://127.0.0.1:%d", m.scheme, m.hostPort)
}

func (m *Manager) GetHealthUrl() string {
	return fmt.Sprintf("%s://127.0.0.1:%d%s", m.scheme, m.hostPort, m.health)
}

func (m *Manager) DevReload() error {
	var imageName ImageName = ImageName(m.image)
	if imageName == "" {
		imageName = GenImageName(m.appEntry.Id, "")
	}
	containerName := GenContainerName(m.appEntry.Id, "")

	containers, err := m.command.GetContainers(m.systemConfig, containerName, false)
	if err != nil {
		return fmt.Errorf("error getting running containers: %w", err)
	}

	if len(containers) != 0 {
		err := m.command.StopContainer(m.systemConfig, containerName)
		if err != nil {
			return fmt.Errorf("error stopping container: %w", err)
		}
	}

	if m.image == "" {
		// Using a container file, rebuild the image
		_ = m.command.RemoveImage(m.systemConfig, imageName)

		err = m.command.BuildImage(m.systemConfig, imageName, m.appEntry.SourceUrl, m.containerFile)
		if err != nil {
			return fmt.Errorf("error building image: %w", err)
		}
	}

	_ = m.command.RemoveContainer(m.systemConfig, containerName)

	err = m.command.RunContainer(m.systemConfig, m.appEntry, containerName, imageName, m.port)
	if err != nil {
		return fmt.Errorf("error building image: %w", err)
	}

	containers, err = m.command.GetContainers(m.systemConfig, containerName, false)
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

func (m *Manager) ProdReload(excludeGlob []string) error {
	sourceHash, err := m.sourceFS.FileHash(excludeGlob)
	if err != nil {
		return fmt.Errorf("error getting file hash: %w", err)
	}

	var imageName ImageName = ImageName(m.image)
	if imageName == "" {
		imageName = GenImageName(m.appEntry.Id, sourceHash)
	}

	containerName := GenContainerName(m.appEntry.Id, sourceHash)

	containers, err := m.command.GetContainers(m.systemConfig, containerName, true)
	if err != nil {
		return fmt.Errorf("error getting running containers: %w", err)
	}

	if len(containers) != 0 {
		if containers[0].State != "running" {
			m.Debug().Msgf("container state %s, starting", containers[0].State)
			err = m.command.StartContainer(m.systemConfig, containerName)
			if err != nil {
				return fmt.Errorf("error starting container: %w", err)
			}
		} else {
			m.Debug().Msg("container already running")
		}

		m.hostPort = containers[0].Port
		return nil
	}

	if m.image == "" {
		// Using a container file, build the image if required
		images, err := m.command.GetImages(m.systemConfig, imageName)
		if err != nil {
			return fmt.Errorf("error getting images: %w", err)
		}

		if len(images) == 0 {
			tempDir, err := m.sourceFS.CreateTempSourceDir()
			if err != nil {
				return fmt.Errorf("error creating temp source dir: %w", err)
			}

			buildErr := m.command.BuildImage(m.systemConfig, imageName, tempDir, m.containerFile)

			// Cleanup temp dir after image has been built (even if build failed)
			if err = os.RemoveAll(tempDir); err != nil {
				return fmt.Errorf("error removing temp source dir: %w", err)
			}

			if buildErr != nil {
				return fmt.Errorf("error building image: %w", buildErr)
			}
		}
	}

	// Start the container with newly built image
	err = m.command.RunContainer(m.systemConfig, m.appEntry, containerName, imageName, m.port)
	if err != nil {
		return fmt.Errorf("error building image: %w", err)
	}

	containers, err = m.command.GetContainers(m.systemConfig, containerName, false)
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
