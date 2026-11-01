package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"timeline/pkg/blame"
	"timeline/pkg/daemon"
	"timeline/pkg/differ"
	"timeline/pkg/exporter"
	"timeline/pkg/filter"
	"timeline/pkg/gitbackend"
	"timeline/pkg/normalizer"
	"timeline/pkg/restorer"
	"timeline/pkg/srlclient"
	"timeline/pkg/tui"
	"timeline/pkg/tui/modals"

	"github.com/charmbracelet/lipgloss"
)

// Version is the current release version of srl-timeline.
const Version = "0.0.4"

func init() {
	configureEnvironmentAppearance()
}

func configureEnvironmentAppearance() {
	if os.Getenv("NO_COLOR") != "" {
		_ = tui.SetColorProfile("ascii")
	} else if envProfile := strings.TrimSpace(os.Getenv("TIMELINE_COLOR_PROFILE")); envProfile != "" {
		_ = tui.SetColorProfile(envProfile)
	} else {
		_ = tui.SetColorProfile("truecolor")
	}

	if envTheme := strings.TrimSpace(os.Getenv("TIMELINE_THEME")); envTheme != "" {
		if _, ok := tui.SetTheme(envTheme); !ok {
			_, _ = tui.SetTheme("dark")
		}
	} else {
		_, _ = tui.SetTheme("dark")
	}
}

func themeNamesList() string {
	themes := tui.AvailableThemes()
	names := make([]string, 0, len(themes))
	for _, t := range themes {
		names = append(names, t.Name)
	}
	return strings.Join(names, ", ")
}

func printUsage() {
	fmt.Printf(`srl-timeline v%s — Interactive Configuration History & Rollback for Nokia SR Linux

Usage:
  timeline [--filter <path>] [--theme <name>] [--color-profile <profile>]
  timeline tui [--filter <path>] [--theme <name>] [--color-profile <profile>]
  timeline log [--limit <n>] [--filter <path>] [--theme <name>]
  timeline diff [<rev1> [<rev2>]] [--format unified|json|cli|path] [--filter <path>] [--theme <name>]
  timeline show [<rev>|running] [--format cli|json] [--filter <path>]
  timeline restore <commit-sha> [--path <subtree>] [--yes]
  timeline cherry-pick <commit-sha> --path <subtree> [--yes]
  timeline export [<rev>] [--format json|cli] [--output <file>] [--save-startup]
  timeline blame [<rev>] [--filter <path|author>]
  timeline remote [show|key|set <url> [--branch <name>] [--auto-push]|push]
  timeline daemon [start|stop|status|bootstrap]
  timeline themes
  timeline version

Global Options & Environment:
  --theme <name>            Color theme (env: TIMELINE_THEME). Press [T] in TUI to cycle live.
                            Built-in themes: %s
  --color-profile <profile> Terminal color profile: truecolor, 256, ansi, ascii (env: TIMELINE_COLOR_PROFILE)
`, Version, themeNamesList())
}

func printThemes() {
	current := tui.CurrentTheme().Name
	fmt.Println("Available srl-timeline themes:")
	for _, t := range tui.AvailableThemes() {
		marker := "  "
		if t.Name == current {
			marker = "* "
		}
		mode := "dark"
		if !t.IsDark {
			mode = "light"
		}
		fmt.Printf("%s%-18s %-20s (%s)\n", marker, t.Name, t.DisplayName, mode)
	}
}

