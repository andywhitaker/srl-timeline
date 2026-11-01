package tui

import (
	"strings"
	"testing"

	"timeline/pkg/tui/modals"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/ansi"
)

// assertNoUnrecoveredReset verifies that every line starts with an SGR escape sequence,
// ends with \x1b[0m, and never has an inline \x1b[0m or \x1b[m followed by non-ANSI text
// without immediately re-opening an SGR sequence.
func assertNoUnrecoveredReset(t *testing.T, label, block string, wantWidth, wantHeight int) {
	t.Helper()
	lines := strings.Split(block, "\n")
	if wantHeight > 0 && len(lines) != wantHeight {
		t.Fatalf("%s: expected %d lines, got %d", label, wantHeight, len(lines))
	}
	for rowIdx, line := range lines {
		gotW := ansi.PrintableRuneWidth(line)
		if gotW != wantWidth {
			t.Fatalf("%s line %d: expected printable width %d, got %d: %q", label, rowIdx, wantWidth, gotW, line)
		}
		if !strings.HasPrefix(line, "\x1b[") {
			t.Fatalf("%s line %d: does not start with SGR prefix: %q", label, rowIdx, line)
		}
		if !strings.HasSuffix(line, "\x1b[0m") {
			t.Fatalf("%s line %d: does not end with SGR reset: %q", label, rowIdx, line)
		}

		// Strip the final trailing \x1b[0m and inspect all earlier resets
		body := strings.TrimSuffix(line, "\x1b[0m")
		for i := 0; i < len(body); i++ {
			if body[i] == '\x1b' && i+1 < len(body) && body[i+1] == '[' {
				j := i + 2
				for j < len(body) && ((body[j] >= '0' && body[j] <= '9') || body[j] == ';') {
					j++
				}
				if j < len(body) && body[j] == 'm' {
					params := body[i+2 : j]
					isReset := params == "" || params == "0" || params == "00" || strings.HasPrefix(params, "0;")
					if isReset {
						rest := body[j+1:]
						if rest != "" && !strings.HasPrefix(rest, "\x1b[") {
							t.Fatalf("%s line %d: unrecovered SGR reset at byte %d before %q in line %q",
								label, rowIdx, i, rest, line)
						}
					}
					i = j
				}
			}
		}
	}
}

func TestPaintLineAndSurfaceResetSafety(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	badge := StyleBadgeActive.Render("● RUNNING")
	title := StyleAppTitle.Render("srl-timeline")
	raw := title + "  " + badge + "  plain trailing text"

	painted := PaintBarLine(raw, 60, ColorSurface, ColorText)
	assertNoUnrecoveredReset(t, "PaintBarLine", painted, 60, 1)

	// Test wrapping of a long styled line across multiple rows
	longStyled := StyleDiffAddLine.Render("+ set / interface ethernet-1/1 subinterface 0 ipv4 address 192.0.2.1/30 admin-state enable")
	wrapped := PaintLine(longStyled, 32, ColorDiffAddBg, ColorSuccess)
	wrappedLines := strings.Split(wrapped, "\n")
	if len(wrappedLines) < 3 {
		t.Fatalf("expected long styled line to wrap into >=3 lines, got %d", len(wrappedLines))
	}
	assertNoUnrecoveredReset(t, "PaintLine-wrapped", wrapped, 32, len(wrappedLines))

	// Test PaintSurface height padding and clipping
	surface := PaintSurface(wrapped, 32, 6, ColorPanel, ColorText)
	assertNoUnrecoveredReset(t, "PaintSurface-padded", surface, 32, 6)

	clipped := PaintSurface(wrapped, 32, 2, ColorPanel, ColorText)
	assertNoUnrecoveredReset(t, "PaintSurface-clipped", clipped, 32, 2)
}

