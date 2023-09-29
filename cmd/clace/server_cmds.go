// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/claceio/clace/internal/utils"
	"github.com/claceio/clace/pkg/api"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

func getServerCommands(serverConfig *utils.ServerConfig) ([]*cli.Command, error) {
	_, _, defaultServerConfig, err := utils.GetDefaultConfigs()
	if err != nil {
		return nil, err
	}

	flags := []cli.Flag{
		newAltStringFlag("server_uri", "s", "The server connection uri", defaultServerConfig.ServerUri, &serverConfig.ServerUri),
		newAltStringFlag("admin_user", "u", "The admin user name", defaultServerConfig.AdminUser, &serverConfig.AdminUser),
		newAltStringFlag("http.host", "i", "The interface to bind on for HTTP", defaultServerConfig.Http.Host, &serverConfig.Http.Host),
		newAltIntFlag("http.port", "p", "The port to listen on for HTTP", defaultServerConfig.Http.Port, &serverConfig.Http.Port),
		newAltStringFlag("https.host", "", "The interface to bind on for HTTPS", defaultServerConfig.Https.Host, &serverConfig.Https.Host),
		newAltIntFlag("https.port", "", "The port to listen on for HTTPS", defaultServerConfig.Https.Port, &serverConfig.Https.Port),
		newAltStringFlag("logging.level", "l", "The logging level to use", defaultServerConfig.Log.Level, &serverConfig.Log.Level),
		newAltBoolFlag("logging.console", "c", "Enable console logging", defaultServerConfig.Log.Console, &serverConfig.Log.Console),
	}

	return []*cli.Command{
		{
			Name:  "server",
			Usage: "Manage the Clace server",
			Subcommands: []*cli.Command{
				{
					Name:   "start",
					Usage:  "Start the clace server",
					Flags:  flags,
					Before: altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
					Action: func(cCtx *cli.Context) error {
						return startServer(cCtx, serverConfig)
					},
				},
			},
		},
	}, nil
}

func startServer(cCtx *cli.Context, serverConfig *utils.ServerConfig) error {
	apiConfig := api.ServerConfig{ServerConfig: serverConfig}
	server, err := api.NewServer(&apiConfig)
	if err != nil {
		fmt.Printf("Error initializing server: %s\n", err)
		os.Exit(1)
	}
	err = server.Start()
	if err != nil {
		fmt.Printf("Error starting server: %s\n", err)
		os.Exit(1)
	}

	if serverConfig.Http.Port >= 0 {
		addr := fmt.Sprintf("http://%s:%d", serverConfig.Http.Host, serverConfig.Http.Port)
		fmt.Fprintf(os.Stderr, "Server listening on %s\n", addr)
	}
	if serverConfig.Https.Port >= 0 {
		addr := fmt.Sprintf("https://%s:%d", serverConfig.Https.Host, serverConfig.Https.Port)
		fmt.Fprintf(os.Stderr, "Server listening on %s\n", addr)
	}

	c := make(chan os.Signal, 1)
	// We'll accept graceful shutdowns when quit via SIGINT (Ctrl+C)
	signal.Notify(c, os.Interrupt)

	// Block until we receive our signal.
	<-c

	// Create a deadline to wait for.
	ctxTimeout, cancel := context.WithTimeout(context.Background(), 30)
	defer cancel()
	server.Stop(ctxTimeout)

	return nil
}