func main() {
	configureEnvironmentAppearance()
	args := applyGlobalOptions(os.Args[1:])

	if len(args) >= 1 {
		switch args[0] {
		case "-v", "--version", "version":
			fmt.Printf("srl-timeline version %s\n", Version)
			return
		case "-h", "--help", "help":
			printUsage()
			return
		case "themes":
			printThemes()
			return
		}
	}

	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		// Launch interactive Bubble Tea TUI
		initialFilter := extractInitialFilter(args)
		if err := tui.RunTUI(initialFilter); err != nil {
			fmt.Fprintf(os.Stderr, "Error running timeline TUI: %v\n", err)
			os.Exit(1)
		}
		return
	}

	subcommand := args[0]
	subArgs := args[1:]
	backend := gitbackend.NewGitBackend("")
	client := srlclient.NewSRLClient()

	if subcommand != "daemon" {
		if !backend.HasCommits() {
			if cfg, err := client.GetRunningConfig("/"); err == nil && len(cfg) > 0 {
				if unlock, ok := daemon.TryAcquireRepoRecordLock(backend.RepoDir); ok {
					_, _, _ = backend.EnsureBaseline(cfg)
					unlock()
				}
			}
		}
	}

	switch subcommand {
	case "version":
		fmt.Printf("srl-timeline version %s\n", Version)
		return
	case "themes":
		printThemes()
		return
	case "tui":
		initialFilter := extractInitialFilter(subArgs)
		if err := tui.RunTUI(initialFilter); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "daemon":
		action := "status"
		if len(subArgs) >= 1 {
			action = subArgs[0]
		}
		if err := daemon.ManageDaemonProcess(action); err != nil {
			fmt.Fprintf(os.Stderr, "Daemon error: %v\n", err)
			os.Exit(1)
		}

	case "log":
		logFlags := flag.NewFlagSet("log", flag.ExitOnError)
		limit := logFlags.Int("limit", 20, "Maximum number of commits")
		filterQuery := logFlags.String("filter", "", "Filter commits by path")
		_ = logFlags.Parse(reorderFlags(subArgs))

		commits := backend.GetTimeline(*limit, *filterQuery)
		header := fmt.Sprintf("%-10s %-12s %-15s %-12s %s", "REV", "AUTHOR", "WHEN", "CHANGES", "MESSAGE")
		fmt.Println(lipgloss.NewStyle().Bold(true).Foreground(tui.ColorMuted).Render(header))
		fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorBorder).Render(strings.Repeat("─", 75)))
		for _, c := range commits {
			revStr := lipgloss.NewStyle().Bold(true).Foreground(tui.ColorPrimary).Render(fmt.Sprintf("%-10s", c.CommitID))
			authStyle := lipgloss.NewStyle().Foreground(tui.ColorPurple)
			if c.Author == "admin" || c.Author == "root" {
				authStyle = lipgloss.NewStyle().Bold(true).Foreground(tui.ColorSuccess)
			}
			authStr := authStyle.Render(fmt.Sprintf("%-12s", c.Author))
			whenStr := lipgloss.NewStyle().Foreground(tui.ColorTextDim).Render(fmt.Sprintf("%-15s", c.RelativeTime()))

			statStyle := lipgloss.NewStyle().Foreground(tui.ColorTextDim)
			if strings.HasPrefix(c.DiffStat, "+") {
				statStyle = lipgloss.NewStyle().Bold(true).Foreground(tui.ColorSuccess)
			} else if strings.HasPrefix(c.DiffStat, "~") {
				statStyle = lipgloss.NewStyle().Foreground(tui.ColorWarning)
			} else if strings.HasPrefix(c.DiffStat, "-") {
				statStyle = lipgloss.NewStyle().Bold(true).Foreground(tui.ColorDanger)
			}
			statStr := statStyle.Render(fmt.Sprintf("%-12s", c.DiffStat))
			msgStr := lipgloss.NewStyle().Foreground(tui.ColorText).Render(c.Message)

			fmt.Printf("%s %s %s %s %s\n", revStr, authStr, whenStr, statStr, msgStr)
		}

	case "diff":
		diffFlags := flag.NewFlagSet("diff", flag.ExitOnError)
		diffFmt := diffFlags.String("format", "unified", "Diff format: unified, json, cli, path")
		filterQuery := diffFlags.String("filter", "", "Filter diff by path")
		_ = diffFlags.Parse(reorderFlags(subArgs))

		revs := diffFlags.Args()
		rev1 := "HEAD~1"
		rev2 := "HEAD"
		if len(revs) >= 1 {
			rev1 = revs[0]
		}
		if len(revs) >= 2 {
			rev2 = revs[1]
		}

		cfg1 := backend.GetConfigAtCommit(rev1)
		cfg2 := backend.GetConfigAtCommit(rev2)
		res := differ.SemanticDiff(cfg1, cfg2, *filterQuery)

		switch strings.ToLower(*diffFmt) {
		case "cli":
			for _, l := range res.CLIDiffLines {
				if strings.HasPrefix(l, "+") {
					fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Render(l))
				} else if strings.HasPrefix(l, "-") {
					fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorDanger).Render(l))
				} else {
					fmt.Println(l)
				}
			}
		case "path":
			for _, c := range res.Changes {
				var badge string
				switch c.DiffType {
				case "ADDED":
					badge = lipgloss.NewStyle().Background(tui.ColorSuccessBg).Foreground(tui.ColorOnAccent).Bold(true).Render(" ADD ")
				case "MODIFIED":
					badge = lipgloss.NewStyle().Background(tui.ColorWarning).Foreground(tui.ColorOnAccent).Bold(true).Render(" MOD ")
				case "DELETED":
					badge = lipgloss.NewStyle().Background(tui.ColorDangerBg).Foreground(tui.ColorOnAccent).Bold(true).Render(" DEL ")
				}
				fmt.Printf("%s %s\n", badge, lipgloss.NewStyle().Bold(true).Foreground(tui.ColorText).Render(c.Path))
			}
		case "unified", "json":
			fallthrough
		default:
			for _, l := range res.UnifiedDiffLines {
				if strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "---") {
					fmt.Println(lipgloss.NewStyle().Bold(true).Foreground(tui.ColorMuted).Render(l))
				} else if strings.HasPrefix(l, "@@") {
					fmt.Println(lipgloss.NewStyle().Bold(true).Foreground(tui.ColorPrimary).Render(l))
				} else if strings.HasPrefix(l, "+") {
					fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorSuccess).Render(l))
				} else if strings.HasPrefix(l, "-") {
					fmt.Println(lipgloss.NewStyle().Foreground(tui.ColorDanger).Render(l))
				} else {
					fmt.Println(l)
				}
			}
		}

	case "show":
		showFlags := flag.NewFlagSet("show", flag.ExitOnError)
		showFmt := showFlags.String("format", "cli", "Output format: cli, json")
		filterQuery := showFlags.String("filter", "", "Filter by path")
		_ = showFlags.Parse(reorderFlags(subArgs))

		rev := "HEAD"
		if len(showFlags.Args()) >= 1 {
			rev = showFlags.Args()[0]
		}

		var cfg map[string]interface{}
		if rev == "running" || rev == "live" {
			cfg, _ = client.GetRunningConfig("/")
		} else {
			cfg = backend.GetConfigAtCommit(rev)
		}

		if *filterQuery != "" {
			cfg = filter.FilterConfigSubtree(cfg, *filterQuery)
		}

		if strings.EqualFold(*showFmt, "json") {
			out, _ := normalizer.CanonicalJSONString(cfg, 2)
			fmt.Println(out)
		} else {
			out := normalizer.FlatCLIString(cfg)
			fmt.Print(out)
		}

	case "restore", "cherry-pick":
		restoreFlags := flag.NewFlagSet(subcommand, flag.ExitOnError)
		subtreePath := restoreFlags.String("path", "", "Subtree path for cherry-pick restoration (e.g. /interface[name=ethernet-1/1])")
		yes := restoreFlags.Bool("yes", false, "Confirm restore without prompt")
		_ = restoreFlags.Parse(reorderFlags(subArgs, "yes"))

		if len(restoreFlags.Args()) == 0 {
			if subcommand == "cherry-pick" {
				fmt.Println("Usage: timeline cherry-pick <commit-sha> --path <subtree> [--yes]")
			} else {
				fmt.Println("Usage: timeline restore <commit-sha> [--path <subtree>] [--yes]")
			}
			os.Exit(1)
		}
		targetSHA := restoreFlags.Args()[0]

		var cleanPaths []string
		if *subtreePath != "" {
			rawPaths := strings.Split(*subtreePath, ",")
			for _, p := range rawPaths {
				p = strings.TrimSpace(p)
				if p != "" {
					cleanPaths = append(cleanPaths, p)
				}
			}
		}
		for _, posArg := range restoreFlags.Args()[1:] {
			for _, p := range strings.Split(posArg, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					cleanPaths = append(cleanPaths, p)
				}
			}
		}

		if subcommand == "cherry-pick" && len(cleanPaths) == 0 {
			fmt.Fprintln(os.Stderr, "Error: timeline cherry-pick requires --path <subtree>")
			os.Exit(1)
		}

		if len(cleanPaths) > 0 {
			if !*yes {
				fmt.Printf("Are you sure you want to cherry-pick restore %v from %s? (y/N): ", cleanPaths, targetSHA)
				var resp string
				_, _ = fmt.Scanln(&resp)
				if strings.ToLower(strings.TrimSpace(resp)) != "y" {
					fmt.Println("Restore cancelled.")
					return
				}
			}

			r := restorer.NewConfigRestorer(client, backend)
			ok, msg, err := r.CherryPickRestore(targetSHA, cleanPaths...)
			if err != nil || !ok {
				fmt.Fprintf(os.Stderr, "Cherry-pick restore failed: %v (%s)\n", err, msg)
				os.Exit(1)
			}
			fmt.Println(msg)
			return
		}

		if !*yes {
			fmt.Printf("Are you sure you want to restore full configuration to %s? (y/N): ", targetSHA)
			var resp string
			_, _ = fmt.Scanln(&resp)
			if strings.ToLower(strings.TrimSpace(resp)) != "y" {
				fmt.Println("Restore cancelled.")
				return
			}
		}

		r := restorer.NewConfigRestorer(client, backend)
		ok, msg, err := r.RestoreFullConfig(targetSHA)
		if err != nil || !ok {
			fmt.Fprintf(os.Stderr, "Restore failed: %v (%s)\n", err, msg)
			os.Exit(1)
		}
		fmt.Println(msg)

	case "export":
		exportFlags := flag.NewFlagSet("export", flag.ExitOnError)
		exportFmt := exportFlags.String("format", "json", "Format: json, cli")
		outPath := exportFlags.String("output", "", "Output file path")
		saveStartup := exportFlags.Bool("save-startup", false, "Save directly to /etc/opt/srlinux/config.json")
		_ = exportFlags.Parse(reorderFlags(subArgs, "save-startup"))

		rev := "HEAD"
		if len(exportFlags.Args()) >= 1 {
			rev = exportFlags.Args()[0]
		}

		e := exporter.NewConfigExporter(backend, client)
		if *saveStartup {
			ok, msg, err := e.SaveAsDeviceStartupConfig(rev)
			if err != nil || !ok {
				fmt.Fprintf(os.Stderr, "Export failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Println(msg)
			return
		}

		if strings.EqualFold(*exportFmt, "cli") {
			res, err := e.ExportAsFlatCLI(rev, *outPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Export failed: %v\n", err)
				os.Exit(1)
			}
			if *outPath != "" {
				fmt.Printf("Exported CLI to: %s\n", *outPath)
			} else {
				fmt.Print(res)
			}
		} else {
			res, err := e.ExportAsStartupJSON(rev, *outPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Export failed: %v\n", err)
				os.Exit(1)
			}
			if *outPath != "" {
				fmt.Printf("Exported JSON to: %s\n", *outPath)
			} else {
				fmt.Println(res)
			}
		}

	case "blame":
		blameFlags := flag.NewFlagSet("blame", flag.ExitOnError)
		filterQuery := blameFlags.String("filter", "", "Filter blame by path or author")
		_ = blameFlags.Parse(reorderFlags(subArgs))

		rev := "HEAD"
		if len(blameFlags.Args()) >= 1 {
			rev = blameFlags.Args()[0]
		}

		b := blame.NewBlameEngine(backend)
		entries := b.GetBlameLinesAtCommit("cli", rev, *filterQuery)
		for _, e := range entries {
			fmt.Printf("%-8s (%-10s %s) %s\n", e.ShortSHA(), e.Author, e.Timestamp.Format("2006-01-02"), e.Content)
		}

	case "remote":
		if len(subArgs) < 1 {
			fmt.Println("Usage: timeline remote [show|key|set <url> [--branch <name>] [--auto-push]|push]")
			return
		}
		action := subArgs[0]
		switch action {
		case "show":
			cfg := backend.LoadRemoteConfig()
			syncSt := backend.CheckRemoteSyncStatus()
			pubKey := backend.GetPublicSSHKey()
			fmt.Printf("Remote URL:    %s\n", cfg.URL)
			fmt.Printf("Branch:        %s\n", cfg.Branch)
			autoPushStr := "Disabled"
			if cfg.AutoPush {
				autoPushStr = "Enabled"
			}
			fmt.Printf("Auto-push:     %s\n", autoPushStr)
			fmt.Printf("Sync Status:   %s\n", syncSt)
			if pubKey != "" {
				fmt.Printf("\nPublic SSH Key (Add to GitHub Deploy Keys with write access):\n%s\n", pubKey)
			}

		case "key":
			pubKey := backend.GetPublicSSHKey()
			if pubKey == "" {
				fmt.Println("No SSH key available.")
				os.Exit(1)
			}
			modals.CopyToClipboard(pubKey)
			fmt.Println(pubKey)

		case "set":
			if len(subArgs) < 2 {
				fmt.Println("Usage: timeline remote set <url> [--branch <name>] [--auto-push]")
				os.Exit(1)
			}
			url := subArgs[1]
			branch := "main"
			autoPush := false
			for i := 2; i < len(subArgs); i++ {
				if subArgs[i] == "--branch" && i+1 < len(subArgs) {
					branch = subArgs[i+1]
					i++
				} else if subArgs[i] == "--auto-push" {
					autoPush = true
				}
			}
			cfg := backend.LoadRemoteConfig()
			cfg.URL = url
			cfg.Branch = branch
			cfg.AutoPush = autoPush
			if err := backend.SaveRemoteConfig(cfg); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to save remote configuration: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Saved remote repository: %s (branch: %s)\n", url, branch)

		case "push":
			err := backend.PushRemote()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Push failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Successfully pushed to remote.")
		}

	default:
		fmt.Printf("Unknown command '%s'. Run 'timeline' or 'timeline --help'.\n", subcommand)
	}
}

