// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"strings"
	"testing"
)

func AssertEqualsString(tb testing.TB, msg, want, got string) {
	tb.Helper()
	if want != got {
		tb.Errorf("%s want <%s> length %d, got <%s> length %d",
			msg, want, len(want), got, len(got))
	}
}

// AssertStringMatch matches strings after removing extra spaces
func AssertStringMatch(tb testing.TB, msg, want, got string) {
	want = strings.Join(strings.Fields(want), " ")
	got = strings.Join(strings.Fields(got), " ")

	tb.Helper()
	if want != got {
		tb.Errorf("%s want <%s> length %d, got <%s> length %d",
			msg, want, len(want), got, len(got))
	}
}

func AssertEqualsInt(tb testing.TB, msg string, want, got int) {
	tb.Helper()
	if want != got {
		tb.Errorf("%s want <%d> got <%d>", msg, want, got)
	}
}

func AssertEqualsBool(tb testing.TB, msg string, want, got bool) {
	tb.Helper()
	if want != got {
		tb.Errorf("%s want <%t> got <%t>", msg, want, got)
	}
}

func AssertNoError(tb testing.TB, err error) {
	tb.Helper()
	if err != nil {
		tb.Errorf("expected no error, got : `%s`", err)
	}
}

func AssertErrorContains(tb testing.TB, err error, want string) {
	tb.Helper()
	if err == nil {
		tb.Errorf("expected error containing msg `%s`, got nil", want)
	} else if !strings.Contains(err.Error(), want) {
		tb.Errorf("expected error containing msg `%s`, got: `%s`", want, err.Error())
	}
}

func AssertStringContains(tb testing.TB, str string, want string) {
	tb.Helper()
	if !strings.Contains(str, want) {
		tb.Errorf("expected string containing msg `%s`, got: `%s`", want, str)
	}
}
