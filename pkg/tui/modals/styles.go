package modals

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/reflow/truncate"
	"github.com/muesli/termenv"
)

// Theme defines a complete semantic color palette for the TUI.
type Theme struct {
	Name            string
	DisplayName     string
	Aliases         []string
	IsDark          bool
	Background      lipgloss.Color
	Surface         lipgloss.Color
	SurfaceFocus    lipgloss.Color
	Panel           lipgloss.Color
	BadgeBg         lipgloss.Color
	Highlight       lipgloss.Color
	Border          lipgloss.Color
	Active          lipgloss.Color
	Primary         lipgloss.Color
	Success         lipgloss.Color
	SuccessBg       lipgloss.Color
	Warning         lipgloss.Color
	WarningBg       lipgloss.Color
	OnWarning       lipgloss.Color
	Danger          lipgloss.Color
	DangerBg        lipgloss.Color
	Muted           lipgloss.Color
	Text            lipgloss.Color
	TextDim         lipgloss.Color
	Purple          lipgloss.Color
	Cyan            lipgloss.Color
	OnAccent        lipgloss.Color
	DiffAddBg       lipgloss.Color
	DiffDelBg       lipgloss.Color
	DiffHunkBg      lipgloss.Color
	SyntaxInterface lipgloss.Color
	SyntaxNetwork   lipgloss.Color
	SyntaxSystem    lipgloss.Color
	SyntaxACL       lipgloss.Color
	SyntaxValue     lipgloss.Color
}

