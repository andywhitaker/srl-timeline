# SR Linux Timeline (`timeline`)

A configuration timeline and version history tool for Nokia SR Linux using Git.

`timeline` automatically tracks configuration changes on Nokia SR Linux switches into an on-box Git repository. It provides an interactive Terminal User Interface (TUI) as the primary way to interact with the switch's history, along with command-line tools to view historical commits, inspect semantic diffs, trace line attribution, restore configurations, and synchronize with remote Git repositories.

## Features

- **Commit History & Visual Timeline**: Automatically records configuration commits with author identity, timestamps, diff statistics, and commit comments.
- **Pure-Go Native Git Engine**: Writes and reads standard Git loose objects (`blob`, `tree`, `commit`) and refs directly in Go under `/etc/opt/srlinux/timeline`. Does **not** require an external `git` binary on the switch for commit recording, history, diffing, blame, restore, or export (external `git` is only optional for SSH remote push).
- **Semantic Configuration Diffs**: Compares revisions structurally in memory. Supports Unified Diff (`unified` / `json`), Path Changes (`path`), and flat CLI (`cli`: `set` / `delete`) formats. Includes a **Live Compare Mode** to diff any past snapshot against the switch's live running configuration.
- **Scoped Path Filtering**: Filter the timeline, diffs, and full configuration to specific subtrees or interfaces (e.g., `interface ethernet-1/1`, `bgp`, `acl`).
- **Configuration Rollback & Cherry-Pick**: Roll back the switch to a past revision or interactively cherry-pick and apply specific configuration subtrees.
- **Configuration Blame**: Line-by-line attribution showing who modified each line or block of configuration and when.
- **11 Built-in Color Themes & Full-Bleed TUI**: Cycle live through 11 curated dark and light themes (<kbd>T</kbd> in the TUI) or select via `--theme` / `TIMELINE_THEME` and `--color-profile` / `TIMELINE_COLOR_PROFILE`.
- **Remote Git Synchronization**: Push configuration history to external Git repositories (GitHub, GitLab, etc.) with automated background push on commit.
- **Configuration Export**: Export any historical commit as SR Linux startup JSON (`config.json`) or flat CLI scripts.

![Timeline Image](images/timeline-image.png)

---

## 1. Getting or Building `srl-timeline`

You can either download a pre-built Debian package / binary or build from source:

