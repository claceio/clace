// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"encoding/json"
	"fmt"
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
	ace.permission("store.in", "select"),
	ace.permission("store.in", "delete"),
	ace.permission("store.in", "count"),
]
)

def handler(req):

	rows = store.delete(table.test1, {})
	myt = star.test1(aint=10, astring="abc", abool=False, alist=[1], adict={'a': 1})
	ret = store.insert(table.test1, myt)
	if not ret:
		return {"error": ret.error}
	myt.aint=20
	ret2 = store.insert(table.test1, myt)
	if not ret2:
		return {"error": ret2.error}
	myt.aint=30
	myt.adict = {"a": 2}
	ret3 = store.insert(table.test1, myt)
	if not ret3:
		return {"error": ret3.error}
	ret4 = store.insert(table.test1, myt)
	if ret4: # Expect to fail
		return {"error": "Expected duplicate insert to fail"}

	id = ret.value
	ret = store.select_by_id(table.test1, id)
	if not ret:
		return {"error": ret.error}

	f = ret.value
	f.aint = 100
	f.astring = "xyz"

	upd_status = store.update(table.test1, f)
	if not upd_status:
		return {"error": ret.error}

	# Duplicate updates should fail (optimistic locking)
	upd_status = store.update(table.test1, f)
	if upd_status:
		return {"error": "Expected duplicate update to fail"}

	q1 = store.count(table.test1, {"aint": 100})
	if not q1:
		return {"error": q1.error}
	if q1.value != 1:
		return {"error": "Expected count to be 1, got %d" % q1.value}

	q2 = store.count(table.test1, {"adict.a": 2})
	if not q2:
		return {"error": q2.error}
	if q2.value != 1:
		return {"error": "Expected count to be 1, got %d" % q2.value}


	ret = store.select_by_id(table.test1, id)

	select_result = store.select(table.test1, {})

	all_rows = []
	for row in select_result.value:
		all_rows.append(row)

	select_result = store.select(table.test1, {}, sort=["aint:asc"])
	if not select_result:
		return {"error": select_result.error}
	index = 0
	for row in select_result.value:
		if row.aint != 20:
			return {"error": "Expected first aint to be 20, got %d" % row.aint}
		break

	del_status = store.delete_by_id(table.test1, id)
	if not del_status:
		return {"error": ret.error}
	del_status = store.delete_by_id(table.test1, id)
	if del_status:
		return {"error": "Expected delete to fail"}

	return {"intval": ret.value.aint, "stringval": ret.value.astring,
		"_id": ret.value._id,
		"creator": ret.value._created_by, "created_at": ret.value._created_at,
	    "all_rows": all_rows}
	`,

		"schema.star": `
type("test1", fields=[
    field("aint", INT),
    field("astring", STRING),
    field("abool", BOOLEAN),
    field("alist", LIST),
    field("adict", DICT),
],
indexes=[
	index(["aint:asc", "astring:desc"], unique=True)
	])`,
		"index.go.html": ``,
	}

	a, _, err := CreateTestAppPlugin(logger, fileData, []string{"store.in"},
		[]utils.Permission{
			{Plugin: "store.in", Method: "insert"},
			{Plugin: "store.in", Method: "select_by_id"},
			{Plugin: "store.in", Method: "update"},
			{Plugin: "store.in", Method: "delete_by_id"},
			{Plugin: "store.in", Method: "select"},
			{Plugin: "store.in", Method: "delete"},
			{Plugin: "store.in", Method: "count"},
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
	str := response.Body.String()
	fmt.Print(str)
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
	testutil.AssertEqualsInt(t, "length", 3, len(ret["all_rows"].([]any)))
	rows := ret["all_rows"].([]any)
	if rows[0].(map[string]any)["aint"].(float64) != 100 {
		t.Errorf("Expected aint to be 100, got %f", rows[0].(map[string]any)["aint"].(float64))
	}
	if rows[1].(map[string]any)["aint"].(float64) != 20 {
		t.Errorf("Expected aint to be 20, got %f", rows[0].(map[string]any)["aint"].(float64))
	}
}
