// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

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
			versionFilesCommand(commonFlags, clientConfig),
			versionSwitchCommand(commonFlags, clientConfig),
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
		formatStrHead := "%6s %8s %8s %-30s %-20s %-40s\n"
		formatStrData := "%6s %8d %8d %-30s %.20s %-40s\n"
		fmt.Fprintf(cCtx.App.Writer, formatStrHead, "Active", "Version", "Previous", "CreateTime", "GitCommit", "GitMessage")
		for _, version := range versions {
			isLive := ""
			if version.Active {
				isLive = "=====>"
			}
			fmt.Fprintf(cCtx.App.Writer, formatStrData, isLive, version.Version, version.PreviousVersion, version.CreateTime, version.Metadata.VersionMetadata.GitCommit, version.Metadata.VersionMetadata.GitMessage)
		}
	case FORMAT_CSV:
		for _, version := range versions {
			fmt.Fprintf(cCtx.App.Writer, "%t,%d,%d,\"%s\",%s,\"%s\"\n", version.Active, version.Version, version.PreviousVersion, version.CreateTime, version.Metadata.VersionMetadata.GitCommit, version.Metadata.VersionMetadata.GitMessage)
		}
	default:
		panic(fmt.Errorf("unknown format %s", format))
	}
}

func versionFilesCommand(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, newStringFlag("format", "f", "The display format. Valid options are table, csv, json, jsonl and jsonl_pretty", FORMAT_TABLE))

	return &cli.Command{
		Name:      "files",
		Usage:     "List the files in a versions of the app",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<appPath> [<version>]",
		UsageText: `args: <appPath> [<version>]

    <app_path> is a required first argument. The optional domain and path are separated by a ":". This is the app for which versions are listed.
	<version> is an optional second argument. This is the version of the app for which files are listed. Lists current version by default.

	Examples:
		clace version files example.com:/myapp`,
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() == 0 {
				return fmt.Errorf("requires argument: <appPath> [<version>]")
			}

			client := utils.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			values.Add("appPath", cCtx.Args().First())
			if cCtx.NArg() > 1 {
				values.Add("version", cCtx.Args().Get(1))
			}

			var response utils.AppVersionFilesResponse
			err := client.Get("/_clace/version/files", values, &response)
			if err != nil {
				return err
			}

			printFileList(cCtx, response.Files, cCtx.String("format"))
			return nil
		},
	}
}

func printFileList(cCtx *cli.Context, files []utils.AppFile, format string) {
	switch format {
	case FORMAT_JSON:
		enc := json.NewEncoder(cCtx.App.Writer)
		enc.SetIndent("", "  ")
		enc.Encode(files)
	case FORMAT_JSONL:
		enc := json.NewEncoder(cCtx.App.Writer)
		for _, version := range files {
			enc.Encode(version)
		}
	case FORMAT_JSONL_PRETTY:
		enc := json.NewEncoder(cCtx.App.Writer)
		enc.SetIndent("", "  ")
		for _, f := range files {
			enc.Encode(f)
			fmt.Fprintf(cCtx.App.Writer, "\n")
		}
	case FORMAT_TABLE:
		formatStrHead := "%7s %-64s %-50s\n"
		formatStrData := "%7d %-64s %-50s\n"
		fmt.Fprintf(cCtx.App.Writer, formatStrHead, "Size", "Etag", "Path")
		for _, f := range files {
			fmt.Fprintf(cCtx.App.Writer, formatStrData, f.Size, f.Etag, f.Name)
		}
	case FORMAT_CSV:
		for _, version := range files {
			fmt.Fprintf(cCtx.App.Writer, "%d,%s,\"%s\"\n", version.Size, version.Etag, version.Name)
		}
	default:
		panic(fmt.Errorf("unknown format %s", format))
	}
}

func versionSwitchCommand(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, newStringFlag("format", "f", "The display format. Valid options are table, csv, json, jsonl and jsonl_pretty", FORMAT_TABLE))
	flags = append(flags, dryRunFlag())

	return &cli.Command{
		Name:      "switch",
		Usage:     "Switch the version for an app",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<appPath> <version>",
		UsageText: `args: <appPath> <version>

<app_path> is a required first argument. The optional domain and path are separated by a ":". This is the app for which versions are listed.
<version> is a required second argument. This is the version number to switch to. Use "previous" or "next" to switch to the previous or next version.

	Examples:
		clace version switch example.com:/myapp next
		clace version switch /myapp_cl_stage 123
		clace version switch /test previous`,

		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 2 {
				return fmt.Errorf("requires argument: <appPath> <version>")
			}

			client := utils.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			values.Add("appPath", cCtx.Args().First())
			values.Add("version", cCtx.Args().Get(1))
			values.Add(DRY_RUN_ARG, strconv.FormatBool(cCtx.Bool(DRY_RUN_FLAG)))

			var response utils.AppVersionSwitchResponse
			err := client.Post("/_clace/version", values, nil, &response)
			if err != nil {
				return err
			}

			fmt.Fprintf(cCtx.App.Writer, "Switched %s from version %d to version %d\n", cCtx.Args().First(), response.FromVersion, response.ToVersion)

			if response.DryRun {
				fmt.Print(DRY_RUN_MESSAGE)
			}

			return nil
		},
	}
}
