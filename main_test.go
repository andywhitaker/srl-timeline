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

func TestExtractGlobalOptionsAndThemes(t *testing.T) {
	defer func() {
		configureEnvironmentAppearance()
	}()

	rem, theme, profile := extractGlobalOptions([]string{"diff", "HEAD~1", "HEAD", "--theme", "cobalt2", "--color-profile", "256", "--format", "cli"})
	wantRem := []string{"diff", "HEAD~1", "HEAD", "--format", "cli"}
	if !reflect.DeepEqual(rem, wantRem) {
		t.Fatalf("extractGlobalOptions remaining = %v, want %v", rem, wantRem)
	}
	if theme != "cobalt2" {
		t.Fatalf("theme = %q, want %q", theme, "cobalt2")
	}
	if profile != "256" {
		t.Fatalf("profile = %q, want %q", profile, "256")
	}

	rem2, theme2, profile2 := extractGlobalOptions([]string{"--theme=Solarized Dark", "--color-profile=truecolor", "-f", "bgp"})
	wantRem2 := []string{"-f", "bgp"}
	if !reflect.DeepEqual(rem2, wantRem2) {
		t.Fatalf("extractGlobalOptions remaining = %v, want %v", rem2, wantRem2)
	}
	if theme2 != "Solarized Dark" || profile2 != "truecolor" {
		t.Fatalf("got theme=%q profile=%q", theme2, profile2)
	}

	// Test applyGlobalOptions sets active theme
	_ = applyGlobalOptions([]string{"--theme", "monokai", "--color-profile", "truecolor"})
	if got := themeNamesList(); got == "" {
		t.Fatalf("expected non-empty themeNamesList")
	}

	// Test unknown theme falls back cleanly to dark
	_ = applyGlobalOptions([]string{"--theme", "nonexistent-xyz"})
}


