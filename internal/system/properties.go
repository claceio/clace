// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package system

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
)

func LoadProperties(filename string) (map[string]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	ret := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if hash := strings.Index(line, "#"); hash >= 0 {
			line = strings.TrimSpace(line[0:hash])
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		ret[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return ret, nil
}

// propertiesSecretProvider is a secret provider that reads secrets from a properties file ( a = b format)
type propertiesSecretProvider struct {
	props map[string]string
}

func (e *propertiesSecretProvider) Configure(ctx context.Context, conf map[string]any) error {
	file_name, err := getConfigString(conf, "file_name")
	if err != nil {
		return fmt.Errorf("properties secret invalid config: %w", err)
	}

	e.props, err = LoadProperties(file_name)
	if err != nil {
		return fmt.Errorf("properties secret failed to load properties file: %w", err)
	}
	return nil
}

func (e *propertiesSecretProvider) GetSecret(ctx context.Context, secretName string) (string, error) {
	v, ok := e.props[secretName]
	if !ok {
		return "", fmt.Errorf("key %s not found in secret properties", secretName)
	}
	return v, nil
}

func (e *propertiesSecretProvider) GetJoinDelimiter() string {
	return "."
}

var _ secretProvider = &propertiesSecretProvider{}

func FileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil // any error is treated as not exists
}
