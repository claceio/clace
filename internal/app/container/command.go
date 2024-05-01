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

type ContainerName string

type ImageName string

var base32encoder = base32.StdEncoding.WithPadding(base32.NoPadding)

func genLowerCaseId(name string) string {
	// The container id needs to be lower case. Use base32 to encode the name so that it can be lowercased
	return strings.ToLower(base32encoder.EncodeToString([]byte(name)))
}

func GenContainerName(name string) ContainerName {
	return ContainerName("c" + genLowerCaseId(name))
}

func GenImageName(name string) ImageName {
	return ImageName("i" + genLowerCaseId(name))
}

func RemoveImage(config *types.SystemConfig, name ImageName) error {
	cmd := exec.Command(config.ContainerCommand, "rmi", string(name))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error removing image: %s : %s", output, err)
	}

	return nil
}

func BuildImage(config *types.SystemConfig, name ImageName, sourceUrl, containerFile string) error {
	cmd := exec.Command(config.ContainerCommand, "build", "-t", string(name), "-f", containerFile, ".")
	cmd.Dir = sourceUrl
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error building image: %s : %s", output, err)
	}

	return nil
}

func RemoveContainer(config *types.SystemConfig, name ContainerName) error {
	cmd := exec.Command(config.ContainerCommand, "rm", string(name))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error removing image: %s : %s", output, err)
	}

	return nil
}

func GetRunningContainers(config *types.SystemConfig, name ContainerName) ([]Container, error) {
	args := []string{"ps", "--format", "json"}
	if name != "" {
		args = append(args, "--filter", fmt.Sprintf("name=%s", name))
	}
	cmd := exec.Command(config.ContainerCommand, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("error listing containers: %s : %s", output, err)
	}

	resp := []Container{}
	if len(output) == 0 {
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
	return resp, nil
}

func StopContainer(config *types.SystemConfig, name ContainerName) error {
	cmd := exec.Command(config.ContainerCommand, "stop", "-t", "1", string(name))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error stopping container: %s : %s", output, err)
	}

	return nil
}

func RunContainer(config *types.SystemConfig, containerName ContainerName, imageName ImageName, port int64) error {
	publish := fmt.Sprintf("127.0.0.1::%d", port)
	cmd := exec.Command(config.ContainerCommand, "run", "--name", string(containerName), "--detach", "--publish", publish, string(imageName))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error running container: %s : %s", output, err)
	}

	return nil
}
