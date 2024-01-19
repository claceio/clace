// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/claceio/clace/internal/testutil"
	"github.com/claceio/clace/internal/utils"
)

func TestStoreBasics(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
load("store.in", "store")

app = ace.app("testApp", custom_layout=True, pages = [ace.page("/", type="json")],
permissions=[
	ace.permission("store.in", "insert"),
	ace.permission("store.in", "select_by_id"),
	ace.permission("store.in", "update"),
	ace.permission("store.in", "delete_by_id"),
]
)

def handler(req):
	myt = star.test1(aint=10, astring="abc", abool=False, alist=[1], adict={'a': 1})
	ret = store.insert(table.test1, myt)
	if not ret:
		return {"error": ret.error}

	id = ret.data
	ret = store.select_by_id(table.test1, id)
	if not ret:
		return {"error": ret.error}

	f = ret.data
	f.aint = 100
	f.astring = "xyz"

	upd_status = store.update(table.test1, f)
	if not upd_status:
		return {"error": ret.error}

	# Duplicate updates should fail (optimistic locking)
	upd_status = store.update(table.test1, f)
	if upd_status:
		return {"error": "Expected duplicate update to fail"}

	ret = store.select_by_id(table.test1, id)

	del_status = store.delete_by_id(table.test1, id)
	if not del_status:
		return {"error": ret.error}
	del_status = store.delete_by_id(table.test1, id)
	if del_status:
		return {"error": "Expected delete to fail"}

	return {"intval": ret.data.aint, "stringval": ret.data.astring,
		"_id": ret.data._id,
		"creator": ret.data._created_by, "created_at": ret.data._created_at}
	`,

		"schema.star": `
type("test1", fields=[
    field("aint", INT),
    field("astring", STRING),
    field("abool", BOOLEAN),
    field("alist", LIST),
    field("adict", DICT),
])`,
		"index.go.html": ``,
	}

	a, _, err := CreateTestAppPlugin(logger, fileData, []string{"store.in"},
		[]utils.Permission{
			{Plugin: "store.in", Method: "insert"},
			{Plugin: "store.in", Method: "select_by_id"},
			{Plugin: "store.in", Method: "update"},
			{Plugin: "store.in", Method: "delete_by_id"},
		}, map[string]utils.PluginSettings{
			"store.in": {
				"db_connection": "sqlite:/tmp/clace_app.db?_journal_mode=WAL",
			},
		})
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)

	ret := make(map[string]any)
	json.NewDecoder(response.Body).Decode(&ret)

	if _, ok := ret["error"]; ok {
		t.Fatal(ret["error"])
	}

	testutil.AssertEqualsString(t, "creator", "admin", ret["creator"].(string))
	testutil.AssertEqualsString(t, "astring", "xyz", ret["stringval"].(string))
	id := ret["_id"].(float64)
	if id <= 0 {
		t.Errorf("Expected _id to be > 0, got %f", id)
	}
}