var builtinThemes = []Theme{
	{
		Name:            "dark",
		DisplayName:     "GitHub Dark",
		Aliases:         []string{"github-dark", "default"},
		IsDark:          true,
		Background:      lipgloss.Color("#0d1117"),
		Surface:         lipgloss.Color("#161b22"),
		SurfaceFocus:    lipgloss.Color("#0d1b2a"),
		Panel:           lipgloss.Color("#090d13"),
		BadgeBg:         lipgloss.Color("#21262d"),
		Highlight:       lipgloss.Color("#1c2128"),
		Border:          lipgloss.Color("#30363d"),
		Active:          lipgloss.Color("#58a6ff"),
		Primary:         lipgloss.Color("#1f6feb"),
		Success:         lipgloss.Color("#3fb950"),
		SuccessBg:       lipgloss.Color("#238636"),
		Warning:         lipgloss.Color("#d29922"),
		WarningBg:       lipgloss.Color("#d29922"),
		OnWarning:       lipgloss.Color("#0d1117"),
		Danger:          lipgloss.Color("#f85149"),
		DangerBg:        lipgloss.Color("#da3633"),
		Muted:           lipgloss.Color("#8b949e"),
		Text:            lipgloss.Color("#e6edf3"),
		TextDim:         lipgloss.Color("#7d8590"),
		Purple:          lipgloss.Color("#bc8cff"),
		Cyan:            lipgloss.Color("#39c5cf"),
		OnAccent:        lipgloss.Color("#ffffff"),
		DiffAddBg:       lipgloss.Color("#12261e"),
		DiffDelBg:       lipgloss.Color("#2d1215"),
		DiffHunkBg:      lipgloss.Color("#0d1d30"),
		SyntaxInterface: lipgloss.Color("#79c0ff"),
		SyntaxNetwork:   lipgloss.Color("#d2a8ff"),
		SyntaxSystem:    lipgloss.Color("#56d364"),
		SyntaxACL:       lipgloss.Color("#ffa657"),
		SyntaxValue:     lipgloss.Color("#a5d6ff"),
	},
	{
		Name:            "cobalt2",
		DisplayName:     "Cobalt2",
		Aliases:         []string{"cobalt"},
		IsDark:          true,
		Background:      lipgloss.Color("#193549"),
		Surface:         lipgloss.Color("#15232d"),
		SurfaceFocus:    lipgloss.Color("#1f4662"),
		Panel:           lipgloss.Color("#122738"),
		BadgeBg:         lipgloss.Color("#1f4662"),
		Highlight:       lipgloss.Color("#1f4662"),
		Border:          lipgloss.Color("#0d3a58"),
		Active:          lipgloss.Color("#ffc600"),
		Primary:         lipgloss.Color("#0088ff"),
		Success:         lipgloss.Color("#3ad900"),
		SuccessBg:       lipgloss.Color("#1d7a00"),
		Warning:         lipgloss.Color("#ff9d00"),
		WarningBg:       lipgloss.Color("#ff9d00"),
		OnWarning:       lipgloss.Color("#122738"),
		Danger:          lipgloss.Color("#ff628c"),
		DangerBg:        lipgloss.Color("#c72c48"),
		Muted:           lipgloss.Color("#8095a8"),
		Text:            lipgloss.Color("#ffffff"),
		TextDim:         lipgloss.Color("#667f94"),
		Purple:          lipgloss.Color("#fb94ff"),
		Cyan:            lipgloss.Color("#80fcff"),
		OnAccent:        lipgloss.Color("#ffffff"),
		DiffAddBg:       lipgloss.Color("#143d29"),
		DiffDelBg:       lipgloss.Color("#3b1d2e"),
		DiffHunkBg:      lipgloss.Color("#173b56"),
		SyntaxInterface: lipgloss.Color("#80fcff"),
		SyntaxNetwork:   lipgloss.Color("#fb94ff"),
		SyntaxSystem:    lipgloss.Color("#3ad900"),
		SyntaxACL:       lipgloss.Color("#ffc600"),
		SyntaxValue:     lipgloss.Color("#9effff"),
	},
	{
		Name:            "monokai",
		DisplayName:     "Monokai",
		Aliases:         []string{"monokai-pro", "monokaipro"},
		IsDark:          true,
		Background:      lipgloss.Color("#272822"),
		Surface:         lipgloss.Color("#1e1f1c"),
		SurfaceFocus:    lipgloss.Color("#34352f"),
		Panel:           lipgloss.Color("#22231e"),
		BadgeBg:         lipgloss.Color("#3e3d32"),
		Highlight:       lipgloss.Color("#3e3d32"),
		Border:          lipgloss.Color("#49483e"),
		Active:          lipgloss.Color("#66d9ef"),
		Primary:         lipgloss.Color("#7c5cff"),
		Success:         lipgloss.Color("#a6e22e"),
		SuccessBg:       lipgloss.Color("#4d7012"),
		Warning:         lipgloss.Color("#fd971f"),
		WarningBg:       lipgloss.Color("#fd971f"),
		OnWarning:       lipgloss.Color("#1e1f1c"),
		Danger:          lipgloss.Color("#f92672"),
		DangerBg:        lipgloss.Color("#b0154b"),
		Muted:           lipgloss.Color("#908c77"),
		Text:            lipgloss.Color("#f8f8f2"),
		TextDim:         lipgloss.Color("#75715e"),
		Purple:          lipgloss.Color("#ae81ff"),
		Cyan:            lipgloss.Color("#66d9ef"),
		OnAccent:        lipgloss.Color("#ffffff"),
		DiffAddBg:       lipgloss.Color("#29371e"),
		DiffDelBg:       lipgloss.Color("#3d1e28"),
		DiffHunkBg:      lipgloss.Color("#253338"),
		SyntaxInterface: lipgloss.Color("#66d9ef"),
		SyntaxNetwork:   lipgloss.Color("#ae81ff"),
		SyntaxSystem:    lipgloss.Color("#a6e22e"),
		SyntaxACL:       lipgloss.Color("#fd971f"),
		SyntaxValue:     lipgloss.Color("#e6db74"),
	},
	{
		Name:            "solarized-dark",
		DisplayName:     "Solarized Dark",
		Aliases:         []string{"solarized", "solarizeddark"},
		IsDark:          true,
		Background:      lipgloss.Color("#002b36"),
		Surface:         lipgloss.Color("#073642"),
		SurfaceFocus:    lipgloss.Color("#0b4654"),
		Panel:           lipgloss.Color("#00212b"),
		BadgeBg:         lipgloss.Color("#0d4352"),
		Highlight:       lipgloss.Color("#0b4251"),
		Border:          lipgloss.Color("#284b54"),
		Active:          lipgloss.Color("#268bd2"),
		Primary:         lipgloss.Color("#268bd2"),
		Success:         lipgloss.Color("#859900"),
		SuccessBg:       lipgloss.Color("#4d6b00"),
		Warning:         lipgloss.Color("#b58900"),
		WarningBg:       lipgloss.Color("#c99a00"),
		OnWarning:       lipgloss.Color("#001a22"),
		Danger:          lipgloss.Color("#dc322f"),
		DangerBg:        lipgloss.Color("#a8201d"),
		Muted:           lipgloss.Color("#839496"),
		Text:            lipgloss.Color("#eee8d5"),
		TextDim:         lipgloss.Color("#657b83"),
		Purple:          lipgloss.Color("#6c71c4"),
		Cyan:            lipgloss.Color("#2aa198"),
		OnAccent:        lipgloss.Color("#fdf6e3"),
		DiffAddBg:       lipgloss.Color("#073b2d"),
		DiffDelBg:       lipgloss.Color("#361c24"),
		DiffHunkBg:      lipgloss.Color("#07364a"),
		SyntaxInterface: lipgloss.Color("#268bd2"),
		SyntaxNetwork:   lipgloss.Color("#6c71c4"),
		SyntaxSystem:    lipgloss.Color("#859900"),
		SyntaxACL:       lipgloss.Color("#cb4b16"),
		SyntaxValue:     lipgloss.Color("#2aa198"),
	},
	{
		Name:            "dracula",
		DisplayName:     "Dracula",
		Aliases:         []string{},
		IsDark:          true,
		Background:      lipgloss.Color("#282a36"),
		Surface:         lipgloss.Color("#21222c"),
		SurfaceFocus:    lipgloss.Color("#343746"),
		Panel:           lipgloss.Color("#191a21"),
		BadgeBg:         lipgloss.Color("#343746"),
		Highlight:       lipgloss.Color("#44475a"),
		Border:          lipgloss.Color("#44475a"),
		Active:          lipgloss.Color("#bd93f9"),
		Primary:         lipgloss.Color("#6272a4"),
		Success:         lipgloss.Color("#50fa7b"),
		SuccessBg:       lipgloss.Color("#267d3d"),
		Warning:         lipgloss.Color("#ffb86c"),
		WarningBg:       lipgloss.Color("#ffb86c"),
		OnWarning:       lipgloss.Color("#191a21"),
		Danger:          lipgloss.Color("#ff5555"),
		DangerBg:        lipgloss.Color("#b82c2c"),
		Muted:           lipgloss.Color("#92a1cf"),
		Text:            lipgloss.Color("#f8f8f2"),
		TextDim:         lipgloss.Color("#6272a4"),
		Purple:          lipgloss.Color("#ff79c6"),
		Cyan:            lipgloss.Color("#8be9fd"),
		OnAccent:        lipgloss.Color("#ffffff"),
		DiffAddBg:       lipgloss.Color("#1f3a2e"),
		DiffDelBg:       lipgloss.Color("#3f232c"),
		DiffHunkBg:      lipgloss.Color("#292d42"),
		SyntaxInterface: lipgloss.Color("#8be9fd"),
		SyntaxNetwork:   lipgloss.Color("#ff79c6"),
		SyntaxSystem:    lipgloss.Color("#50fa7b"),
		SyntaxACL:       lipgloss.Color("#ffb86c"),
		SyntaxValue:     lipgloss.Color("#f1fa8c"),
	},
	{
		Name:            "nord",
		DisplayName:     "Nord",
		Aliases:         []string{},
		IsDark:          true,
		Background:      lipgloss.Color("#2e3440"),
		Surface:         lipgloss.Color("#3b4252"),
		SurfaceFocus:    lipgloss.Color("#434c5e"),
		Panel:           lipgloss.Color("#242933"),
		BadgeBg:         lipgloss.Color("#434c5e"),
		Highlight:       lipgloss.Color("#434c5e"),
		Border:          lipgloss.Color("#4c566a"),
		Active:          lipgloss.Color("#88c0d0"),
		Primary:         lipgloss.Color("#5e81ac"),
		Success:         lipgloss.Color("#a3be8c"),
		SuccessBg:       lipgloss.Color("#4f6b3e"),
		Warning:         lipgloss.Color("#ebcb8b"),
		WarningBg:       lipgloss.Color("#ebcb8b"),
		OnWarning:       lipgloss.Color("#242933"),
		Danger:          lipgloss.Color("#bf616a"),
		DangerBg:        lipgloss.Color("#943d46"),
		Muted:           lipgloss.Color("#a5b1c2"),
		Text:            lipgloss.Color("#eceff4"),
		TextDim:         lipgloss.Color("#818d9f"),
		Purple:          lipgloss.Color("#b48ead"),
		Cyan:            lipgloss.Color("#8fbcbb"),
		OnAccent:        lipgloss.Color("#eceff4"),
		DiffAddBg:       lipgloss.Color("#2b3b34"),
		DiffDelBg:       lipgloss.Color("#3f2b32"),
		DiffHunkBg:      lipgloss.Color("#2b3847"),
		SyntaxInterface: lipgloss.Color("#88c0d0"),
		SyntaxNetwork:   lipgloss.Color("#b48ead"),
		SyntaxSystem:    lipgloss.Color("#a3be8c"),
		SyntaxACL:       lipgloss.Color("#d08770"),
		SyntaxValue:     lipgloss.Color("#81a1c1"),
	},
	{
		Name:            "catppuccin-mocha",
		DisplayName:     "Catppuccin Mocha",
		Aliases:         []string{"catppuccin", "mocha", "catppuccinmocha"},
		IsDark:          true,
		Background:      lipgloss.Color("#1e1e2e"),
		Surface:         lipgloss.Color("#181825"),
		SurfaceFocus:    lipgloss.Color("#313244"),
		Panel:           lipgloss.Color("#11111b"),
		BadgeBg:         lipgloss.Color("#313244"),
		Highlight:       lipgloss.Color("#313244"),
		Border:          lipgloss.Color("#45475a"),
		Active:          lipgloss.Color("#89b4fa"),
		Primary:         lipgloss.Color("#4669b8"),
		Success:         lipgloss.Color("#a6e3a1"),
		SuccessBg:       lipgloss.Color("#357a38"),
		Warning:         lipgloss.Color("#f9e2af"),
		WarningBg:       lipgloss.Color("#f9e2af"),
		OnWarning:       lipgloss.Color("#11111b"),
		Danger:          lipgloss.Color("#f38ba8"),
		DangerBg:        lipgloss.Color("#b5385a"),
		Muted:           lipgloss.Color("#a6adc8"),
		Text:            lipgloss.Color("#cdd6f4"),
		TextDim:         lipgloss.Color("#7f849c"),
		Purple:          lipgloss.Color("#cba6f7"),
		Cyan:            lipgloss.Color("#94e2d5"),
		OnAccent:        lipgloss.Color("#ffffff"),
		DiffAddBg:       lipgloss.Color("#1c312b"),
		DiffDelBg:       lipgloss.Color("#361f2c"),
		DiffHunkBg:      lipgloss.Color("#1e283b"),
		SyntaxInterface: lipgloss.Color("#89b4fa"),
		SyntaxNetwork:   lipgloss.Color("#cba6f7"),
		SyntaxSystem:    lipgloss.Color("#a6e3a1"),
		SyntaxACL:       lipgloss.Color("#fab387"),
		SyntaxValue:     lipgloss.Color("#89dceb"),
	},
	{
		Name:            "tokyo-night",
		DisplayName:     "Tokyo Night",
		Aliases:         []string{"tokyonight", "tokyo"},
		IsDark:          true,
		Background:      lipgloss.Color("#1a1b26"),
		Surface:         lipgloss.Color("#24283b"),
		SurfaceFocus:    lipgloss.Color("#292e42"),
		Panel:           lipgloss.Color("#16161e"),
		BadgeBg:         lipgloss.Color("#292e42"),
		Highlight:       lipgloss.Color("#292e42"),
		Border:          lipgloss.Color("#3b4261"),
		Active:          lipgloss.Color("#7aa2f7"),
		Primary:         lipgloss.Color("#3d59a1"),
		Success:         lipgloss.Color("#9ece6a"),
		SuccessBg:       lipgloss.Color("#3b6e28"),
		Warning:         lipgloss.Color("#e0af68"),
		WarningBg:       lipgloss.Color("#e0af68"),
		OnWarning:       lipgloss.Color("#16161e"),
		Danger:          lipgloss.Color("#f7768e"),
		DangerBg:        lipgloss.Color("#b83952"),
		Muted:           lipgloss.Color("#7982a9"),
		Text:            lipgloss.Color("#c0caf5"),
		TextDim:         lipgloss.Color("#565f89"),
		Purple:          lipgloss.Color("#bb9af7"),
		Cyan:            lipgloss.Color("#7dcfff"),
		OnAccent:        lipgloss.Color("#ffffff"),
		DiffAddBg:       lipgloss.Color("#1d2f28"),
		DiffDelBg:       lipgloss.Color("#371f29"),
		DiffHunkBg:      lipgloss.Color("#1e263a"),
		SyntaxInterface: lipgloss.Color("#7aa2f7"),
		SyntaxNetwork:   lipgloss.Color("#bb9af7"),
		SyntaxSystem:    lipgloss.Color("#9ece6a"),
		SyntaxACL:       lipgloss.Color("#ff9e64"),
		SyntaxValue:     lipgloss.Color("#7dcfff"),
	},
	{
		Name:            "gruvbox-dark",
		DisplayName:     "Gruvbox Dark",
		Aliases:         []string{"gruvbox", "gruvboxdark"},
		IsDark:          true,
		Background:      lipgloss.Color("#282828"),
		Surface:         lipgloss.Color("#3c3836"),
		SurfaceFocus:    lipgloss.Color("#504945"),
		Panel:           lipgloss.Color("#1d2021"),
		BadgeBg:         lipgloss.Color("#504945"),
		Highlight:       lipgloss.Color("#3c3836"),
		Border:          lipgloss.Color("#665c54"),
		Active:          lipgloss.Color("#83a598"),
		Primary:         lipgloss.Color("#458588"),
		Success:         lipgloss.Color("#b8bb26"),
		SuccessBg:       lipgloss.Color("#687016"),
		Warning:         lipgloss.Color("#fabd2f"),
		WarningBg:       lipgloss.Color("#fabd2f"),
		OnWarning:       lipgloss.Color("#1d2021"),
		Danger:          lipgloss.Color("#fb4934"),
		DangerBg:        lipgloss.Color("#9d0006"),
		Muted:           lipgloss.Color("#a89984"),
		Text:            lipgloss.Color("#ebdbb2"),
		TextDim:         lipgloss.Color("#928374"),
		Purple:          lipgloss.Color("#d3869b"),
		Cyan:            lipgloss.Color("#8ec07c"),
		OnAccent:        lipgloss.Color("#fbf1c7"),
		DiffAddBg:       lipgloss.Color("#2d361e"),
		DiffDelBg:       lipgloss.Color("#402120"),
		DiffHunkBg:      lipgloss.Color("#223235"),
		SyntaxInterface: lipgloss.Color("#83a598"),
		SyntaxNetwork:   lipgloss.Color("#d3869b"),
		SyntaxSystem:    lipgloss.Color("#b8bb26"),
		SyntaxACL:       lipgloss.Color("#fe8019"),
		SyntaxValue:     lipgloss.Color("#8ec07c"),
	},
	{
		Name:            "light",
		DisplayName:     "GitHub Light",
		Aliases:         []string{"github-light", "githublight"},
		IsDark:          false,
		Background:      lipgloss.Color("#f6f8fa"),
		Surface:         lipgloss.Color("#eaeef2"),
		SurfaceFocus:    lipgloss.Color("#ddf4ff"),
		Panel:           lipgloss.Color("#ffffff"),
		BadgeBg:         lipgloss.Color("#d0d7de"),
		Highlight:       lipgloss.Color("#ddf4ff"),
		Border:          lipgloss.Color("#afb8c1"),
		Active:          lipgloss.Color("#0969da"),
		Primary:         lipgloss.Color("#0969da"),
		Success:         lipgloss.Color("#1a7f37"),
		SuccessBg:       lipgloss.Color("#1a7f37"),
		Warning:         lipgloss.Color("#9a6700"),
		WarningBg:       lipgloss.Color("#8a5a00"),
		OnWarning:       lipgloss.Color("#ffffff"),
		Danger:          lipgloss.Color("#cf222e"),
		DangerBg:        lipgloss.Color("#cf222e"),
		Muted:           lipgloss.Color("#57606a"),
		Text:            lipgloss.Color("#1f2328"),
		TextDim:         lipgloss.Color("#6e7781"),
		Purple:          lipgloss.Color("#8250df"),
		Cyan:            lipgloss.Color("#0550ae"),
		OnAccent:        lipgloss.Color("#ffffff"),
		DiffAddBg:       lipgloss.Color("#dafbe1"),
		DiffDelBg:       lipgloss.Color("#ffebe9"),
		DiffHunkBg:      lipgloss.Color("#ddf4ff"),
		SyntaxInterface: lipgloss.Color("#0550ae"),
		SyntaxNetwork:   lipgloss.Color("#8250df"),
		SyntaxSystem:    lipgloss.Color("#1a7f37"),
		SyntaxACL:       lipgloss.Color("#953800"),
		SyntaxValue:     lipgloss.Color("#0a3069"),
	},
	{
		Name:            "solarized-light",
		DisplayName:     "Solarized Light",
		Aliases:         []string{"solarizedlight"},
		IsDark:          false,
		Background:      lipgloss.Color("#fdf6e3"),
		Surface:         lipgloss.Color("#eee8d5"),
		SurfaceFocus:    lipgloss.Color("#e2dec9"),
		Panel:           lipgloss.Color("#fefbf1"),
		BadgeBg:         lipgloss.Color("#dfdac6"),
		Highlight:       lipgloss.Color("#e6dfcb"),
		Border:          lipgloss.Color("#93a1a1"),
		Active:          lipgloss.Color("#268bd2"),
		Primary:         lipgloss.Color("#268bd2"),
		Success:         lipgloss.Color("#5f6e00"),
		SuccessBg:       lipgloss.Color("#5f6e00"),
		Warning:         lipgloss.Color("#b58900"),
		WarningBg:       lipgloss.Color("#d4a216"),
		OnWarning:       lipgloss.Color("#001a22"),
		Danger:          lipgloss.Color("#dc322f"),
		DangerBg:        lipgloss.Color("#b82523"),
		Muted:           lipgloss.Color("#586e75"),
		Text:            lipgloss.Color("#073642"),
		TextDim:         lipgloss.Color("#657b83"),
		Purple:          lipgloss.Color("#6c71c4"),
		Cyan:            lipgloss.Color("#2aa198"),
		OnAccent:        lipgloss.Color("#fdf6e3"),
		DiffAddBg:       lipgloss.Color("#e5f0d5"),
		DiffDelBg:       lipgloss.Color("#f8dedc"),
		DiffHunkBg:      lipgloss.Color("#ddebf2"),
		SyntaxInterface: lipgloss.Color("#268bd2"),
		SyntaxNetwork:   lipgloss.Color("#6c71c4"),
		SyntaxSystem:    lipgloss.Color("#5f6e00"),
		SyntaxACL:       lipgloss.Color("#cb4b16"),
		SyntaxValue:     lipgloss.Color("#2aa198"),
	},
}

