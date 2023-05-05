// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"strings"
	"testing"
)

func AssertEqualsString(tb testing.TB, msg, want, got string) {
	tb.Helper()
	if want != got {
		tb.Errorf("%s want %s got %s", msg, want, got)
	}
}

func AssertEqualsInt(tb testing.TB, msg string, want, got int) {
	tb.Helper()
	if want != got {
		tb.Errorf("%s want %d got %d", msg, want, got)
	}
}

func AssertEqualsBool(tb testing.TB, msg string, want, got bool) {
	tb.Helper()
	if want != got {
		tb.Errorf("%s want %t got %t", msg, want, got)
	}
}

func AssertErrorContains(tb testing.TB, err error, want string) {
	tb.Helper()
	if !strings.Contains(err.Error(), want) {
		tb.Errorf("expected error containing msg `%s`, got: `%s`", want, err.Error())
	}
}
