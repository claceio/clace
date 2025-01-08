// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

const FILE_SIZE_LIMIT = 50 * 1024 * 1024

func readFileCommands(fileName string) ([]string, error) {
	file, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	return readCommands(reader)
}

func readCommands(r io.Reader) ([]string, error) {
	ret := []string{}
	size := 0

	command := strings.Builder{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		size += len(line)
		if size > FILE_SIZE_LIMIT {
			return nil, fmt.Errorf("file size exceeds limit of %d bytes", FILE_SIZE_LIMIT)
		}

		line = strings.TrimSpace(line)
		if len(line) > 0 && line[0] == '#' {
			continue
		}

		if !strings.HasSuffix(line, "\\") {
			command.WriteString(" ")
			command.WriteString(line)

			commandStr := strings.TrimSpace(command.String())
			if commandStr != "" {
				ret = append(ret, commandStr)
			}
			command.Reset()
		} else {
			command.WriteString(" ")
			command.WriteString(line[:len(line)-1])
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	commandStr := strings.TrimSpace(command.String())
	if commandStr != "" {
		ret = append(ret, commandStr)
	}
	return ret, nil
}