var (
	activeThemeIndex = 0
	themeListeners   []func(Theme)

	// Modal-accessible palette tokens
	ColorBackground   lipgloss.Color
	ColorSurface      lipgloss.Color
	ColorSurfaceFocus lipgloss.Color
	ColorPanel        lipgloss.Color
	ColorBadgeBg      lipgloss.Color
	ColorHighlight    lipgloss.Color
	ColorBorder       lipgloss.Color
	ColorActive       lipgloss.Color
	ColorPrimary      lipgloss.Color
	ColorSuccess      lipgloss.Color
	ColorSuccessBg    lipgloss.Color
	ColorWarning      lipgloss.Color
	ColorWarningBg    lipgloss.Color
	ColorOnWarning    lipgloss.Color
	ColorDanger       lipgloss.Color
	ColorDangerBg     lipgloss.Color
	ColorMuted        lipgloss.Color
	ColorText         lipgloss.Color
	ColorTextDim      lipgloss.Color
	ColorPurple       lipgloss.Color
	ColorCyan         lipgloss.Color
	ColorOnAccent     lipgloss.Color
)

func init() {
	if os.Getenv("NO_COLOR") == "" {
		lipgloss.SetColorProfile(termenv.TrueColor)
	}
	applyModalTheme(builtinThemes[0])
	if envProfile := os.Getenv("TIMELINE_COLOR_PROFILE"); envProfile != "" {
		_ = SetColorProfile(envProfile)
	}
	if envTheme := os.Getenv("TIMELINE_THEME"); envTheme != "" {
		_, _ = SetTheme(envTheme)
	}
}

