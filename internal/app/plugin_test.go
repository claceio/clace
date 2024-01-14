// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0
package app

import (
	"testing"
)

func TestParseModulePath(t *testing.T) {
	tests := []struct {
		moduleFullPath string
		expectedPath   string
		expectedName   string
		expectedAcc    string
	}{
		{
			moduleFullPath: "module1.in#account1",
			expectedPath:   "module1.in",
			expectedName:   "module1",
			expectedAcc:    "account1",
		},
		{
			moduleFullPath: "module2.in",
			expectedPath:   "module2.in",
			expectedName:   "module2",
			expectedAcc:    "",
		},
		{
			moduleFullPath: "module3",
			expectedPath:   "module3",
			expectedName:   "module3",
			expectedAcc:    "",
		},
	}

	for _, test := range tests {
		path, name, acc := parseModulePath(test.moduleFullPath)

		if path != test.expectedPath {
			t.Errorf("Expected path: %s, but got: %s", test.expectedPath, path)
		}

		if name != test.expectedName {
			t.Errorf("Expected name: %s, but got: %s", test.expectedName, name)
		}

		if acc != test.expectedAcc {
			t.Errorf("Expected account: %s, but got: %s", test.expectedAcc, acc)
		}
	}
}
