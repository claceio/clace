// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/plugin"
	"github.com/claceio/clace/internal/system"
	"github.com/claceio/clace/internal/types"
	"go.starlark.net/starlark"
)

func initClacePlugin(server *Server) {
	c := &clacePlugin{}
	pluginFuncs := []plugin.PluginFunc{
		app.CreatePluginApiName(c.ListApps, app.READ, "list_apps"),
		app.CreatePluginApiName(c.ListAllApps, app.READ, "list_all_apps"),
		app.CreatePluginApiName(c.ListAuditEvents, app.READ, "list_audit_events"),
	}

	newClacePlugin := func(pluginContext *types.PluginContext) (any, error) {
		return &clacePlugin{server: server}, nil
	}

	app.RegisterPlugin("clace", newClacePlugin, pluginFuncs)
}

type clacePlugin struct {
	server *Server
}

func (c *clacePlugin) verifyHasAccess(userId string, appAuth types.AppAuthnType) bool {
	if appAuth == types.AppAuthnDefault {
		appAuth = types.AppAuthnType(c.server.config.Security.AppDefaultAuthType)
	}

	// Verify user_id as set in authenticateAndServeApp
	if appAuth == "" || appAuth == types.AppAuthnNone {
		// No auth required for this app, allow access
		return true
	} else if appAuth == types.AppAuthnSystem {
		return userId == types.ADMIN_USER
	} else if appAuth == "cert" || strings.HasPrefix(string(appAuth), "cert_") {
		return userId == string(appAuth)
	} else {
		provider, _, ok := strings.Cut(string(userId), ":")
		if !ok {
			c.server.Warn().Str("user_id", userId).Msg("Unknown user_id format")
			return false
		}
		// Check Oauth provider is the same as the app's provider
		return provider == string(appAuth)
	}
}

func getAppUrl(app types.AppInfo, server *Server) string {
	useHttps := server.config.Https.Port > 0
	domain := cmp.Or(app.AppPathDomain.Domain, server.config.System.DefaultDomain)
	if useHttps {
		return fmt.Sprintf("https://%s:%d%s", domain, server.config.Https.Port, app.Path)
	} else {
		return fmt.Sprintf("http://%s:%d%s", domain, server.config.Http.Port, app.Path)
	}
}

func (c *clacePlugin) ListAllApps(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return c.listAppsImpl(thread, builtin, args, kwargs, false, "list_all_apps")
}

func (c *clacePlugin) ListApps(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return c.listAppsImpl(thread, builtin, args, kwargs, true, "list_apps")
}

func (c *clacePlugin) listAppsImpl(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple, permCheck bool, apiName string) (starlark.Value, error) {
	var query starlark.String
	var include_internal starlark.Bool
	if err := starlark.UnpackArgs(apiName, args, kwargs, "query?", &query, "include_internal?", &include_internal); err != nil {
		return nil, err
	}

	apps, err := c.server.apps.GetAllAppsInfo()
	if err != nil {
		return nil, err
	}

	userId := system.GetRequestUserId(thread)
	ret := starlark.List{}
	for _, app := range apps {
		if permCheck && !c.verifyHasAccess(userId, app.Auth) {
			continue
		}

		// Filter out internal apps
		if app.MainApp != "" && !bool(include_internal) {
			continue
		}

		// Check query filter
		if query != "" {
			queryStr := strings.ToLower(query.GoString())
			if !strings.Contains(strings.ToLower(app.Name), queryStr) &&
				!strings.Contains(strings.ToLower(app.AppPathDomain.String()), queryStr) &&
				!strings.Contains(strings.ToLower(app.SourceUrl), queryStr) {
				continue
			}
		}

		v := starlark.Dict{}
		v.SetKey(starlark.String("name"), starlark.String(app.Name))
		v.SetKey(starlark.String("url"), starlark.String(getAppUrl(app, c.server)))
		v.SetKey(starlark.String("path"), starlark.String(app.AppPathDomain.String()))
		v.SetKey(starlark.String("id"), starlark.String(app.Id))
		v.SetKey(starlark.String("is_dev"), starlark.Bool(app.IsDev))
		v.SetKey(starlark.String("main_app"), starlark.String(app.MainApp))
		if app.Auth == types.AppAuthnDefault {
			v.SetKey(starlark.String("auth"), starlark.String(c.server.config.Security.AppDefaultAuthType))
			v.SetKey(starlark.String("auth_uses_default"), starlark.Bool(true))
		} else {
			v.SetKey(starlark.String("auth"), starlark.String(app.Auth))
			v.SetKey(starlark.String("auth_uses_default"), starlark.Bool(false))
		}
		v.SetKey(starlark.String("source_url"), starlark.String(app.SourceUrl))
		v.SetKey(starlark.String("spec"), starlark.String(app.Spec))
		v.SetKey(starlark.String("version"), starlark.MakeInt(app.Version))
		v.SetKey(starlark.String("git_sha"), starlark.String(app.GitSha))
		v.SetKey(starlark.String("git_message"), starlark.String(app.GitMessage))

		ret.Append(&v)
	}

	return &ret, nil
}

