// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package container

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path"
	"slices"
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
	buildDir      string
	sourceFS      appfs.ReadableFS
	paramMap      map[string]string
	volumes       []string // Volumes to be mounted, read from the container file
	extraVolumes  []string // Extra volumes, from the app config
}

func NewContainerManager(logger *types.Logger, appEntry *types.AppEntry, containerFile string,
	systemConfig *types.SystemConfig, configPort int64, lifetime, scheme, health, buildDir string, sourceFS appfs.ReadableFS,
	paramMap map[string]string) (*Manager, error) {

	image := ""
	volumes := []string{}
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
		buildDir:      buildDir,
		sourceFS:      sourceFS,
		command:       ContainerCommand{logger},
		paramMap:      paramMap,
		volumes:       volumes,
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

func (m *Manager) GetEnvMap() (map[string]string, string) {
	paramKeys := []string{}
	for k := range m.paramMap {
		paramKeys = append(paramKeys, k)
	}
	slices.Sort(paramKeys) // Sort the keys to ensure consistent hash

	ret := map[string]string{}
	hashBuilder := strings.Builder{}
	for _, paramName := range paramKeys {
		paramVal := m.paramMap[paramName]
		// Default to string
		hashBuilder.WriteString(paramName)
		hashBuilder.WriteByte(0)
		hashBuilder.WriteString(paramVal)
		hashBuilder.WriteByte(0)
		ret[paramName] = paramVal
	}

	// Add the app path to the return map and hash
	pathValue := m.appEntry.Path
	if pathValue == "/" {
		pathValue = ""
	}
	hashBuilder.WriteString("CL_APP_PATH")
	hashBuilder.WriteByte(0)
	hashBuilder.WriteString(pathValue)
	hashBuilder.WriteByte(0)
	ret["CL_APP_PATH"] = pathValue

	return ret, hashBuilder.String()
}

func (m *Manager) createSpecFiles() ([]string, error) {
	// Create the spec files if they are not already present
	created := []string{}
	for name, data := range *m.appEntry.Metadata.SpecFiles {
		diskFile := path.Join(m.appEntry.SourceUrl, name)
		_, err := os.Stat(diskFile)
		if err != nil {
			if err = os.WriteFile(diskFile, []byte(data), 0644); err != nil {
				return nil, fmt.Errorf("error writing spec file %s: %w", diskFile, err)
			}
			created = append(created, diskFile)
		}
	}

	return created, nil
}

func (m *Manager) getVolumes() []string {
	allVolumes := append(m.volumes, m.extraVolumes...)
	slices.Sort(allVolumes)
	return slices.Compact(allVolumes)
}

func (m *Manager) createVolumes() error {
	allVolumes := m.getVolumes()
	for _, v := range allVolumes {
		volumeName := GenVolumeName(m.appEntry.Id, v)
		if !m.command.VolumeExists(m.systemConfig, volumeName) {
			err := m.command.VolumeCreate(m.systemConfig, volumeName)
			if err != nil {
				return fmt.Errorf("error creating volume %s: %w", volumeName, err)
			}
		}
	}
	return nil
}

func (m *Manager) getMountArgs() []string {
	allVolumes := m.getVolumes()
	args := []string{}
	for _, v := range allVolumes {
		volumeName := GenVolumeName(m.appEntry.Id, v)
		args = append(args, fmt.Sprintf("--mount=type=volume,source=%s,target=%s", volumeName, v))
	}
	return args
}

func (m *Manager) DevReload(dryRun bool) error {
	var imageName ImageName = ImageName(m.image)
	if imageName == "" {
		imageName = GenImageName(m.appEntry.Id, "")
	}
	containerName := GenContainerName(m.appEntry.Id, "")

	containers, err := m.command.GetContainers(m.systemConfig, containerName, false)
	if err != nil {
		return fmt.Errorf("error getting running containers: %w", err)
	}

	if dryRun {
		// The image could be rebuild in case of a dry run, without touch the container.
		// But a temp image id will have to be used to avoid conflict with the existing image.
		// Dryrun is a no-op for now for containers
		return nil
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

		_, err := m.createSpecFiles()
		if err != nil {
			return err
		}
		buildDir := path.Join(m.appEntry.SourceUrl, m.buildDir)
		err = m.command.BuildImage(m.systemConfig, imageName, buildDir, m.containerFile)
		if err != nil {
			return err
		}
		// Don't remove the spec files, it is good if they are checked into the source repo
		// Makes the app independent of changes in the spec files
	}

	_ = m.command.RemoveContainer(m.systemConfig, containerName)

	if err = m.createVolumes(); err != nil {
		// Create named volumes for the container
		return err
	}

	envMap, _ := m.GetEnvMap()
	err = m.command.RunContainer(m.systemConfig, m.appEntry, containerName, imageName, m.port, envMap, m.getMountArgs())
	if err != nil {
		return fmt.Errorf("error building image: %w", err)
	}

	containers, err = m.command.GetContainers(m.systemConfig, containerName, false)
	if err != nil {
		return fmt.Errorf("error getting running containers: %w", err)
	}
	if len(containers) == 0 {
		logs, _ := m.command.GetContainerLogs(m.systemConfig, containerName)
		return fmt.Errorf("container %s not running. Logs\n %s", containerName, logs)
	}
	m.hostPort = containers[0].Port

	if m.health != "" {
		err = m.WaitForHealth(m.GetHealthUrl())
		if err != nil {
			logs, _ := m.command.GetContainerLogs(m.systemConfig, containerName)
			return fmt.Errorf("error waiting for health: %w. Logs\n %s", err, logs)
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

func (m *Manager) ProdReload(excludeGlob []string, dryRun bool) error {
	sourceHash, err := m.sourceFS.FileHash(excludeGlob)
	if err != nil {
		return fmt.Errorf("error getting file hash: %w", err)
	}

	envMap, envHash := m.GetEnvMap()

	fullHashVal := fmt.Sprintf("%s-%s", sourceHash, envHash)
	sha := sha256.New()
	if _, err := sha.Write([]byte(fullHashVal)); err != nil {
		return err
	}
	fullHash := hex.EncodeToString(sha.Sum(nil))
	m.Debug().Msgf("Source hash %s Env hash %s Full hash %s", sourceHash, envHash, fullHash)

	var imageName ImageName = ImageName(m.image)
	if imageName == "" {
		imageName = GenImageName(m.appEntry.Id, fullHash)
	}

	containerName := GenContainerName(m.appEntry.Id, fullHash)

	containers, err := m.command.GetContainers(m.systemConfig, containerName, true)
	if err != nil {
		return fmt.Errorf("error getting running containers: %w", err)
	}

	if dryRun {
		// The image could be rebuild in case of a dry run, without touching the container.
		// But a temp image id will have to be used to avoid conflict with the existing image.
		// Dryrun is a no-op for now for containers
		return nil
	}

	if len(containers) != 0 {
		if containers[0].State != "running" {
			// This does not handle the case where volume list has changed
			m.Debug().Msgf("container state %s, starting", containers[0].State)
			err = m.command.StartContainer(m.systemConfig, containerName)
			if err != nil {
				return fmt.Errorf("error starting container: %w", err)
			}
		} else {
			// TODO handle case where image name is specified and param values change, need to restart container in that case
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
			buildDir := path.Join(tempDir, m.buildDir)
			buildErr := m.command.BuildImage(m.systemConfig, imageName, buildDir, m.containerFile)

			// Cleanup temp dir after image has been built (even if build failed)
			if err = os.RemoveAll(tempDir); err != nil {
				return fmt.Errorf("error removing temp source dir: %w", err)
			}

			if buildErr != nil {
				return fmt.Errorf("error building image: %w", buildErr)
			}
		}
	}

	if err = m.createVolumes(); err != nil {
		// Create named volumes for the container
		return err
	}

	// Start the container with newly built image
	err = m.command.RunContainer(m.systemConfig, m.appEntry, containerName, imageName, m.port, envMap, m.getMountArgs())
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
