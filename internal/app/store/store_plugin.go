// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package store

type StorePlugin struct {
	StoreInfo *StoreInfo
}

func init() {
	/*
		h := &httpPlugin{client: http.DefaultClient}
		pluginFuncs := []app.PluginFunc{
			app.CreatePluginApi("get", true, h.reqMethod("get")),
			app.CreatePluginApi("head", true, h.reqMethod("head")),
			app.CreatePluginApi("options", true, h.reqMethod("options")),
			app.CreatePluginApi("post", false, h.reqMethod("post")),
			app.CreatePluginApi("put", false, h.reqMethod("put")),
			app.CreatePluginApi("delete", false, h.reqMethod("delete")),
			app.CreatePluginApi("patch", false, h.reqMethod("patch")),
		}
		app.RegisterPlugin("http", pluginFuncs)
	*/
}
