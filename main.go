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
	"github.com/muesli/termenv"
)

// Version is the current release version of srl-timeline.
const Version = "0.0.3"

func init() {
	if os.Getenv("NO_COLOR") == "" {
		lipgloss.SetColorProfile(termenv.TrueColor)
	}
}

func main() {
	if len(os.Args) >= 2 && (os.Args[1] == "-v" || os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Printf("srl-timeline version %s\n", Version)
		return
	}

	if len(os.Args) < 2 || strings.HasPrefix(os.Args[1], "-") {
		// Launch interactive Bubble Tea TUI
		initialFilter := ""
		for i, arg := range os.Args {
			if (arg == "-f" || arg == "--filter") && i+1 < len(os.Args) {
				initialFilter = os.Args[i+1]
			}
		}
		if err := tui.RunTUI(initialFilter); err != nil {
			fmt.Fprintf(os.Stderr, "Error running timeline TUI: %v\n", err)
			os.Exit(1)
		}
		return
	}

	subcommand := os.Args[1]
	backend := gitbackend.NewGitBackend("")
	client := srlclient.NewSRLClient()

	if subcommand != "daemon" {
		if !backend.HasCommits() {
			if cfg, err := client.GetRunningConfig("/"); err == nil && len(cfg) > 0 {
				_, _, _ = backend.EnsureBaseline(cfg)
			}
		}
	}

	switch subcommand {
	case "version":
		fmt.Printf("srl-timeline version %s\n", Version)
		return
	case "tui":
		initialFilter := ""
		for i, arg := range os.Args {
			if (arg == "-f" || arg == "--filter") && i+1 < len(os.Args) {
				initialFilter = os.Args[i+1]
			}
		}
		if err := tui.RunTUI(initialFilter); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "daemon":
		action := "status"
		if len(os.Args) >= 3 {
			action = os.Args[2]
		}
		if err := daemon.ManageDaemonProcess(action); err != nil {
			fmt.Fprintf(os.Stderr, "Daemon error: %v\n", err)
			os.Exit(1)
		}

	case "log":
		logFlags := flag.NewFlagSet("log", flag.ExitOnError)
		limit := logFlags.Int("limit", 20, "Maximum number of commits")
		filterQuery := logFlags.String("filter", "", "Filter commits by path")
		_ = logFlags.Parse(reorderFlags(os.Args[2:]))

		commits := backend.GetTimeline(*limit, *filterQuery)
		header := fmt.Sprintf("%-10s %-12s %-15s %-12s %s", "REV", "AUTHOR", "WHEN", "CHANGES", "MESSAGE")
		fmt.Println(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#8b949e")).Render(header))
		fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("#30363d")).Render(strings.Repeat("─", 75)))
		for _, c := range commits {
			revStr := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58a6ff")).Render(fmt.Sprintf("%-10s", c.CommitID))
			authStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#bc8cff"))
			if c.Author == "admin" || c.Author == "root" {
				authStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#3fb950"))
			}
			authStr := authStyle.Render(fmt.Sprintf("%-12s", c.Author))
			whenStr := lipgloss.NewStyle().Foreground(lipgloss.Color("#7d8590")).Render(fmt.Sprintf("%-15s", c.RelativeTime()))

			statStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7d8590"))
			if strings.HasPrefix(c.DiffStat, "+") {
				statStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#3fb950"))
			} else if strings.HasPrefix(c.DiffStat, "~") {
				statStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#d29922"))
			}
			statStr := statStyle.Render(fmt.Sprintf("%-12s", c.DiffStat))
			msgStr := lipgloss.NewStyle().Foreground(lipgloss.Color("#e6edf3")).Render(c.Message)

			fmt.Printf("%s %s %s %s %s\n", revStr, authStr, whenStr, statStr, msgStr)
		}

	case "diff":
		diffFlags := flag.NewFlagSet("diff", flag.ExitOnError)
		diffFmt := diffFlags.String("format", "unified", "Diff format: unified, cli, path")
		filterQuery := diffFlags.String("filter", "", "Filter diff by path")
		_ = diffFlags.Parse(reorderFlags(os.Args[2:]))

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

		switch *diffFmt {
		case "cli":
			for _, l := range res.CLIDiffLines {
				if strings.HasPrefix(l, "+") {
					fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950")).Render(l))
				} else if strings.HasPrefix(l, "-") {
					fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("#f85149")).Render(l))
				} else {
					fmt.Println(l)
				}
			}
		case "path":
			for _, c := range res.Changes {
				var badge string
				switch c.DiffType {
				case "ADDED":
					badge = lipgloss.NewStyle().Background(lipgloss.Color("#238636")).Foreground(lipgloss.Color("#ffffff")).Bold(true).Render(" ADD ")
				case "MODIFIED":
					badge = lipgloss.NewStyle().Background(lipgloss.Color("#d29922")).Foreground(lipgloss.Color("#ffffff")).Bold(true).Render(" MOD ")
				case "DELETED":
					badge = lipgloss.NewStyle().Background(lipgloss.Color("#da3633")).Foreground(lipgloss.Color("#ffffff")).Bold(true).Render(" DEL ")
				}
				fmt.Printf("%s %s\n", badge, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e6edf3")).Render(c.Path))
			}
		default:
			for _, l := range res.UnifiedDiffLines {
				if strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "---") {
					fmt.Println(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#8b949e")).Render(l))
				} else if strings.HasPrefix(l, "@@") {
					fmt.Println(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58a6ff")).Render(l))
				} else if strings.HasPrefix(l, "+") {
					fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950")).Render(l))
				} else if strings.HasPrefix(l, "-") {
					fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("#f85149")).Render(l))
				} else {
					fmt.Println(l)
				}
			}
		}

	case "show":
		showFlags := flag.NewFlagSet("show", flag.ExitOnError)
		showFmt := showFlags.String("format", "cli", "Output format: cli, json")
		filterQuery := showFlags.String("filter", "", "Filter by path")
		_ = showFlags.Parse(reorderFlags(os.Args[2:]))

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

		if *showFmt == "json" {
			out, _ := normalizer.CanonicalJSONString(cfg, 2)
			fmt.Println(out)
		} else {
			out := normalizer.FlatCLIString(cfg)
			fmt.Print(out)
		}

	case "restore", "cherry-pick":
		restoreFlags := flag.NewFlagSet("restore", flag.ExitOnError)
		subtreePath := restoreFlags.String("path", "", "Subtree path for cherry-pick restoration (e.g. /interface[name=ethernet-1/1])")
		yes := restoreFlags.Bool("yes", false, "Confirm restore without prompt")
		_ = restoreFlags.Parse(reorderFlags(os.Args[2:]))

		if len(restoreFlags.Args()) == 0 {
			fmt.Println("Usage: timeline restore <commit-sha> [--path <subtree>] [--yes]")
			os.Exit(1)
		}
		targetSHA := restoreFlags.Args()[0]

		if *subtreePath != "" {
			rawPaths := strings.Split(*subtreePath, ",")
			var cleanPaths []string
			for _, p := range rawPaths {
				p = strings.TrimSpace(p)
				if p != "" {
					cleanPaths = append(cleanPaths, p)
				}
			}

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
		_ = exportFlags.Parse(reorderFlags(os.Args[2:]))

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

		if *exportFmt == "cli" {
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
		_ = blameFlags.Parse(reorderFlags(os.Args[2:]))

		b := blame.NewBlameEngine(backend)
		entries := b.GetBlameLines("cli", *filterQuery)
		for _, e := range entries {
			fmt.Printf("%-8s (%-10s %s) %s\n", e.ShortSHA(), e.Author, e.Timestamp.Format("2006-01-02"), e.Content)
		}

	case "remote":
		if len(os.Args) < 3 {
			fmt.Println("Usage: timeline remote [show|key|set <url> [--branch <name>] [--auto-push]|push]")
			return
		}
		action := os.Args[2]
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
			if len(os.Args) < 4 {
				fmt.Println("Usage: timeline remote set <url> [--branch <name>] [--auto-push]")
				os.Exit(1)
			}
			url := os.Args[3]
			branch := "main"
			autoPush := false
			for i := 4; i < len(os.Args); i++ {
				if os.Args[i] == "--branch" && i+1 < len(os.Args) {
					branch = os.Args[i+1]
					i++
				} else if os.Args[i] == "--auto-push" {
					autoPush = true
				}
			}
			cfg := backend.LoadRemoteConfig()
			cfg.URL = url
			cfg.Branch = branch
			cfg.AutoPush = autoPush
			_ = backend.SaveRemoteConfig(cfg)
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

func reorderFlags(args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			if !strings.Contains(args[i], "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flags = append(flags, args[i+1])
				i++
			}
		} else {
			pos = append(pos, args[i])
		}
	}
	return append(flags, pos...)
}