// AvailableThemes returns a copy of all registered themes.
func AvailableThemes() []Theme {
	out := make([]Theme, len(builtinThemes))
	copy(out, builtinThemes)
	return out
}

// CurrentTheme returns the currently active Theme.
func CurrentTheme() Theme {
	return builtinThemes[activeThemeIndex]
}

// NormalizeThemeName canonicalizes a theme identifier for matching.
func NormalizeThemeName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.Join(strings.Fields(s), "-")
	return s
}

// RegisterThemeListener registers a callback invoked whenever the theme or color profile changes.
func RegisterThemeListener(fn func(Theme)) {
	themeListeners = append(themeListeners, fn)
	fn(builtinThemes[activeThemeIndex])
}

func applyModalTheme(t Theme) {
	baseSGRCacheMu.Lock()
	clear(baseSGRCache)
	clear(styleSGRCache)
	baseSGRCacheMu.Unlock()

	lipgloss.SetHasDarkBackground(t.IsDark)
	ColorBackground = t.Background
	ColorSurface = t.Surface
	ColorSurfaceFocus = t.SurfaceFocus
	ColorPanel = t.Panel
	ColorBadgeBg = t.BadgeBg
	ColorHighlight = t.Highlight
	ColorBorder = t.Border
	ColorActive = t.Active
	ColorPrimary = t.Primary
	ColorSuccess = t.Success
	ColorSuccessBg = t.SuccessBg
	ColorWarning = t.Warning
	ColorWarningBg = t.WarningBg
	ColorOnWarning = t.OnWarning
	ColorDanger = t.Danger
	ColorDangerBg = t.DangerBg
	ColorMuted = t.Muted
	ColorText = t.Text
	ColorTextDim = t.TextDim
	ColorPurple = t.Purple
	ColorCyan = t.Cyan
	ColorOnAccent = t.OnAccent

	for _, fn := range themeListeners {
		fn(t)
	}
}

