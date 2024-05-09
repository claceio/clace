// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"testing"

	"github.com/claceio/clace/internal/testutil"
)

func TestFragmentParamsBasics(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = ace.app("testApp")

def handler(req):
	return {}
		`,
		"params.star": ``,
	}
	_, _, err := CreateTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	fileData["params.star"] = `param("param1", description="param1 description", type=STRING, default="myvalue")`
	_, _, err = CreateTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	fileData["params.star"] = `param("param1", description="param1 description", type=INVALID, default="myvalue")`
	_, _, err = CreateTestApp(logger, fileData)
	testutil.AssertErrorContains(t, err, "undefined: INVALID")

	fileData["params.star"] = `param("", description="param1 description", type=STRING, default="myvalue")`
	_, _, err = CreateTestApp(logger, fileData)
	testutil.AssertErrorContains(t, err, "param name is required")

	fileData["params.star"] = `param("abc def", description="param1 description", type=STRING, default="myvalue")`
	_, _, err = CreateTestApp(logger, fileData)
	testutil.AssertErrorContains(t, err, "param name \"abc def\" has space")

	fileData["params.star"] = `param("p1", description="param1 description", type=STRING, default="myvalue")
param("p1", description="param2 description", type=STRING, default="myvalue")`
	_, _, err = CreateTestApp(logger, fileData)
	testutil.AssertErrorContains(t, err, "param \"p1\" already defined")

	fileData["params.star"] = `param("p1", description="param1 description", type=STRING, default="myvalue")
param("p2", description="param2 description", type=INT, default=10)
param("p3", description="param3 description", type=BOOLEAN, default=True)
param("p4", description="param4 description", type=LIST, default=[10])
param("p5", type=DICT, default={"a": 10})
param("p6", default="abc")`
	_, _, err = CreateTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}
}