func reorderFlags(args []string, boolFlagNames ...string) []string {
	boolSet := map[string]bool{
		"-yes": true, "--yes": true,
		"-y": true, "--y": true,
		"-startup": true, "--startup": true,
		"-save-startup": true, "--save-startup": true,
		"-auto-push": true, "--auto-push": true,
		"-help": true, "--help": true,
		"-h": true, "--h": true,
	}
	for _, bf := range boolFlagNames {
		boolSet["-"+bf] = true
		boolSet["--"+bf] = true
	}

	var flags, pos []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if !boolSet[arg] && !strings.Contains(arg, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flags = append(flags, args[i+1])
				i++
			}
		} else {
			pos = append(pos, arg)
		}
	}
	return append(flags, pos...)
}

func extractGlobalOptions(args []string) (remaining []string, themeName string, colorProfile string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if (arg == "--theme" || arg == "-theme") && i+1 < len(args) {
			themeName = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--theme=") {
			themeName = strings.TrimPrefix(arg, "--theme=")
		} else if strings.HasPrefix(arg, "-theme=") {
			themeName = strings.TrimPrefix(arg, "-theme=")
		} else if (arg == "--color-profile" || arg == "-color-profile") && i+1 < len(args) {
			colorProfile = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--color-profile=") {
			colorProfile = strings.TrimPrefix(arg, "--color-profile=")
		} else if strings.HasPrefix(arg, "-color-profile=") {
			colorProfile = strings.TrimPrefix(arg, "-color-profile=")
		} else {
			remaining = append(remaining, arg)
		}
	}
	return remaining, themeName, colorProfile
}

func applyGlobalOptions(args []string) []string {
	remaining, themeName, colorProfile := extractGlobalOptions(args)
	if colorProfile != "" {
		if err := tui.SetColorProfile(colorProfile); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		}
	}
	if themeName != "" {
		if strings.EqualFold(themeName, "list") {
			printThemes()
			os.Exit(0)
		}
		if _, ok := tui.SetTheme(themeName); !ok {
			fmt.Fprintf(os.Stderr, "Warning: unknown theme %q (valid: %s); falling back to 'dark'\n",
				themeName, themeNamesList())
			_, _ = tui.SetTheme("dark")
		}
	}
	return remaining
}

func extractInitialFilter(args []string) string {
	var initialFilter string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if (arg == "-f" || arg == "--filter") && i+1 < len(args) {
			initialFilter = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--filter=") {
			initialFilter = strings.TrimPrefix(arg, "--filter=")
		} else if strings.HasPrefix(arg, "-f=") {
			initialFilter = strings.TrimPrefix(arg, "-f=")
		}
	}
	return initialFilter
}