func (c *clacePlugin) ListAuditEvents(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var appGlob, userId, eventType, operation, target, status, rid, detail starlark.String
	var startDate, endDate, beforeTimestamp starlark.String
	limit := starlark.MakeInt(50)
	if err := starlark.UnpackArgs("list_audit_events", args, kwargs, "app_glob?", &appGlob, "user_id?", &userId, "event_type?",
		&eventType, "operation?", &operation, "target?", &target, "status?", &status, "start_date", &startDate, "end_date?", &endDate,
		"rid?", &rid, "detail?", &detail, "limit?", &limit, "before_timestamp?", &beforeTimestamp); err != nil {
		return nil, err
	}

	var query strings.Builder
	query.WriteString("select rid, app_id, create_time, user_id, event_type, operation, target, status, detail from audit ")

	filterConditions := []string{}
	appGlobStr := strings.TrimSpace(appGlob.GoString())
	if appGlobStr != "" {
		appInfo, err := c.server.ParseGlob(appGlobStr)
		if err != nil {
			return nil, err
		}
		appIds := []string{}
		for _, app := range appInfo {
			appIds = append(appIds, "\""+string(app.Id)+"\"")
		}

		filterConditions = append(filterConditions, fmt.Sprintf("app_id in (%s)", strings.Join(appIds, ",")))
	}

	queryParams := []any{}
	userIdStr := strings.TrimSpace(userId.GoString())
	if userIdStr != "" {
		filterConditions = append(filterConditions, "user_id = ?")
		queryParams = append(queryParams, userIdStr)
	}

	eventTypeStr := strings.TrimSpace(eventType.GoString())
	if eventTypeStr != "" {
		filterConditions = append(filterConditions, "event_type = ?")
		queryParams = append(queryParams, eventTypeStr)
	}

	operationStr := strings.TrimSpace(operation.GoString())
	if operationStr != "" {
		filterConditions = append(filterConditions, "operation = ?")
		queryParams = append(queryParams, operationStr)
	}

	targetStr := strings.TrimSpace(target.GoString())
	if targetStr != "" {
		filterConditions = append(filterConditions, "target = ?")
		queryParams = append(queryParams, targetStr)
	}

	statusStr := strings.TrimSpace(status.GoString())
	if statusStr != "" {
		filterConditions = append(filterConditions, "status = ?")
		queryParams = append(queryParams, statusStr)
	}

	startDateStr := strings.TrimSpace(startDate.GoString())
	if startDateStr != "" {
		filterConditions = append(filterConditions, `create_time >= strftime('%s', ?) * 1000000000`)
		queryParams = append(queryParams, startDateStr)
	}

	endDateStr := strings.TrimSpace(endDate.GoString())
	if endDateStr != "" {
		filterConditions = append(filterConditions, `create_time <= (strftime('%s', ?) + 86400) * 1000000000`)
		queryParams = append(queryParams, endDateStr)
	}

	ridStr := strings.TrimSpace(rid.GoString())
	if ridStr != "" {
		filterConditions = append(filterConditions, "rid = ?")
		queryParams = append(queryParams, ridStr)
	}

	detailStr := strings.TrimSpace(detail.GoString())
	if detailStr != "" {
		filterConditions = append(filterConditions, "detail like ?")
		queryParams = append(queryParams, detailStr)
	}

	beforeTimestampStr := strings.TrimSpace(beforeTimestamp.GoString())
	if beforeTimestampStr != "" {
		filterConditions = append(filterConditions, " create_time < ?")
		bt, err := strconv.ParseInt(beforeTimestampStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("before_timestamp has to be a valid in value in milliseconds")
		}
		queryParams = append(queryParams, bt)
	}

	if len(filterConditions) > 0 {
		query.WriteString(" where ")
		query.WriteString(strings.Join(filterConditions, " and "))
	}

	query.WriteString(" order by create_time desc")

	limitVal, _ := limit.Int64()
	if limitVal <= 0 || limitVal > 10_000 {
		return nil, fmt.Errorf("limit has to be between 1 and 10000")
	}
	query.WriteString(" limit ?")
	queryParams = append(queryParams, limitVal)

	rows, err := c.server.auditDB.Query(query.String(), queryParams...)
	if err != nil {
		return nil, err
	}

	apps, err := c.server.apps.GetAllAppsInfo()
	if err != nil {
		return nil, err
	}
	appIdMap := map[types.AppId]types.AppInfo{}
	for _, app := range apps {
		appIdMap[app.Id] = app
	}

	ret := starlark.List{}
	for rows.Next() {
		var rid, appId, userId, eventType, operation, target, status, detail string
		var createTime int64
		err := rows.Scan(&rid, &appId, &createTime, &userId, &eventType, &operation, &target, &status, &detail)
		if err != nil {
			return nil, err
		}

		utcTime := time.Unix(0, createTime).UTC()

		v := starlark.Dict{}
		v.SetKey(starlark.String("rid"), starlark.String(rid))
		v.SetKey(starlark.String("app_id"), starlark.String(appId))
		if appInfo, ok := appIdMap[types.AppId(appId)]; ok {
			v.SetKey(starlark.String("app_name"), starlark.String(appInfo.Name))
			v.SetKey(starlark.String("app_path"), starlark.String(appInfo.AppPathDomain.String()))
		} else {
			v.SetKey(starlark.String("app_name"), starlark.String(""))
			v.SetKey(starlark.String("app_path"), starlark.String(""))
		}
		v.SetKey(starlark.String("create_time_epoch"), starlark.String(strconv.FormatInt(createTime, 10)))
		v.SetKey(starlark.String("create_time"), starlark.String(utcTime.Format("2006-01-02T15:04:05.999Z")))
		v.SetKey(starlark.String("user_id"), starlark.String(userId))
		v.SetKey(starlark.String("event_type"), starlark.String(eventType))
		v.SetKey(starlark.String("operation"), starlark.String(operation))
		v.SetKey(starlark.String("target"), starlark.String(target))
		v.SetKey(starlark.String("status"), starlark.String(status))
		v.SetKey(starlark.String("detail"), starlark.String(detail))

		ret.Append(&v)
	}

	if closeErr := rows.Close(); closeErr != nil {
		return nil, fmt.Errorf("error closing rows: %w", closeErr)
	}

	return &ret, nil
}
