// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/claceio/clace/internal/app/appfs"
	"github.com/claceio/clace/internal/app/container"
	"github.com/claceio/clace/internal/types"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

type ContainerState string

const (
	ContainerStateUnknown       ContainerState = "unknown"
	ContainerStateRunning       ContainerState = "running"
	ContainerStateIdleShutdown  ContainerState = "idle_shutdown"
	ContainerStateHealthFailure ContainerState = "health_failure"
)

type ContainerManager struct {
	*types.Logger
	command         container.ContainerCommand
	app             *App
	systemConfig    *types.SystemConfig
	containerFile   string
	image           string
	port            int64 // Port number within the container
	hostPort        int   // Port number on the host
	lifetime        string
	scheme          string
	health          string
	buildDir        string
	sourceFS        appfs.ReadableFS
	paramMap        map[string]string
	volumes         []string // Volumes to be mounted, read from the container file
	extraVolumes    []string // Extra volumes, from the app config
	containerConfig types.Container
	excludeGlob     []string

	// Idle shutdown related fields
	idleShutdownTicker *time.Ticker
	stateLock          sync.RWMutex
	currentState       ContainerState

	// Health check related fields
	healthCheckTicker *time.Ticker
}

func NewContainerManager(logger *types.Logger, app *App, containerFile string,
	systemConfig *types.SystemConfig, configPort int64, lifetime, scheme, health, buildDir string, sourceFS appfs.ReadableFS,
	paramMap map[string]string, containerConfig types.Container) (*ContainerManager, error) {

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

	m := &ContainerManager{
		Logger:          logger,
		app:             app,
		containerFile:   containerFile,
		image:           image,
		systemConfig:    systemConfig,
		port:            configPort,
		lifetime:        lifetime,
		scheme:          scheme,
		buildDir:        buildDir,
		sourceFS:        sourceFS,
		command:         container.ContainerCommand{Logger: logger},
		paramMap:        paramMap,
		volumes:         volumes,
		containerConfig: containerConfig,
		stateLock:       sync.RWMutex{},
		currentState:    ContainerStateUnknown,
	}

	if containerConfig.IdleShutdownSecs > 0 && (!app.IsDev || containerConfig.IdleShutdownDevApps) {
		// Start the idle shutdown check
		m.idleShutdownTicker = time.NewTicker(time.Duration(containerConfig.IdleShutdownSecs) * time.Second)
		go m.idleAppShutdown()
	}

	m.health = m.GetHealthUrl(health)
	if containerConfig.StatusCheckIntervalSecs > 0 {
		// Start the health check goroutine
		m.healthCheckTicker = time.NewTicker(time.Duration(containerConfig.StatusCheckIntervalSecs) * time.Second)
		go m.healthChecker()
	}

	excludeGlob := []string{}
	templateFiles, err := fs.Glob(sourceFS, "*.go.html")
	if err != nil {
		return nil, err
	}

	if len(templateFiles) != 0 { // a.UsesHtmlTemplate is set in initRouter, so it cannot be used here
		excludeGlob = app.codeConfig.Routing.ContainerExclude
	}
	m.excludeGlob = excludeGlob

	return m, nil
}

func (m *ContainerManager) idleAppShutdown() {
	for range m.idleShutdownTicker.C {
		idleTimeSecs := time.Now().Unix() - m.app.lastRequestTime.Load()
		if m.currentState != ContainerStateRunning || idleTimeSecs < int64(m.containerConfig.IdleShutdownSecs) {
			// Not idle
			m.Trace().Msgf("App %s not idle, last request %d seconds ago", m.app.Id, idleTimeSecs)
			continue
		}

		m.Debug().Msgf("Shutting down idle app %s after %d seconds", m.app.Id, idleTimeSecs)

		fullHash, err := m.getAppHash()
		if err != nil {
			m.Error().Err(err).Msgf("Error getting app hash for %s", m.app.Id)
			break
		}

		m.stateLock.Lock()
		m.currentState = ContainerStateIdleShutdown

		err = m.command.StopContainer(m.systemConfig, container.GenContainerName(m.app.Id, fullHash))
		if err != nil {
			m.Error().Err(err).Msgf("Error stopping idle app %s", m.app.Id)
		}
		m.stateLock.Unlock()
		if m.app.notifyClose != nil {
			// Notify the server to close the app so that it gets reinitialized on next API call
			m.app.notifyClose <- m.app.AppPathDomain()
		}
		break
	}

	m.Debug().Msgf("Idle checker stopped for app %s", m.app.Id)
}