// SetTheme activates the theme matching name (by Name, DisplayName, or Alias).
// Returns the activated theme and true if matched, or the default dark theme and false if unknown.
func SetTheme(name string) (Theme, bool) {
	norm := NormalizeThemeName(name)
	if norm == "" {
		activeThemeIndex = 0
		applyModalTheme(builtinThemes[0])
		return builtinThemes[0], true
	}
	for idx, t := range builtinThemes {
		if NormalizeThemeName(t.Name) == norm || NormalizeThemeName(t.DisplayName) == norm {
			activeThemeIndex = idx
			applyModalTheme(t)
			return t, true
		}
		for _, alias := range t.Aliases {
			if NormalizeThemeName(alias) == norm {
				activeThemeIndex = idx
				applyModalTheme(t)
				return t, true
			}
		}
	}
	activeThemeIndex = 0
	applyModalTheme(builtinThemes[0])
	return builtinThemes[0], false
}

// NextTheme cycles to the next theme in the registry and returns it.
func NextTheme() Theme {
	activeThemeIndex = (activeThemeIndex + 1) % len(builtinThemes)
	t := builtinThemes[activeThemeIndex]
	applyModalTheme(t)
	return t
}

// SetColorProfile configures the Lip Gloss color profile ("truecolor", "256", "16", "ascii").
func SetColorProfile(profile string) error {
	norm := strings.ToLower(strings.TrimSpace(profile))
	switch norm {
	case "", "auto":
		if os.Getenv("NO_COLOR") != "" {
			lipgloss.SetColorProfile(termenv.Ascii)
		} else {
			lipgloss.SetColorProfile(termenv.TrueColor)
		}
	case "truecolor", "24bit", "24-bit", "true":
		lipgloss.SetColorProfile(termenv.TrueColor)
	case "256", "ansi256", "8bit", "8-bit":
		lipgloss.SetColorProfile(termenv.ANSI256)
	case "16", "ansi", "4bit", "4-bit":
		lipgloss.SetColorProfile(termenv.ANSI)
	case "ascii", "none", "nocolor", "no-color", "off":
		lipgloss.SetColorProfile(termenv.Ascii)
	default:
		return fmt.Errorf("unknown color profile %q (expected truecolor, 256, 16, or ascii)", profile)
	}
	applyModalTheme(builtinThemes[activeThemeIndex])
	return nil
}

// StyleTextInput applies explicit foreground and background styles to a textinput.Model.
// Note: bubbles/cursor renders focused cursors with .Reverse(true), so Cursor.Style sets
// Foreground(accentFg).Background(bg) to produce an accentFg block with bg text when reversed.
func StyleTextInput(ti *textinput.Model, bg, fg, dimFg, accentFg lipgloss.Color) {
	ti.PromptStyle = lipgloss.NewStyle().Bold(true).Foreground(accentFg).Background(bg)
	ti.TextStyle = lipgloss.NewStyle().Foreground(fg).Background(bg)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(dimFg).Background(bg)
	ti.CompletionStyle = lipgloss.NewStyle().Foreground(dimFg).Background(bg)
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(accentFg).Background(bg)
	ti.Cursor.TextStyle = lipgloss.NewStyle().Foreground(fg).Background(bg)
}

