// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/claceio/clace/internal/utils"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

func initVersionCommand(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "Manage app versions",
		Subcommands: []*cli.Command{
			versionListCommand(commonFlags, clientConfig),
		},
	}
}

func versionListCommand(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, newStringFlag("format", "f", "The display format. Valid options are table, csv, json, jsonl and jsonl_pretty", FORMAT_TABLE))

	return &cli.Command{
		Name:      "list",
		Usage:     "List the versions for an app",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<appPath>",
		UsageText: `args: <appPath>

    <app_path> is a required first argument. The optional domain and path are separated by a ":". This is the app for which versions are listed.

	Examples:
		clace version list example.com:/myapp`,
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 1 {
				return fmt.Errorf("requires one argument: <appPath>")
			}

			client := utils.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			values.Add("appPath", cCtx.Args().First())

			var response utils.AppVersionListResponse
			err := client.Get("/_clace/version", values, &response)
			if err != nil {
				return err
			}

			printVersionList(cCtx, response.Versions, cCtx.String("format"))
			return nil
		},
	}
}

func printVersionList(cCtx *cli.Context, versions []utils.AppVersion, format string) {
	switch format {
	case FORMAT_JSON:
		enc := json.NewEncoder(cCtx.App.Writer)
		enc.SetIndent("", "  ")
		enc.Encode(versions)
	case FORMAT_JSONL:
		enc := json.NewEncoder(cCtx.App.Writer)
		for _, version := range versions {
			enc.Encode(version)
		}
	case FORMAT_JSONL_PRETTY:
		enc := json.NewEncoder(cCtx.App.Writer)
		enc.SetIndent("", "  ")
		for _, version := range versions {
			enc.Encode(version)
			fmt.Fprintf(cCtx.App.Writer, "\n")
		}
	case FORMAT_TABLE:
		formatStrHead := "%11s %11s %-30s %-40s %-40s\n"
		formatStrData := "%11d %11d %-30s %-40s %-40s\n"
		fmt.Fprintf(cCtx.App.Writer, formatStrHead, "Version", "PrevVersion", "CreateTime", "GitCommit", "GitMessage")
		for _, version := range versions {
			fmt.Fprintf(cCtx.App.Writer, formatStrData, version.Version, version.PreviousVersion, version.CreateTime, version.Metadata.VersionMetadata.GitCommit, version.Metadata.VersionMetadata.GitMessage)
		}
	case FORMAT_CSV:
		for _, version := range versions {
			fmt.Fprintf(cCtx.App.Writer, "%d,%d,\"%s\",%s,\"%s\"\n", version.Version, version.PreviousVersion, version.CreateTime, version.Metadata.VersionMetadata.GitCommit, version.Metadata.VersionMetadata.GitMessage)
		}
	default:
		panic(fmt.Errorf("unknown format %s", format))
	}
}
