// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/claceio/clace/pkg/api"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

func serverCommands(serverConfig *api.ServerConfig) []*cli.Command {
	flags := []cli.Flag{
		newStringFlag("http.host", "i", "The interface to bind on for HTTP", serverConfig.Http.Host, &serverConfig.Http.Host),
		newIntFlag("http.port", "p", "The port to listen on for HTTP", serverConfig.Http.Port, &serverConfig.Http.Port),
		newStringFlag("logging.level", "l", "The logging level to use", serverConfig.Log.Level, &serverConfig.Log.Level),
		newBoolFlag("logging.console", "", "Enable console logging", serverConfig.Log.Console, &serverConfig.Log.Console),
	}

	return []*cli.Command{
		{
			Name: "server",
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
	}
}

func startServer(cCtx *cli.Context, serverConfig *api.ServerConfig) error {
	server, err := api.NewServer(serverConfig)
	if err != nil {
		fmt.Printf("Error initializing server: %s\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Starting server on http://%s:%d\n", serverConfig.Http.Host, serverConfig.Http.Port)
	err = server.Start()
	if err != nil {
		fmt.Printf("Error starting server: %s\n", err)
		os.Exit(1)
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