type baseSGRKey struct {
	bg lipgloss.Color
	fg lipgloss.Color
}

type styleSGRKey struct {
	bg        lipgloss.Color
	fg        lipgloss.Color
	bold      bool
	italic    bool
	underline bool
}

var (
	baseSGRCacheMu sync.RWMutex
	baseSGRCache   = make(map[baseSGRKey]string, 64)
	styleSGRCache  = make(map[styleSGRKey]string, 128)
)

// StyleSGR returns the cached ANSI SGR opening prefix for the given colors and text attributes
// under the active Lip Gloss color profile (or "" in ASCII/no-color mode).
func StyleSGR(bg, fg lipgloss.Color, bold, italic, underline bool) string {
	key := styleSGRKey{bg: bg, fg: fg, bold: bold, italic: italic, underline: underline}
	baseSGRCacheMu.RLock()
	if cached, ok := styleSGRCache[key]; ok {
		baseSGRCacheMu.RUnlock()
		return cached
	}
	baseSGRCacheMu.RUnlock()

	st := lipgloss.NewStyle()
	if bg != "" {
		st = st.Background(bg)
	}
	if fg != "" {
		st = st.Foreground(fg)
	}
	if bold {
		st = st.Bold(true)
	}
	if italic {
		st = st.Italic(true)
	}
	if underline {
		st = st.Underline(true)
	}
	rendered := st.Render("\x00")
	idx := strings.IndexByte(rendered, '\x00')
	var res string
	if idx > 0 {
		res = rendered[:idx]
	}

	baseSGRCacheMu.Lock()
	styleSGRCache[key] = res
	baseSGRCacheMu.Unlock()
	return res
}

// WrapSGR wraps text in sgr + text + "\x1b[0m" when sgr is non-empty, else returns text verbatim.
func WrapSGR(sgr, text string) string {
	if sgr == "" || text == "" {
		return text
	}
	return sgr + text + "\x1b[0m"
}

// fastRuneWidth returns the terminal cell width of r, fast-pathing printable ASCII and
// deterministic single-width box-drawing/block/geometric/arrow glyphs used across the TUI.
func fastRuneWidth(r rune) int {
	if r >= 0x20 && r < 0x7f {
		return 1
	}
	if (r >= 0x2500 && r <= 0x25FF) || (r >= 0x2190 && r <= 0x21FF) || r == 0x00B7 || r == 0x2026 || r == 0x2713 || r == 0x2715 || r == 0x00D7 {
		return 1
	}
	return runewidth.RuneWidth(r)
}

func baseSGR(bg, fg lipgloss.Color) string {
	key := baseSGRKey{bg: bg, fg: fg}
	baseSGRCacheMu.RLock()
	if cached, ok := baseSGRCache[key]; ok {
		baseSGRCacheMu.RUnlock()
		return cached
	}
	baseSGRCacheMu.RUnlock()

	st := lipgloss.NewStyle()
	if bg != "" {
		st = st.Background(bg)
	}
	if fg != "" {
		st = st.Foreground(fg)
	}
	rendered := st.Render("\x00")
	idx := strings.IndexByte(rendered, '\x00')
	var res string
	if idx > 0 {
		res = rendered[:idx]
	}

	baseSGRCacheMu.Lock()
	baseSGRCache[key] = res
	baseSGRCacheMu.Unlock()
	return res
}

// skipANSISeq advances past a CSI (\x1b[...) or OSC (\x1b]...) escape sequence starting at s[i].
// Returns the index immediately after the sequence, or -1 if s[i] does not start a recognized sequence.
func skipANSISeq(s string, i int) int {
	n := len(s)
	if i+1 >= n || s[i] != '\x1b' {
		return -1
	}
	if s[i+1] == '[' {
		end := i + 2
		for end < n && (s[end] < 0x40 || s[end] > 0x7E) {
			end++
		}
		if end < n {
			return end + 1
		}
		return end
	}
	if s[i+1] == ']' {
		end := i + 2
		for end < n {
			if s[end] == '\x07' {
				return end + 1
			}
			if s[end] == '\x1b' && end+1 < n && s[end+1] == '\\' {
				return end + 2
			}
			end++
		}
		return end
	}
	return -1
}

// fastPrintableWidth computes the printable cell width of s ignoring ANSI CSI/OSC sequences,
// fast-pathing printable ASCII bytes without UTF-8 or runewidth calls.
func fastPrintableWidth(s string) int {
	w := 0
	n := len(s)
	i := 0
	for i < n {
		c := s[i]
		if c == '\x1b' {
			if next := skipANSISeq(s, i); next > i {
				i = next
				continue
			}
		}
		if c >= 0x20 && c < 0x7f {
			w++
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			i++
			continue
		}
		w += fastRuneWidth(r)
		i += size
	}
	return w
}

// isAlreadyPaintedLine reports whether line is already a single-row, exact-width,
// full-bleed ANSI-safe string (starts with SGR, ends with \x1b[0m, no unrecovered resets,
// no \r/\n/\t, and printable width == width).
func isAlreadyPaintedLine(line string, width int) bool {
	if width <= 0 || len(line) < 7 {
		return false
	}
	if !strings.HasPrefix(line, "\x1b[") || !strings.HasSuffix(line, "\x1b[0m") {
		return false
	}
	bodyEnd := len(line) - 4 // exclude trailing "\x1b[0m"
	w := 0
	i := 0
	for i < bodyEnd {
		c := line[i]
		if c == '\r' || c == '\n' || c == '\t' {
			return false
		}
		if c == '\x1b' {
			if i+1 < bodyEnd && line[i+1] == '[' {
				end := i + 2
				for end < bodyEnd && (line[end] < 0x40 || line[end] > 0x7E) {
					end++
				}
				if end >= bodyEnd {
					return false
				}
				if line[end] == 'm' {
					params := line[i+2 : end]
					if params == "" || params == "0" || params == "00" || strings.HasPrefix(params, "0;") {
						next := end + 1
						if next < bodyEnd && !(line[next] == '\x1b' && next+1 < bodyEnd && line[next+1] == '[') {
							return false
						}
					}
				}
				i = end + 1
				continue
			}
			if next := skipANSISeq(line, i); next > i && next <= bodyEnd {
				i = next
				continue
			}
		}
		if c >= 0x20 && c < 0x7f {
			w++
			if w > width {
				return false
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:bodyEnd])
		if r == utf8.RuneError && size == 1 {
			i++
			continue
		}
		w += fastRuneWidth(r)
		if w > width {
			return false
		}
		i += size
	}
	return w == width
}

