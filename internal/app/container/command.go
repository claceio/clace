// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package container

import (
	"bytes"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/claceio/clace/internal/types"
)

type Container struct {
	ID         string `json:"ID"`
	Names      string `json:"Names"`
	Image      string `json:"Image"`
	State      string `json:"State"`
	Status     string `json:"Status"`
	PortString string `json:"Ports"`
	Port       int
}

type Image struct {
	Repository string `json:"Repository"`
}

type ContainerName string

type ImageName string

var base32encoder = base32.StdEncoding.WithPadding(base32.NoPadding)

func genLowerCaseId(name string) string {
	// The container id needs to be lower case. Use base32 to encode the name so that it can be lowercased
	return strings.ToLower(base32encoder.EncodeToString([]byte(name)))
}

func GenContainerName(appId types.AppId, contentHash string) ContainerName {
	if contentHash == "" {
		return ContainerName(fmt.Sprintf("clc-%s", appId))
	} else {
		return ContainerName(fmt.Sprintf("clc-%s-%s", appId, genLowerCaseId(contentHash)))
	}
}

func GenImageName(appId types.AppId, contentHash string) ImageName {
	if contentHash == "" {
		return ImageName(fmt.Sprintf("cli-%s", appId))
	} else {
		return ImageName(fmt.Sprintf("cli-%s-%s", appId, genLowerCaseId(contentHash)))
	}
}

type ContainerCommand struct {
	*types.Logger
}

func (c ContainerCommand) RemoveImage(config *types.SystemConfig, name ImageName) error {
	cmd := exec.Command(config.ContainerCommand, "rmi", string(name))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error removing image: %s : %s", output, err)
	}

	return nil
}

func (c ContainerCommand) BuildImage(config *types.SystemConfig, name ImageName, sourceUrl, containerFile string) error {
	c.Debug().Msgf("Building image %s from %s with %s", name, containerFile, sourceUrl)
	cmd := exec.Command(config.ContainerCommand, "build", "-t", string(name), "-f", containerFile, ".")
	cmd.Dir = sourceUrl
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error building image: %s : %s", output, err)
	}

	return nil
}

func (c ContainerCommand) RemoveContainer(config *types.SystemConfig, name ContainerName) error {
	c.Debug().Msgf("Removing container %s", name)
	cmd := exec.Command(config.ContainerCommand, "rm", string(name))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error removing image: %s : %s", output, err)
	}

	return nil
}

func (c ContainerCommand) GetContainers(config *types.SystemConfig, name ContainerName, getAll bool) ([]Container, error) {
	c.Debug().Msgf("Getting containers with name %s, getAll %t", name, getAll)
	args := []string{"ps", "--format", "json"}
	if name != "" {
		args = append(args, "--filter", fmt.Sprintf("name=%s", name))
	}

	if getAll {
		args = append(args, "--all")
	}
	cmd := exec.Command(config.ContainerCommand, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("error listing containers: %s : %s", output, err)
	}

	resp := []Container{}
	if len(output) == 0 {
		c.Debug().Msg("No containers found")
		return resp, nil
	}

	if output[0] == '[' {
		// Podman format (Names and Ports are arrays)
		type Port struct {
			// only HostPort is needed
			HostPort int `json:"host_port"`
		}

		type ContainerPodman struct {
			ID     string   `json:"ID"`
			Names  []string `json:"Names"`
			Image  string   `json:"Image"`
			State  string   `json:"State"`
			Status string   `json:"Status"`
			Ports  []Port   `json:"Ports"`
		}
		result := []ContainerPodman{}

		// JSON output (podman)
		err = json.Unmarshal(output, &result)
		if err != nil {
			return nil, err
		}

		for _, c := range result {
			port := 0
			if len(c.Ports) > 0 {
				port = c.Ports[0].HostPort
			}
			resp = append(resp, Container{
				ID:     c.ID,
				Names:  c.Names[0],
				Image:  c.Image,
				State:  c.State,
				Status: c.Status,
				Port:   port,
			})
		}
	} else if output[0] == '{' {
		// Newline separated JSON (Docker)
		decoder := json.NewDecoder(bytes.NewReader(output))
		for decoder.More() {
			var c Container
			if err := decoder.Decode(&c); err != nil {
				return nil, fmt.Errorf("error decoding container output: %v", err)
			}

			if c.PortString != "" {
				// "Ports":"127.0.0.1:55000-\u003e5000/tcp"
				_, v, ok := strings.Cut(c.PortString, ":")
				if !ok {
					return nil, fmt.Errorf("error parsing \":\" from port string: %s", c.PortString)
				}
				v, _, ok = strings.Cut(v, "-")
				if !ok {
					return nil, fmt.Errorf("error parsing \"-\" from port string: %s", v)
				}

				c.Port, err = strconv.Atoi(v)
				if err != nil {
					return nil, fmt.Errorf("error converting to int port string: %s", v)
				}
			}

			resp = append(resp, c)
		}
	} else {
		return nil, fmt.Errorf("\"%s ps\" returned unknown output: %s", config.ContainerCommand, output)
	}

	c.Debug().Msgf("Found containers: %+v", resp)
	return resp, nil
}

