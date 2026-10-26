package main

import (
	"reflect"
	"testing"
)

func TestReorderFlags(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "trailing value flag",
			in:   []string{"diff", "HEAD~1", "HEAD", "--path", "bgp"},
			want: []string{"--path", "bgp", "diff", "HEAD~1", "HEAD"},
		},
		{
			name: "boolean flag before positional args does not swallow positional arg",
			in:   []string{"record", "--startup", "admin", "cli"},
			want: []string{"--startup", "record", "admin", "cli"},
		},
		{
			name: "help flag after subcommand",
			in:   []string{"blame", "--help"},
			want: []string{"--help", "blame"},
		},
		{
			name: "flag with equals sign",
			in:   []string{"diff", "HEAD", "--format=json"},
			want: []string{"--format=json", "diff", "HEAD"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := reorderFlags(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("reorderFlags(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestExtractInitialFilter(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--filter", "bgp"}, "bgp"},
		{[]string{"-f", "/interface[name=ethernet-1/1]"}, "/interface[name=ethernet-1/1]"},
		{[]string{"--filter=network-instance"}, "network-instance"},
		{[]string{"-f=system"}, "system"},
		{[]string{}, ""},
	}
	for _, tc := range cases {
		got := extractInitialFilter(tc.args)
		if got != tc.want {
			t.Fatalf("extractInitialFilter(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