// nextSGRSetsBg reports whether s begins immediately with an ANSI SGR sequence that
// explicitly sets a background color (40-47, 48;..., or 100-107).
func nextSGRSetsBg(s string) bool {
	if len(s) < 4 || s[0] != '\x1b' || s[1] != '[' {
		return false
	}
	end := 2
	for end < len(s) && (s[end] < 0x40 || s[end] > 0x7E) {
		end++
	}
	if end >= len(s) || s[end] != 'm' {
		return false
	}
	params := s[2:end]
	if params == "" {
		return false
	}
	// Parse semicolon-separated numeric codes without allocating.
	p := 0
	for p < len(params) {
		semi := strings.IndexByte(params[p:], ';')
		var tok string
		if semi < 0 {
			tok = params[p:]
			p = len(params)
		} else {
			tok = params[p : p+semi]
			p += semi + 1
		}
		switch tok {
		case "48":
			return true
		case "40", "41", "42", "43", "44", "45", "46", "47",
			"100", "101", "102", "103", "104", "105", "106", "107":
			return true
		case "38":
			// Skip 38;5;N or 38;2;R;G;B so color components aren't misread as bg codes.
			if p < len(params) {
				subSemi := strings.IndexByte(params[p:], ';')
				var mode string
				if subSemi < 0 {
					mode = params[p:]
					p = len(params)
				} else {
					mode = params[p : p+subSemi]
					p += subSemi + 1
				}
				skip := 0
				if mode == "5" {
					skip = 1
				} else if mode == "2" {
					skip = 3
				}
				for k := 0; k < skip && p < len(params); k++ {
					ns := strings.IndexByte(params[p:], ';')
					if ns < 0 {
						p = len(params)
					} else {
						p += ns + 1
					}
				}
			}
		}
	}
	return false
}

type ansiToken struct {
	raw     string
	width   int
	isSpace bool
}

func tokenizeANSI(s string) []ansiToken {
	var tokens []ansiToken
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			if i+1 < len(s) && s[i+1] == '[' {
				end := i + 2
				for end < len(s) && (s[end] < 0x40 || s[end] > 0x7E) {
					end++
				}
				if end < len(s) {
					end++
				}
				tokens = append(tokens, ansiToken{raw: s[i:end], width: 0})
				i = end
				continue
			} else if i+1 < len(s) && s[i+1] == ']' {
				end := i + 2
				for end < len(s) {
					if s[end] == '\x07' {
						end++
						break
					}
					if s[end] == '\x1b' && end+1 < len(s) && s[end+1] == '\\' {
						end += 2
						break
					}
					end++
				}
				tokens = append(tokens, ansiToken{raw: s[i:end], width: 0})
				i = end
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			i++
			continue
		}
		rw := runewidth.RuneWidth(r)
		tokens = append(tokens, ansiToken{
			raw:     s[i : i+size],
			width:   rw,
			isSpace: r == ' ',
		})
		i += size
	}
	return tokens
}

func joinTokens(tokens []ansiToken) string {
	var b strings.Builder
	for _, t := range tokens {
		b.WriteString(t.raw)
	}
	return b.String()
}