func (m *ContainerManager) healthChecker() {
	for range m.healthCheckTicker.C {
		err := m.WaitForHealth(m.app.appConfig.Container.StatusHealthAttempts)
		if err == nil {
			continue
		}
		m.Info().Msgf("Health check failed for app %s: %s", m.app.Id, err)

		fullHash, err := m.getAppHash()
		if err != nil {
			m.Error().Err(err).Msgf("Error getting app hash for %s", m.app.Id)
			break
		}

		m.stateLock.Lock()
		m.currentState = ContainerStateHealthFailure

		err = m.command.StopContainer(m.systemConfig, container.GenContainerName(m.app.Id, fullHash))
		if err != nil {
			m.Error().Err(err).Msgf("Error stopping app %s after health failure", m.app.Id)
		}
		m.stateLock.Unlock()
		if m.app.notifyClose != nil {
			// Notify the server to close the app so that it gets reinitialized on next API call
			m.app.notifyClose <- m.app.AppPathDomain()
		}
		break
	}

	m.Debug().Msgf("Health checker stopped for app %s", m.app.Id)
}

func extractVolumes(node *parser.Node) []string {
	ret := []string{}
	for node.Next != nil {
		node = node.Next
		ret = append(ret, types.StripQuotes(node.Value))
	}
	return ret
}

func (m *ContainerManager) GetProxyUrl() string {
	return fmt.Sprintf("%s://127.0.0.1:%d", m.scheme, m.hostPort)
}

func (m *ContainerManager) GetHealthUrl(appHealthUrl string) string {
	healthUrl := m.app.appConfig.Container.HealthUrl
	if appHealthUrl != "" && appHealthUrl != "/" {
		// Health check URL is specified in the app code, use that
		healthUrl = appHealthUrl
	}

	if healthUrl == "" {
		healthUrl = "/"
	} else if healthUrl[0] != '/' {
		healthUrl = "/" + healthUrl
	}
	return healthUrl
}

func (m *ContainerManager) GetEnvMap() (map[string]string, string) {
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
	pathValue := m.app.Path
	if pathValue == "/" {
		pathValue = ""
	}
	hashBuilder.WriteString("CL_APP_PATH")
	hashBuilder.WriteByte(0)
	hashBuilder.WriteString(pathValue)
	hashBuilder.WriteByte(0)
	ret["CL_APP_PATH"] = pathValue

	// Add the port number to use into the env
	// Using PORT instead of CL_PORT since that seems to be the most common convention across apps
	hashBuilder.WriteString("PORT")
	hashBuilder.WriteByte(0)
	portStr := strconv.FormatInt(m.port, 10)
	hashBuilder.WriteString(portStr)
	hashBuilder.WriteByte(0)
	ret["PORT"] = portStr

	return ret, hashBuilder.String()
}

