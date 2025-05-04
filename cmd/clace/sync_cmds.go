// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/claceio/clace/internal/system"
	"github.com/claceio/clace/internal/types"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

func initSyncCommand(commonFlags []cli.Flag, clientConfig *types.ClientConfig) *cli.Command {
	return &cli.Command{
		Name:  "sync",
		Usage: "Manage sync operations, scheduled and webhook",
		Subcommands: []*cli.Command{
			syncCreateCommand(commonFlags, clientConfig),
			syncListCommand(commonFlags, clientConfig),
			syncDeleteCommand(commonFlags, clientConfig),
		},
	}
}

func syncCreateCommand(commonFlags []cli.Flag, clientConfig *types.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, newStringFlag("branch", "b", "The branch to checkout if using git source", "main"))
	flags = append(flags, newStringFlag("git-auth", "g", "The name of the git_auth entry in server config to use", ""))
	flags = append(flags, newBoolFlag("approve", "a", "Approve the app permissions", false))
	flags = append(flags, newStringFlag("reload", "r", "Which apps to reload: none, updated, matched", ""))
	flags = append(flags, newBoolFlag("promote", "p", "Promote changes from stage to prod", false))
	flags = append(flags, newIntFlag("schedule", "s", "Schedule sync for every N minutes", 0))
	flags = append(flags, newBoolFlag("clobber", "", "Force update app config, overwriting non-declarative changes", false))
	flags = append(flags, newBoolFlag("force-reload", "f", "Force reload even if there is no new commit", false))
	flags = append(flags, dryRunFlag())

	return &cli.Command{
		Name:      "create",
		Usage:     "Create sync job for updating app config",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<filePath>",
		UsageText: `args: <filePath>

<filePath> is the path to the apply file containing the app configuration.

Examples:
  Create sync entry, reloading all apps: clace sync ./app.ace
  Create sync entry, reloading updated apps: clace sync --reload=updated github.com/claceio/apps/apps.ace
  Create sync entry: clace sync --promote --approve github.com/claceio/apps/apps.ace
  Create sync entry, overwriting changes: clace sync --promote --clobber github.com/claceio/apps/apps.ace
`,

		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 1 {
				return fmt.Errorf("expected one arg : <filePath>")
			}

			client := system.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.Client.AdminPassword, clientConfig.Client.SkipCertCheck)
			reloadMode := types.AppReloadOption(cmp.Or(cCtx.String("reload"), string(types.AppReloadOptionMatched)))
			schedule := cCtx.Int("schedule")
			values := url.Values{}
			values.Add("path", cCtx.Args().Get(0))
			values.Add(DRY_RUN_ARG, strconv.FormatBool(cCtx.Bool(DRY_RUN_FLAG)))

			sync := types.SyncMetadata{
				GitBranch:         cCtx.String("branch"),
				GitAuth:           cCtx.String("git-auth"),
				Promote:           cCtx.Bool("promote"),
				Approve:           cCtx.Bool("approve"),
				Reload:            string(reloadMode),
				Clobber:           cCtx.Bool("clobber"),
				ForceReload:       cCtx.Bool("force-reload"),
				ScheduleFrequency: schedule,
			}

			var syncResponse types.SyncCreateResponse
			err := client.Post("/_clace/sync", values, sync, &syncResponse)
			if err != nil {
				return err
			}

			fmt.Printf("Sync job created with Id: %s\n", syncResponse.Id)

			if syncResponse.DryRun {
				fmt.Print(DRY_RUN_MESSAGE)
			}

			return nil
		},
	}
}

func syncListCommand(commonFlags []cli.Flag, clientConfig *types.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, newStringFlag("format", "f", "The display format. Valid options are table, basic, csv, json, jsonl and jsonl_pretty", ""))
	flags = append(flags, dryRunFlag())

	return &cli.Command{
		Name:      "list",
		Usage:     "List the sync jobs",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "",
		UsageText: `
	Examples:
	  List sync jobs: clace sync list`,
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() > 0 {
				return fmt.Errorf("no args expected")
			}

			client := system.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.Client.AdminPassword, clientConfig.Client.SkipCertCheck)
			values := url.Values{}
			values.Add("appPath", cCtx.Args().First())

			var response types.SyncListResponse
			err := client.Get("/_clace/sync", values, &response)
			if err != nil {
				return err
			}

			printSyncList(cCtx, response.Entries, cmp.Or(cCtx.String("format"), clientConfig.Client.DefaultFormat))
			return nil
		},
	}
}

