// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"testing"
)

func TestGenTableName(t *testing.T) {
	s := &SqlStore{
		prefix: "prefix",
	}

	table := "table"
	expected := "'prefix_table'"

	result, err := s.genTableName(table)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if result != expected {
		t.Errorf("Expected %s, but got %s", expected, result)
	}
}

func TestGenSortString(t *testing.T) {
	sort := []string{"field1:asc", "field2:DEsc", "_id"}

	expected := "_json ->> 'field1' ASC, _json ->> 'field2' DESC, _id ASC"
	result, err := genSortString(sort)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if result != expected {
		t.Errorf("Expected %s, but got %s", expected, result)
	}
}
