// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/claceio/clace/internal/system"
	"github.com/claceio/clace/internal/types"
	"github.com/urfave/cli/v2"
)

func getClientCommands(clientConfig *types.ClientConfig) ([]*cli.Command, error) {
	defaultClientConfig, err := system.NewClientConfigEmbedded()
	if err != nil {
		return nil, err
	}

	flags := []cli.Flag{
		newAltStringFlag("server-uri", "s", "The server connection uri", defaultClientConfig.ServerUri, &clientConfig.ServerUri),
		newAltStringFlag("admin-user", "u", "The admin user name", defaultClientConfig.AdminUser, &clientConfig.AdminUser),
		newAltStringFlag("admin-password", "w", "The admin user password", defaultClientConfig.Client.AdminPassword, &clientConfig.Client.AdminPassword),
		newAltBoolFlag("skip-cert-check", "k", "Skip TLS certificate verification", defaultClientConfig.Client.SkipCertCheck, &clientConfig.Client.SkipCertCheck),
	}

	commands := make([]*cli.Command, 0, 6)
	commands = append(commands, initAppCommand(flags, clientConfig))
	commands = append(commands, initPreviewCommand(flags, clientConfig))
	commands = append(commands, initAccountCommand(flags, clientConfig))
	commands = append(commands, initParamCommand(flags, clientConfig))
	commands = append(commands, initVersionCommand(flags, clientConfig))
	return commands, nil
}
