// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/claceio/clace/internal/testutil"
	"github.com/claceio/clace/internal/utils"
)

func createAppEntry() *utils.AppEntry {
	return &utils.AppEntry{
		Id:      "testApp",
		Path:    "/test",
		Domain:  "",
		CodeUrl: ".",
	}
}

type TestFileRead struct {
	fileData map[string]string
}

var _ AppFileReader = (*TestFileRead)(nil)

func (f TestFileRead) Read(name string) (io.Reader, error) {
	data, ok := f.fileData[name]
	if !ok {
		return nil, fmt.Errorf("test data not found: %s", name)
	}
	return strings.NewReader(data), nil
}

func TestAppLoadError(t *testing.T) {
	logger := testutil.TestLogger()
	a := NewApp(logger, createAppEntry())

	fileRead := TestFileRead{fileData: map[string]string{
		"clace.star": ``,
	}}
	err := a.Initialize(fileRead)
	testutil.AssertErrorContains(t, err, "app not defined, check clace.star")

	fileRead = TestFileRead{fileData: map[string]string{
		"clace.star": `app = 1`,
	}}
	err = a.Initialize(fileRead)
	testutil.AssertErrorContains(t, err, "app not of type APP in clace.star")

	fileRead = TestFileRead{fileData: map[string]string{
		"clace.star": `app = APP()`,
	}}
	err = a.Initialize(fileRead)
	logger.Info().Msg(err.Error())
	testutil.AssertErrorContains(t, err, "missing argument for name")
}