func wrapANSILine(seg string, width int) []string {
	if width <= 0 || fastPrintableWidth(seg) <= width {
		return []string{seg}
	}
	tokens := tokenizeANSI(seg)
	var lines []string
	lineStart := 0
	for lineStart < len(tokens) {
		visW := 0
		seenNonSpace := false
		lastBreakIdx := -1
		i := lineStart
		wrapped := false
		for i < len(tokens) {
			tok := tokens[i]
			if tok.width == 0 {
				i++
				continue
			}
			if visW+tok.width > width && visW > 0 {
				if lastBreakIdx != -1 {
					lines = append(lines, joinTokens(tokens[lineStart:lastBreakIdx]))
					lineStart = lastBreakIdx + 1
				} else {
					lines = append(lines, joinTokens(tokens[lineStart:i]))
					lineStart = i
				}
				wrapped = true
				break
			}
			visW += tok.width
			if tok.isSpace {
				if seenNonSpace {
					lastBreakIdx = i
				}
			} else {
				seenNonSpace = true
			}
			i++
		}
		if !wrapped {
			lines = append(lines, joinTokens(tokens[lineStart:]))
			break
		}
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

// hasRemainingPrintable returns true as soon as any printable cell is found in s (O(1) amortized).
func hasRemainingPrintable(s string) bool {
	n := len(s)
	i := 0
	for i < n {
		c := s[i]
		if c == '\x1b' {
			if next := skipANSISeq(s, i); next > i {
				i = next
				continue
			}
		}
		if c >= 0x20 && c < 0x7f {
			return true
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			i++
			continue
		}
		if fastRuneWidth(r) > 0 {
			return true
		}
		i += size
	}
	return false
}

// PaintLine wraps line if width > 0 and its visual width exceeds width, carries active SGR state
// across wrapped lines, injects base (bg + defaultFg) at the start of every line and immediately
// after every embedded \x1b[0m or \x1b[m reset, and pads to width.
func PaintLine(line string, width int, bg, defaultFg lipgloss.Color) string {
	if strings.IndexByte(line, '\r') >= 0 {
		line = strings.ReplaceAll(line, "\r", "")
	}
	if strings.IndexByte(line, '\t') >= 0 {
		line = strings.ReplaceAll(line, "\t", "    ")
	}

	var rawLines []string
	if strings.IndexByte(line, '\n') < 0 {
		if width > 0 && fastPrintableWidth(line) > width {
			rawLines = wrapANSILine(line, width)
		} else {
			rawLines = []string{line}
		}
	} else {
		for _, seg := range strings.Split(line, "\n") {
			if width > 0 && fastPrintableWidth(seg) > width {
				rawLines = append(rawLines, wrapANSILine(seg, width)...)
			} else {
				rawLines = append(rawLines, seg)
			}
		}
	}

	base := baseSGR(bg, defaultFg)
	var activeSGRs []string
	outLines := make([]string, 0, len(rawLines))

	for _, rawLine := range rawLines {
		var b strings.Builder
		b.Grow(len(rawLine) + 64)
		hasANSI := base != ""
		if base != "" && (len(activeSGRs) > 0 || !nextSGRSetsBg(rawLine)) {
			b.WriteString(base)
		}
		for _, sgr := range activeSGRs {
			b.WriteString(sgr)
			hasANSI = true
		}
		atBaseReset := len(activeSGRs) == 0
		visWidth := 0
		lineFull := false

		i := 0
		n := len(rawLine)
		for i < n {
			c := rawLine[i]
			if c == '\x1b' {
				if i+1 < n && rawLine[i+1] == '[' {
					end := i + 2
					for end < n && (rawLine[end] < 0x40 || rawLine[end] > 0x7E) {
						end++
					}
					if end < n {
						seq := rawLine[i : end+1]
						params := rawLine[i+2 : end]
						termByte := rawLine[end]
						if termByte == 'm' {
							hasANSI = true
							if params == "" || params == "0" || params == "00" {
								activeSGRs = activeSGRs[:0]
								rest := rawLine[end+1:]
								moreText := !lineFull && hasRemainingPrintable(rest)
								if !atBaseReset && (moreText || (width > 0 && visWidth < width)) {
									b.WriteString("\x1b[0m")
									if base != "" && !nextSGRSetsBg(rest) {
										b.WriteString(base)
									}
									atBaseReset = true
								}
							} else if strings.HasPrefix(params, "0;") {
								restSeq := "\x1b[" + params[2:] + "m"
								activeSGRs = append(activeSGRs[:0], restSeq)
								if !lineFull {
									b.WriteString("\x1b[0m")
									if base != "" && !nextSGRSetsBg(restSeq) {
										b.WriteString(base)
									}
									b.WriteString(restSeq)
									atBaseReset = false
								}
							} else {
								activeSGRs = append(activeSGRs, seq)
								if !lineFull {
									b.WriteString(seq)
									atBaseReset = false
								}
							}
						} else if !lineFull {
							b.WriteString(seq)
						}
						i = end + 1
						continue
					}
				} else if i+1 < n && rawLine[i+1] == ']' {
					end := i + 2
					for end < n {
						if rawLine[end] == '\x07' {
							end++
							break
						}
						if rawLine[end] == '\x1b' && end+1 < n && rawLine[end+1] == '\\' {
							end += 2
							break
						}
						end++
					}
					if !lineFull {
						b.WriteString(rawLine[i:end])
					}
					i = end
					continue
				}
			}

			if c >= 0x20 && c < 0x7f {
				if lineFull || (width > 0 && visWidth+1 > width) {
					lineFull = true
					i++
					continue
				}
				b.WriteByte(c)
				visWidth++
				i++
				continue
			}

			r, size := utf8.DecodeRuneInString(rawLine[i:])
			if r == utf8.RuneError && size == 1 {
				i++
				continue
			}
			rw := fastRuneWidth(r)
			if lineFull || (width > 0 && visWidth+rw > width) {
				lineFull = true
				i += size
				continue
			}
			b.WriteRune(r)
			visWidth += rw
			i += size
		}

		if width > visWidth {
			padCount := width - visWidth
			if !atBaseReset && base != "" {
				b.WriteString("\x1b[0m")
				b.WriteString(base)
				atBaseReset = true
			}
			b.WriteString(strings.Repeat(" ", padCount))
		}

		if hasANSI {
			b.WriteString("\x1b[0m")
		}
		outLines = append(outLines, b.String())
	}

	return strings.Join(outLines, "\n")
}

// PaintBarLine forces line onto a single visual row of exact width (truncating if wider)
// and paints its full surface with bg and defaultFg.
func PaintBarLine(line string, width int, bg, defaultFg lipgloss.Color) string {
	if isAlreadyPaintedLine(line, width) {
		return line
	}
	clean := line
	if strings.IndexByte(clean, '\r') >= 0 {
		clean = strings.ReplaceAll(clean, "\r", "")
	}
	if strings.IndexByte(clean, '\n') >= 0 {
		clean = strings.ReplaceAll(clean, "\n", " ")
	}
	if strings.IndexByte(clean, '\t') >= 0 {
		clean = strings.ReplaceAll(clean, "\t", "    ")
	}
	if width > 0 && fastPrintableWidth(clean) > width {
		clean = truncate.String(clean, uint(width))
	}
	return PaintLine(clean, width, bg, defaultFg)
}

// PaintSurface splits block into lines, paints each line to width with bg and defaultFg,
// and pads or clamps the vertical line count to height (if height > 0).
func PaintSurface(block string, width, height int, bg, defaultFg lipgloss.Color) string {
	var rawLines []string
	if block != "" {
		rawLines = strings.Split(block, "\n")
	}

	paintedLines := make([]string, 0, max(len(rawLines), height))
	for _, l := range rawLines {
		if isAlreadyPaintedLine(l, width) {
			paintedLines = append(paintedLines, l)
			continue
		}
		res := PaintLine(l, width, bg, defaultFg)
		if strings.IndexByte(res, '\n') < 0 {
			paintedLines = append(paintedLines, res)
		} else {
			paintedLines = append(paintedLines, strings.Split(res, "\n")...)
		}
	}

	if height > 0 {
		if len(paintedLines) < height {
			blankLine := PaintLine("", width, bg, defaultFg)
			for len(paintedLines) < height {
				paintedLines = append(paintedLines, blankLine)
			}
		} else if len(paintedLines) > height {
			paintedLines = paintedLines[:height]
		}
	}

	return strings.Join(paintedLines, "\n")
}
