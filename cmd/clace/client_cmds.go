// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/claceio/clace/internal/utils"
	"github.com/urfave/cli/v2"
)

func getClientCommands(clientConfig *utils.ClientConfig) ([]*cli.Command, error) {
	defaultClientConfig, err := utils.NewClientConfigEmbedded()
	if err != nil {
		return nil, err
	}

	flags := []cli.Flag{
		newAltStringFlag("server_uri", "s", "The server connection uri", defaultClientConfig.ServerUri, &clientConfig.ServerUri),
		newAltStringFlag("admin_user", "u", "The admin user name", defaultClientConfig.AdminUser, &clientConfig.AdminUser),
		newAltStringFlag("admin_password", "w", "The admin user password", defaultClientConfig.AdminPassword, &clientConfig.AdminPassword),
		newAltBoolFlag("skip_cert_check", "k", "Skip TLS certificate verification", defaultClientConfig.SkipCertCheck, &clientConfig.SkipCertCheck),
	}

	commands := make([]*cli.Command, 0, 6)
	commands = append(commands, initAppCommand(flags, clientConfig))
	return commands, nil
}