func syncDeleteCommand(commonFlags []cli.Flag, clientConfig *types.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, dryRunFlag())

	return &cli.Command{
		Name:      "delete",
		Usage:     "Delete specified sync job",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "args: <syncId>",
		UsageText: `
	Examples:
	  Delete sync jobs: clace sync delete cl_sync_44asd232`,
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 1 {
				return fmt.Errorf("expected one args: <syncId>")
			}

			client := system.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.Client.AdminPassword, clientConfig.Client.SkipCertCheck)
			values := url.Values{}
			values.Add("id", cCtx.Args().First())

			var response types.SyncDeleteResponse
			err := client.Delete("/_clace/sync", values, &response)
			if err != nil {
				return err
			}

			if response.Id != "" {
				fmt.Fprintf(cCtx.App.Writer, "Sync job with Id %s deleted\n", cCtx.Args().First())
			}

			if response.DryRun {
				fmt.Print(DRY_RUN_MESSAGE)
			}

			return nil
		},
	}
}

func printSyncList(cCtx *cli.Context, sync []*types.SyncEntry, format string) {
	switch format {
	case FORMAT_JSON:
		enc := json.NewEncoder(cCtx.App.Writer)
		enc.SetIndent("", "  ")
		enc.Encode(sync)
	case FORMAT_JSONL:
		enc := json.NewEncoder(cCtx.App.Writer)
		for _, s := range sync {
			enc.Encode(s)
		}
	case FORMAT_JSONL_PRETTY:
		enc := json.NewEncoder(cCtx.App.Writer)
		enc.SetIndent("", "  ")
		for _, s := range sync {
			enc.Encode(s)
		}
	case FORMAT_BASIC:
		formatStr := "%-35s %-10s %-40s %-s\n"
		fmt.Fprintf(cCtx.App.Writer, formatStr, "Id", "Trigger", "Path", "Url")

		for _, s := range sync {
			fmt.Fprintf(cCtx.App.Writer, formatStr, s.Id, getTriggerType(s), s.Path, s.Metadata.WebhookUrl)
		}
	case FORMAT_TABLE:
		formatStrHead := "%-35s %-10s %-8s %-8s %-7s %-6s %-10s %-15s %-40s %-s\n"
		formatStrData := "%-35s %-10s %-8s %-8t %-7t %-6t %-10s %-15s %-40s %-s\n"
		fmt.Fprintf(cCtx.App.Writer, formatStrHead, "Id", "Trigger", "Reload", "Promote", "Approve", "Clobber", "GitAuth", "Branch", "Path", "Url")

		for _, s := range sync {
			fmt.Fprintf(cCtx.App.Writer, formatStrData, s.Id, getTriggerType(s), s.Metadata.Reload, s.Metadata.Promote, s.Metadata.Approve, s.Metadata.Clobber, s.Metadata.GitAuth, s.Metadata.GitBranch, s.Path, s.Metadata.WebhookUrl)
		}
	case FORMAT_CSV:
		for _, s := range sync {
			fmt.Fprintf(cCtx.App.Writer, "%s,%s,%s,%t,%t,%t,%s,%s,%s,%s\n", s.Id, getTriggerType(s), s.Metadata.Reload, s.Metadata.Promote, s.Metadata.Approve, s.Metadata.Clobber, s.Metadata.GitAuth, s.Metadata.GitBranch, s.Path, s.Metadata.WebhookUrl)
		}
	default:
		panic(fmt.Errorf("unknown format %s", format))
	}
}

func getTriggerType(sync *types.SyncEntry) string {
	if sync.Metadata.ScheduleFrequency > 0 {
		return fmt.Sprintf("%d (mins)", sync.Metadata.ScheduleFrequency)
	}
	return "Webhook"
}