func TestBuiltinThemesAndAliases(t *testing.T) {
	_ = SetColorProfile("truecolor")
	defer func() {
		_, _ = SetTheme("dark")
	}()

	expectedThemes := []struct {
		query     string
		wantName  string
		wantDark  bool
		wantBgHex string
	}{
		{"dark", "dark", true, "#0d1117"},
		{"GitHub Dark", "dark", true, "#0d1117"},
		{"cobalt2", "cobalt2", true, "#193549"},
		{"Cobalt2", "cobalt2", true, "#193549"},
		{"monokai", "monokai", true, "#272822"},
		{"Monokai", "monokai", true, "#272822"},
		{"solarized-dark", "solarized-dark", true, "#002b36"},
		{"Solarized Dark", "solarized-dark", true, "#002b36"},
		{"solarized_dark", "solarized-dark", true, "#002b36"},
		{"dracula", "dracula", true, "#282a36"},
		{"Dracula", "dracula", true, "#282a36"},
		{"nord", "nord", true, "#2e3440"},
		{"Nord", "nord", true, "#2e3440"},
		{"catppuccin-mocha", "catppuccin-mocha", true, "#1e1e2e"},
		{"Catppuccin Mocha", "catppuccin-mocha", true, "#1e1e2e"},
		{"tokyo-night", "tokyo-night", true, "#1a1b26"},
		{"Tokyo Night", "tokyo-night", true, "#1a1b26"},
		{"gruvbox-dark", "gruvbox-dark", true, "#282828"},
		{"Gruvbox Dark", "gruvbox-dark", true, "#282828"},
		{"light", "light", false, "#f6f8fa"},
		{"GitHub Light", "light", false, "#f6f8fa"},
		{"solarized-light", "solarized-light", false, "#fdf6e3"},
		{"Solarized Light", "solarized-light", false, "#fdf6e3"},
	}

	for _, tc := range expectedThemes {
		theme, ok := SetTheme(tc.query)
		if !ok {
			t.Fatalf("SetTheme(%q) returned ok=false", tc.query)
		}
		if theme.Name != tc.wantName {
			t.Fatalf("SetTheme(%q).Name = %q, want %q", tc.query, theme.Name, tc.wantName)
		}
		if theme.IsDark != tc.wantDark {
			t.Fatalf("SetTheme(%q).IsDark = %v, want %v", tc.query, theme.IsDark, tc.wantDark)
		}
		if string(ColorBackground) != tc.wantBgHex {
			t.Fatalf("SetTheme(%q): tui.ColorBackground = %q, want %q", tc.query, ColorBackground, tc.wantBgHex)
		}
		if string(modals.ColorBackground) != tc.wantBgHex {
			t.Fatalf("SetTheme(%q): modals.ColorBackground = %q, want %q", tc.query, modals.ColorBackground, tc.wantBgHex)
		}
		if lipgloss.HasDarkBackground() != tc.wantDark {
			t.Fatalf("SetTheme(%q): lipgloss.HasDarkBackground() = %v, want %v", tc.query, lipgloss.HasDarkBackground(), tc.wantDark)
		}
	}

	// Unknown theme falls back to dark and returns ok=false
	fallback, ok := SetTheme("nonexistent-theme-xyz")
	if ok || fallback.Name != "dark" {
		t.Fatalf("expected fallback to dark with ok=false, got theme=%q ok=%v", fallback.Name, ok)
	}
}

func TestNextThemeCyclesAllThemes(t *testing.T) {
	_, _ = SetTheme("dark")
	all := AvailableThemes()
	if len(all) < 11 {
		t.Fatalf("expected at least 11 built-in themes, got %d", len(all))
	}

	seen := make(map[string]bool)
	seen[CurrentTheme().Name] = true
	for i := 0; i < len(all)-1; i++ {
		next := NextTheme()
		if seen[next.Name] {
			t.Fatalf("duplicate theme %q encountered at cycle step %d", next.Name, i)
		}
		seen[next.Name] = true
	}
	backToStart := NextTheme()
	if backToStart.Name != "dark" {
		t.Fatalf("expected full cycle to return to 'dark', got %q", backToStart.Name)
	}
}

func TestSetColorProfile(t *testing.T) {
	defer func() {
		_ = SetColorProfile("truecolor")
	}()

	for _, valid := range []string{"truecolor", "24bit", "256", "ansi256", "16", "ansi", "ascii", "none"} {
		if err := SetColorProfile(valid); err != nil {
			t.Fatalf("SetColorProfile(%q) unexpected error: %v", valid, err)
		}
	}
	if err := SetColorProfile("invalid-profile"); err == nil {
		t.Fatalf("expected error for invalid color profile")
	}
}

func TestPaintLineWideRuneTruncationAndWrapping(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	// 1. Wide rune overflowing the last 1 column of a truncated line before \x1b[0m:
	// "12345" is 5 cols, "界" is 2 cols -> at width=6, "界" overflows column 6.
	// The trailing \x1b[0m after "界" in line 1 must still be consumed so its bold/red SGR
	// does not leak into line 2 when rendered via PaintBarLine.
	boldRed := lipgloss.NewStyle().Bold(true).Foreground(ColorDanger).Background(ColorSurface)
	line1 := boldRed.Render("12345界")
	barOut := PaintBarLine(line1+"\nplain", 6, ColorSurface, ColorText)
	assertNoUnrecoveredReset(t, "PaintBarLine-wide-trunc", barOut, 6, 1)

	// 2. Wide rune wrapping across lines in PaintLine:
	// At width=7, "ab界cd界ef" -> "ab界cd" (6 cols, padded to 7) + "界ef" (4 cols, padded to 7)
	wideStyled := StyleDiffAddLine.Render("ab界cd界ef🔥gh")
	wrapped := PaintLine(wideStyled, 7, ColorDiffAddBg, ColorSuccess)
	wrappedLines := strings.Split(wrapped, "\n")
	if len(wrappedLines) < 2 {
		t.Fatalf("expected wide-rune line to wrap into >=2 lines, got %d", len(wrappedLines))
	}
	assertNoUnrecoveredReset(t, "PaintLine-wide-wrap", wrapped, 7, len(wrappedLines))
}

func TestStyleTextInputCursorNotDoubleReversed(t *testing.T) {
	_ = SetColorProfile("truecolor")
	_, _ = SetTheme("dark")

	fb := NewFilterBarModel(80)
	fb.Focus()

	if fb.TextInput.Cursor.Style.GetForeground() != ColorActive {
		t.Fatalf("expected Cursor.Style foreground to be ColorActive (%v) before bubbles/cursor Reverse(true), got %v",
			ColorActive, fb.TextInput.Cursor.Style.GetForeground())
	}
	if fb.TextInput.Cursor.Style.GetBackground() != ColorSurfaceFocus {
		t.Fatalf("expected Cursor.Style background to be ColorSurfaceFocus (%v) before bubbles/cursor Reverse(true), got %v",
			ColorSurfaceFocus, fb.TextInput.Cursor.Style.GetBackground())
	}
}

