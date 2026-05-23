package slog

import (
	"fmt"
	"testing"
)

func Test_sprint(t *testing.T) {
	tests := []struct {
		name string
		args []any
		want string
	}{
		{
			name: "NoArgs",
			args: []any{},
			want: "",
		},
		{
			name: "WithOneArgString",
			args: []any{"arg1"},
			want: "arg1",
		},
		{
			name: "WithOneArgNotString",
			args: []any{123},
			want: "123",
		},
		{
			name: "WithMultipleArgsString",
			args: []any{"arg1", "arg2"},
			want: "arg1arg2",
		},
		{
			name: "WithMultipleArgsNotString",
			args: []any{123, 456},
			want: "123 456",
		},
		{
			name: "WithErrorArgs",
			args: []any{fmt.Errorf("error message")},
			want: "error message",
		},
		{
			name: "WithStringerArgs",
			args: []any{stringer{str: "stringer"}},
			want: "stringer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sprint(tt.args...); got != tt.want {
				t.Errorf("sprint() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_sprintf(t *testing.T) {
	type args struct {
		template string
		args     []any
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "NoArgs",
			args: args{template: "template", args: []any{}},
			want: "template",
		},
		{
			name: "WithTemplateAndOneArg",
			args: args{template: "template %s", args: []any{"arg1"}},
			want: "template arg1",
		},
		{
			name: "WithTemplateAndMultipleArgs",
			args: args{template: "template %s %s", args: []any{"arg1", "arg2"}},
			want: "template arg1 arg2",
		},
		{
			name: "WithOneArgNotString",
			args: args{template: "", args: []any{123}},
			want: "123",
		},
		{
			name: "WithMultipleArgsNotString",
			args: args{template: "", args: []any{123, 456}},
			want: "123 456",
		},
		{
			name: "WithErrorArgs",
			args: args{template: "", args: []any{fmt.Errorf("error message")}},
			want: "error message",
		},
		{
			name: "WithStringerArgs",
			args: args{template: "", args: []any{stringer{str: "stringer"}}},
			want: "stringer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sprintf(tt.args.template, tt.args.args...); got != tt.want {
				t.Errorf("sprintf() = %v, want %v", got, tt.want)
			}
		})
	}
}

type stringer struct {
	str string
}

func (s stringer) String() string {
	return s.str
}
