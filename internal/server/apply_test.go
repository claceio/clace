// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"reflect"
	"strings"
	"testing"
)

func TestReadCommands(t *testing.T) {
	tests := []struct {
		name     string
		commands string
		want     []string
	}{
		{
			name:     "empty",
			commands: "",
			want:     []string{},
		},
		{
			name:     "simple",
			commands: "echo hello\n",
			want:     []string{"echo hello"},
		},
		{
			name:     "comment",
			commands: "# comment\n",
			want:     []string{},
		},
		{
			name:     "multi-line",
			commands: "echo hello\r\nworld\n",
			want:     []string{"echo hello", "world"},
		},
		{
			name:     "multi-line with comment",
			commands: "echo hello\n# comment\nworld\n",
			want:     []string{"echo hello", "world"},
		},
		{
			name:     "multi-line with comment and continuation1",
			commands: "echo hello\\\n# comment\nworld\\abc",
			want:     []string{"echo hello world\\abc"},
		},
		{
			name: "multi-line with comment and continuation2",
			commands: `echo hello
	# comment
	world`,
			want: []string{"echo hello", "world"},
		},
		{
			name:     "multi-line with comment and continuation3",
			commands: "echo hello\\\n\n\nworld\nworld\n",
			want:     []string{"echo hello", "world", "world"},
		},
		{
			name:     "multi-line with comment and continuation4",
			commands: "echo hello\\\nworld\nworld\n",
			want:     []string{"echo hello world", "world"},
		},
		{
			name:     "multi-line with comment and continuation5",
			commands: "echo hello\\\nworld again\\\nworld\n# comment\n",
			want:     []string{"echo hello world again world"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readCommands(strings.NewReader(tt.commands))
			if err != nil {
				t.Errorf("readCommands() error = %+#v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("readCommands() = %+#v, want %+#v", got, tt.want)
			}
		})
	}
}