func (m *ContainerManager) createSpecFiles() ([]string, error) {
	// Create the spec files if they are not already present
	created := []string{}
	for name, data := range *m.app.Metadata.SpecFiles {
		diskFile := path.Join(m.app.SourceUrl, name)
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

func (m *ContainerManager) getVolumes() []string {
	allVolumes := append(m.volumes, m.extraVolumes...)
	slices.Sort(allVolumes)
	return slices.Compact(allVolumes)
}

func (m *ContainerManager) createVolumes() error {
	allVolumes := m.getVolumes()
	for _, v := range allVolumes {
		volumeName := container.GenVolumeName(m.app.Id, v)
		if !m.command.VolumeExists(m.systemConfig, volumeName) {
			err := m.command.VolumeCreate(m.systemConfig, volumeName)
			if err != nil {
				return fmt.Errorf("error creating volume %s: %w", volumeName, err)
			}
		}
	}
	return nil
}

func (m *ContainerManager) getMountArgs() []string {
	allVolumes := m.getVolumes()
	args := []string{}
	for _, v := range allVolumes {
		volumeName := container.GenVolumeName(m.app.Id, v)
		args = append(args, fmt.Sprintf("--mount=type=volume,source=%s,target=%s", volumeName, v))
	}
	return args
}

func (m *ContainerManager) DevReload(dryRun bool) error {
	var imageName container.ImageName = container.ImageName(m.image)
	if imageName == "" {
		imageName = container.GenImageName(m.app.Id, "")
	}
	containerName := container.GenContainerName(m.app.Id, "")

	containers, err := m.command.GetContainers(m.systemConfig, containerName, false)
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
		buildDir := path.Join(m.app.SourceUrl, m.buildDir)
		err = m.command.BuildImage(m.systemConfig, imageName, buildDir, m.containerFile, m.app.Metadata.ContainerArgs)
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

	m.stateLock.Lock()
	defer m.stateLock.Unlock()

	envMap, _ := m.GetEnvMap()
	err = m.command.RunContainer(m.systemConfig, m.app.AppEntry, containerName,
		imageName, m.port, envMap, m.getMountArgs(), m.app.Metadata.ContainerOptions)
	if err != nil {
		return fmt.Errorf("error running container: %w", err)
	}

	containers, err = m.command.GetContainers(m.systemConfig, containerName, false)
	if err != nil {
		return fmt.Errorf("error getting running containers: %w", err)
	}
	if len(containers) == 0 {
		logs, _ := m.command.GetContainerLogs(m.systemConfig, containerName)
		return fmt.Errorf("container %s not running. Logs\n %s", containerName, logs)
	}
	m.currentState = ContainerStateRunning
	m.hostPort = containers[0].Port

	if m.health != "" {
		err = m.WaitForHealth(m.app.appConfig.Container.HealthAttemptsAfterStartup)
		if err != nil {
			logs, _ := m.command.GetContainerLogs(m.systemConfig, containerName)
			return fmt.Errorf("error waiting for health: %w. Logs\n %s", err, logs)
		}
	}

	return nil
}

func (m *ContainerManager) WaitForHealth(attempts int) error {
	client := &http.Client{
		Timeout: time.Duration(m.app.appConfig.Container.HealthTimeoutSecs) * time.Second,
	}

	var err error
	var resp *http.Response
	for attempt := 1; attempt <= attempts; attempt++ {
		resp, err = client.Get(m.GetProxyUrl() + m.health)
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

func (m *ContainerManager) getAppHash() (string, error) {
	if m.app.IsDev {
		return "", nil
	}

	sourceHash, err := m.sourceFS.FileHash(m.excludeGlob)
	if err != nil {
		return "", fmt.Errorf("error getting file hash: %w", err)
	}

	_, envHash := m.GetEnvMap()
	fullHashVal := fmt.Sprintf("%s-%s", sourceHash, envHash)
	sha := sha256.New()
	if _, err := sha.Write([]byte(fullHashVal)); err != nil {
		return "", err
	}
	fullHash := hex.EncodeToString(sha.Sum(nil))
	m.Debug().Msgf("Source hash %s Env hash %s Full hash %s", sourceHash, envHash, fullHash)
	return fullHash, nil
}

func (m *ContainerManager) ProdReload(dryRun bool) error {
	fullHash, err := m.getAppHash()
	if err != nil {
		return err
	}

	var imageName container.ImageName = container.ImageName(m.image)
	if imageName == "" {
		imageName = container.GenImageName(m.app.Id, fullHash)
	}

	containerName := container.GenContainerName(m.app.Id, fullHash)

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
		m.stateLock.Lock()
		defer m.stateLock.Unlock()

		if containers[0].State != "running" {
			// This does not handle the case where volume list has changed
			m.Debug().Msgf("container state %s, starting", containers[0].State)
			err = m.command.StartContainer(m.systemConfig, containerName)
			if err != nil {
				return fmt.Errorf("error starting container: %w", err)
			}

			// Fetch port number after starting the container
			containers, err = m.command.GetContainers(m.systemConfig, containerName, true)
			if err != nil {
				return fmt.Errorf("error getting running containers: %w", err)
			}
			m.hostPort = containers[0].Port

			if m.health != "" {
				err = m.WaitForHealth(m.app.appConfig.Container.HealthAttemptsAfterStartup)
				if err != nil {
					return fmt.Errorf("error waiting for health: %w", err)
				}
			}
		} else {
			// TODO handle case where image name is specified and param values change, need to restart container in that case
			m.hostPort = containers[0].Port
			m.Debug().Msg("container already running")
		}

		m.currentState = ContainerStateRunning
		m.Debug().Msgf("updating port to %d", m.hostPort)
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
			buildErr := m.command.BuildImage(m.systemConfig, imageName, buildDir, m.containerFile, m.app.Metadata.ContainerArgs)

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

	envMap, _ := m.GetEnvMap()

	m.stateLock.Lock()
	defer m.stateLock.Unlock()
	// Start the container with newly built image
	err = m.command.RunContainer(m.systemConfig, m.app.AppEntry, containerName,
		imageName, m.port, envMap, m.getMountArgs(), m.app.Metadata.ContainerOptions)
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
	m.currentState = ContainerStateRunning
	m.hostPort = containers[0].Port

	if m.health != "" {
		err = m.WaitForHealth(m.app.appConfig.Container.HealthAttemptsAfterStartup)
		if err != nil {
			return fmt.Errorf("error waiting for health: %w", err)
		}
	}

	return nil
}

func (m *ContainerManager) Close() error {
	m.Debug().Msgf("Closing container manager for app %s", m.app.Id)
	if m.idleShutdownTicker != nil {
		m.idleShutdownTicker.Stop()
	}

	if m.healthCheckTicker != nil {
		m.healthCheckTicker.Stop()
	}
	return nil
}