### Option A: Download Pre-built Package or Binary (Recommended)
Download the latest Debian package (`srl-timeline_0.0.4_amd64.deb`) or static Linux binary (`timeline`) from the [GitHub Releases](https://github.com/andywhitaker/srl-timeline/releases/latest) page on your local workstation.

### Option B: Build from Source with Go
`timeline` is written in pure Go and compiles to a single static binary with no external runtime dependencies:

```bash
# Clone the repository
git clone https://github.com/andywhitaker/srl-timeline.git
cd srl-timeline

# Build the Debian package (dist/srl-timeline_0.0.4_amd64.deb) and static binary (bin/timeline)
make deb

# Or compile only the static binary
CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/timeline .
```

---

## 2. Install on the SR Linux Switch

### Method A: Debian Package Installation (Recommended)
The `.deb` package installs `/usr/local/bin/timeline` and `/etc/opt/srlinux/appmgr/timeline_config.yml`, sets up multi-user repository permissions under `/etc/opt/srlinux/timeline`, enables the on-box JSON-RPC Unix domain socket (`/opt/srlinux/var/run/sr_json_rpc`), and automatically reloads `app_mgr`:

```bash
# 1. Copy the package from your workstation to the switch
scp dist/srl-timeline_0.0.4_amd64.deb admin@<switch-ip>:/tmp/

# 2. Install on the switch (from Linux bash or via 'bash' in sr_cli)
sudo dpkg -i /tmp/srl-timeline_0.0.4_amd64.deb
```

### Method B: Manual Binary & App Manager Installation
If you are installing the standalone binary manually, copy both `timeline` and `timeline_config.yml` to the switch so `app_mgr` runs the background recorder daemon:

#### Step 1: SCP to the switch
From your local workstation, copy the binary and `app_mgr` configuration to `/tmp` on the SR Linux switch:
```bash
scp bin/timeline timeline_config.yml admin@<switch-ip>:/tmp/
```

#### Step 2: Log into the switch, install, and bootstrap `timeline`
Log into the switch, move the binary to `/usr/local/bin`, install `timeline_config.yml` into `/etc/opt/srlinux/appmgr/`, and bootstrap the daemon:

- **From the SR Linux CLI**:
  ```text
  --{ running }--[  ]--
  A:srl-timeline# bash sudo mv /tmp/timeline /usr/local/bin/timeline
  A:srl-timeline# bash sudo chmod +x /usr/local/bin/timeline
  A:srl-timeline# bash sudo mv /tmp/timeline_config.yml /etc/opt/srlinux/appmgr/timeline_config.yml
  A:srl-timeline# bash sudo timeline daemon bootstrap
  ```

- **From the Linux Bash Shell** (if logged into bash directly):
  ```bash
  sudo mv /tmp/timeline /usr/local/bin/timeline
  sudo chmod +x /usr/local/bin/timeline
  sudo mv /tmp/timeline_config.yml /etc/opt/srlinux/appmgr/timeline_config.yml
  sudo timeline daemon bootstrap
  ```

---

## 3. Run Timeline

The primary way to interact with `timeline` is through the visual **Terminal User Interface (TUI)**:

- **If logged into the SR Linux CLI**:
  ```text
  --{ running }--[  ]--
  A:srl-timeline# bash timeline
  ```

- **If logged into the SR Linux Bash Shell**:
  ```bash
  timeline
  ```

You can also launch the TUI pre-filtered to a specific subsystem or interface, or with a specific color theme:
```text
# From SR Linux CLI
bash timeline --filter "interface ethernet-1/1" --theme cobalt2

# From Linux Bash Shell
timeline --filter "interface ethernet-1/1" --theme dracula
```

---

## Color Themes & Terminal Appearance

`timeline` includes **11 built-in color themes** (9 dark themes and 2 light themes) with full-bleed surface rendering that adapts cleanly to both dark and light terminal emulators:

| Theme Name (`--theme`) | Display Name | Mode |
|---|---|---|
| `dark` *(default)* | GitHub Dark | Dark |
| `cobalt2` | Cobalt2 | Dark |
| `monokai` | Monokai | Dark |
| `solarized-dark` | Solarized Dark | Dark |
| `dracula` | Dracula | Dark |
| `nord` | Nord | Dark |
| `catppuccin-mocha` | Catppuccin Mocha | Dark |
| `tokyo-night` | Tokyo Night | Dark |
| `gruvbox-dark` | Gruvbox Dark | Dark |
| `light` | GitHub Light | Light |
| `solarized-light` | Solarized Light | Light |

- **Cycle live in the TUI**: Press <kbd>T</kbd> (`Shift+t`) at any time in the TUI (or inside modals when a text input is not focused) to cycle through all 11 themes immediately.
- **List available themes**:
  ```bash
  timeline themes
  ```
- **Select via CLI flag or environment variable**:
  ```bash
  timeline --theme nord
  TIMELINE_THEME=solarized-light timeline
  ```
- **Terminal color profile override**: Control ANSI color depth via `--color-profile <truecolor|256|ansi|ascii>` or `TIMELINE_COLOR_PROFILE` (also respects `NO_COLOR`):
  ```bash
  timeline --color-profile 256 --theme gruvbox-dark
  ```

---

## Direct Download One-Liner (Optional)

If your SR Linux switch has direct outbound internet access, you can download and install the binary and `app_mgr` config directly on the switch:

```bash
sudo curl -sL https://github.com/andywhitaker/srl-timeline/releases/latest/download/timeline -o /usr/local/bin/timeline && \
sudo chmod +x /usr/local/bin/timeline && \
sudo curl -sL https://raw.githubusercontent.com/andywhitaker/srl-timeline/main/timeline_config.yml -o /etc/opt/srlinux/appmgr/timeline_config.yml && \
sudo timeline daemon bootstrap
```

---

## Lab Deployment (Containerlab)

If you are running in a Containerlab test environment rather than a physical switch, you can deploy the included test lab directly:

```bash
sudo clab deploy -t timeline.clab.yml
```

Because `timeline` includes a pure-Go native Git object engine, minimal SR Linux containers do **not** require `git` or host bind-mounts of `/usr/bin/git` and `/usr/lib/git-core` for commit recording, history browsing, diffing, blame, restore, or export:

```yaml
topology:
  nodes:
    srl-timeline:
      kind: nokia_srlinux
      image: ghcr.io/nokia/srlinux:latest
      type: ixr-d2l
      binds:
        - bin/timeline:/usr/local/bin/timeline
        - timeline_config.yml:/etc/opt/srlinux/appmgr/timeline_config.yml
```

> **Note**: An external `git` binary is only needed if you use `timeline remote push` (or auto-push) to synchronize commits to a remote SSH Git server.

---

## TUI Keyboard Shortcuts

| Shortcut | Action |
|---|---|
| <kbd>j</kbd> / <kbd>↓</kbd> | Next commit in timeline (or scroll down in Detail pane) |
| <kbd>k</kbd> / <kbd>↑</kbd> | Previous commit in timeline (or scroll up in Detail pane) |
| <kbd>Tab</kbd> | Switch focus between Timeline pane and Details pane |
| <kbd>d</kbd> | Switch to **Diff View** |
| <kbd>c</kbd> | Switch to **Full Config View** |
| <kbd>b</kbd> | Switch to **Blame View** |
| <kbd>[</kbd> / <kbd>]</kbd> or <kbd>t</kbd> | Cycle through views (Diff $\rightarrow$ Config $\rightarrow$ Blame) |
| <kbd>1</kbd> / <kbd>2</kbd> / <kbd>3</kbd> | Select sub-formats (Unified, Path Changes, Flat CLI in Diff; CLI, JSON in Config) |
| <kbd>4</kbd> or <kbd>v</kbd> | Toggle **Live Compare Mode** (Revision vs Live Running Config) |
| <kbd>/</kbd> | Focus search / path filter bar |
| <kbd>Esc</kbd> | Clear filter / close modal |
| <kbd>r</kbd> | Open **Restore Modal** (Full configuration rollback) |
| <kbd>p</kbd> | Open **Cherry-Pick Modal** (Interactive subtree restore) |
| <kbd>R</kbd> / <kbd>F5</kbd> | Reload / refresh timeline from disk |
| <kbd>e</kbd> | Open **Export Modal** (Save to file or startup config) |
| <kbd>g</kbd> | Open **Remote Git Settings Modal** |
| <kbd>T</kbd> | Cycle **Color Theme** live (`dark`, `cobalt2`, `monokai`, `solarized-dark`, `dracula`, `nord`, etc.) |
| <kbd>?</kbd> | Display Help dialog |
| <kbd>q</kbd> | Quit application |

---

## Command-Line Interface (CLI Commands)

In addition to the interactive TUI, `timeline` provides subcommands for automation, scripting, CI/CD pipelines, or quick queries from the shell:

```bash
# List all built-in color themes
timeline themes

# View commit history
timeline log --limit 20
timeline log --filter bgp --theme cobalt2

# Semantic diff between revisions (formats: unified, json, cli, path)
timeline diff HEAD~1 HEAD
timeline diff HEAD~2 HEAD --format cli
timeline diff <sha1> <sha2> --format path --filter ethernet-1/1

# Show configuration at a point in time (or live running config)
timeline show HEAD --format cli
timeline show running --format json --filter system

# Line-by-line blame attribution
timeline blame --filter ethernet-1/1

# Full configuration restore
timeline restore <sha> --yes

# Cherry-pick restore specific paths (via 'cherry-pick' or 'restore --path')
timeline cherry-pick <sha> --path "/interface[name=ethernet-1/1]/description" --yes
timeline restore <sha> --path "/interface[name=ethernet-1/1]/description"

# Export configuration
timeline export <sha> --format json --output /tmp/snapshot.json
timeline export <sha> --save-startup

# Remote Git synchronization
timeline remote show
timeline remote key
timeline remote set git@github.com:org/srl-configs.git --auto-push
timeline remote push

# Background recorder daemon management
timeline daemon status
timeline daemon bootstrap
timeline daemon start
timeline daemon stop
```

---

## License

Apache License 2.0