func (c ContainerCommand) GetContainerLogs(config *types.SystemConfig, name ContainerName) (string, error) {
	c.Debug().Msgf("Getting container logs %s", name)
	cmd := exec.Command(config.ContainerCommand, "logs", string(name))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("error getting container logs: %s : %s", output, err)
	}

	return string(output), nil
}

func (c ContainerCommand) StopContainer(config *types.SystemConfig, name ContainerName) error {
	c.Debug().Msgf("Stopping container %s", name)
	cmd := exec.Command(config.ContainerCommand, "stop", "-t", "1", string(name))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error stopping container: %s : %s", output, err)
	}

	return nil
}

func (c ContainerCommand) StartContainer(config *types.SystemConfig, name ContainerName) error {
	c.Debug().Msgf("Starting container %s", name)
	cmd := exec.Command(config.ContainerCommand, "start", string(name))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error starting container: %s : %s", output, err)
	}

	return nil
}

const LABEL_PREFIX = "io.clace."

func (c ContainerCommand) RunContainer(config *types.SystemConfig, appEntry *types.AppEntry, containerName ContainerName,
	imageName ImageName, port int64, envMap map[string]string) error {
	c.Debug().Msgf("Running container %s from image %s with port %d env %+v", containerName, imageName, port, envMap)
	publish := fmt.Sprintf("127.0.0.1::%d", port)

	args := []string{"run", "--name", string(containerName), "--detach", "--publish", publish}

	args = append(args, "--label", LABEL_PREFIX+"app.id="+string(appEntry.Id))
	if appEntry.IsDev {
		args = append(args, "--label", LABEL_PREFIX+"dev=true")
	} else {
		args = append(args, "--label", LABEL_PREFIX+"dev=false")
		args = append(args, "--label", LABEL_PREFIX+"app.version="+strconv.Itoa(appEntry.Metadata.VersionMetadata.Version))
		args = append(args, "--label", LABEL_PREFIX+"git.sha="+appEntry.Metadata.VersionMetadata.GitCommit)
		args = append(args, "--label", LABEL_PREFIX+"git.message="+appEntry.Metadata.VersionMetadata.GitMessage)
	}

	for k, v := range envMap {
		args = append(args, "--env", fmt.Sprintf("%s=%s", k, v))
	}

	args = append(args, string(imageName))

	c.Debug().Msgf("Running container with args: %v", args)
	cmd := exec.Command(config.ContainerCommand, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error running container: %s : %s", output, err)
	}

	return nil
}

func (c ContainerCommand) GetImages(config *types.SystemConfig, name ImageName) ([]Image, error) {
	c.Debug().Msgf("Getting images with name %s", name)
	args := []string{"images", "--format", "json"}
	if name != "" {
		args = append(args, string(name))
	}
	cmd := exec.Command(config.ContainerCommand, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("error listing images: %s : %s", output, err)
	}

	resp := []Image{}
	if len(output) == 0 {
		return resp, nil
	}

	if output[0] == '[' {
		// Podman format
		type ImagePodman struct {
			Id string `json:"Id"`
		}
		result := []ImagePodman{}

		// JSON output (podman)
		err = json.Unmarshal(output, &result)
		if err != nil {
			return nil, err
		}

		for _, i := range result {
			resp = append(resp, Image{
				Repository: i.Id,
			})
		}
	} else if output[0] == '{' {
		// Newline separated JSON (Docker)
		decoder := json.NewDecoder(bytes.NewReader(output))
		for decoder.More() {
			var i Image
			if err := decoder.Decode(&i); err != nil {
				return nil, fmt.Errorf("error decoding image output: %v", err)
			}

			resp = append(resp, i)
		}
	} else {
		return nil, fmt.Errorf("\"%s ps\" returned unknown output: %s", config.ContainerCommand, output)
	}

	c.Debug().Msgf("Found images: %+v", resp)
	return resp, nil
}
