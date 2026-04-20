# sshnav

A terminal UI (TUI) SSH profile manager built with Go + Bubbletea.

## Build

There is no local Go toolchain — all builds run inside Docker:

```bash
bash build.sh          # produces ./sshnav binary
INSTALL_DIR=/usr/local/bin bash build.sh   # build + install
```

The Dockerfile uses a two-stage build (`golang:1.24-alpine` → `scratch`).
`go mod tidy && go mod download` runs inside the container before the build,
so `go.sum` does not need to be committed.

## Project layout

```
main.go               CLI entry point (TUI launcher + subcommands)
config/config.go      Profile struct, YAML load/save, ~/.ssh/config parser
sshutil/sshutil.go    sshfs mount/unmount, SSH tunnel, session command
tui/app.go            Root Bubbletea model, message types, screen routing
tui/dashboard.go      Main screen — profile list, submenu, right detail panel
tui/profileedit.go    Profile edit form (static fields + dynamic port forwards)
tui/proxy.go          SSH proxy/tunnel panel with per-forward status
tui/sshfs.go          SSHFS mount panel
tui/styles.go         Colour palette, shared lipgloss styles, HelpLine/PageLayout/RightPanelWidth helpers
```

## Profile storage

- App profiles: `~/.config/sshnav/profiles.yaml` (default) or a custom path via `--profiles`
- Format: YAML list of `Profile` structs (see `config/config.go`); profiles are separated by a blank line via `MarshalProfilesSpaced`
- `~/.ssh/config` entries are read-only and merged at load time (unless `--profiles-only` or `--profiles`)

## CLI subcommands

```bash
sshnav                               # launch TUI
sshnav --profiles-only               # launch TUI, ignore ~/.ssh/config
sshnav --profiles /path/to/file.yaml # launch TUI using a custom profiles file (implies --profiles-only)
sshnav export-ssh-config             # convert profiles.yaml → ~/.ssh/config format (stdout)
sshnav import-ssh-config             # convert ~/.ssh/config → profiles.yaml format (stdout)
sshnav import-ssh-config /path/file  # convert a specific SSH config file → profiles.yaml format (stdout)
```

## Profile struct

Key fields on `config.Profile`:

| Field | YAML key | Purpose |
|---|---|---|
| `Name` | `name` | Display name and SSH Host alias |
| `Host` | `host` | Hostname or IP |
| `User` | `user` | SSH username |
| `Port` | `port` | SSH port (default 22) |
| `IdentityFile` | `identity_file` | Path to private key |
| `RemoteDir` | `remote_dir` | Working directory for interactive SSH sessions |
| `RemotePath` | `remote_path` | Remote path for SSHFS |
| `MountPoint` | `mount_point` | Local SSHFS mount point |
| `SSHFSOpts` | `sshfs_opts` | Comma-separated sshfs `-o` options |
| `ProxyJump` | `proxy_jump` | Comma-separated ProxyJump chain |
| `LocalForwards` | `local_forwards` | `[]string` in `localPort:remoteHost:remotePort` format |
| `RemoteForwards` | `remote_forwards` | `[]string` in `localPort:remoteHost:remotePort` format |

`Source` is not persisted (set to `SourceApp` or `SourceSSH` at load time).

## Key architecture notes

- All screen state lives on `AppModel` in `tui/app.go`; sub-models hold a `*AppModel` pointer for shared state access.
- **Shared mutable state (activeTunnels, etc.) must be written through `m.<submodel>.app.<field>` (the heap AppModel pointer), NOT through `m.<field>` inside `AppModel.Update`.** `AppModel.Update` has a value receiver, so `m` is a local copy; sub-models read from the original heap-allocated AppModel set up in `NewApp`. Writing only to the local copy means sub-models never see the change.
- Profile reloads (`ProfilesLoadedMsg`) update `m.dashboard.profiles` and `m.dashboard.list` directly in `AppModel.Update` — do not rely on sub-model routing for this.
- Port forwards are stored as `[]string` in `"localPort:remoteHost:remotePort"` format, matching the `-L`/`-R` ssh flag syntax directly. Values parsed from `~/.ssh/config` (which uses a space separator) are normalised to colon-separated by `config.normalizeForwardSpec` at parse time.
- Local forwards are pinned to `127.0.0.1` in `sshutil.buildSSHArgs` via `pinIPv4Bind` to prevent SSH from defaulting to IPv6-only (`::1`) on dual-stack hosts.
- `CheckLocalPort` probes both `127.0.0.1:port` and `[::1]:port` explicitly so it detects listeners on either address family.
- SSHFS mount: directory is created with `os.MkdirAll` on mount and removed with `os.Remove` (empty-dir only) on unmount.
- `AppModel.profilesPath` holds the custom profiles file path (empty = use default). All load and save operations check this field. `NewApp(profilesOnly bool, profilesPath string)` is the constructor.

## Tunnel lifecycle

`OpenTunnel` returns two channels:
- `startCh <-chan error` — fires after a 2-second grace period. `nil` = SSH is still running (ports bound); non-nil = SSH exited before establishing (startup failure).
- `doneCh <-chan TunnelResult` — fires when the SSH process exits for any reason.

SSH flags added for reliability: `-o ExitOnForwardFailure=yes`, `-o ConnectTimeout=15`, `-o BatchMode=yes`.

`TunnelStartedMsg` (from `startCh`) adds the session to `activeTunnels` on the heap AppModel.
`TunnelResultMsg` (from `doneCh`) removes it. Early-exit results (`EarlyExit=true`) suppress the redundant disconnect banner since the startup error was already shown.

## Interactive SSH sessions

`sshutil.SessionCommand` builds an `*exec.Cmd` for a plain interactive SSH session (no `-N`, no port forwards, no `BatchMode`). It is invoked via `tea.ExecProcess`, which suspends Bubbletea's event loop, hands the terminal to SSH, and resumes the TUI when the user exits the session.

When `Profile.RemoteDir` is set, `-t` is added to force PTY allocation (SSH suppresses PTY when a remote command is given), and `"cd '<dir>' && exec $SHELL -l"` is appended as the remote command. The path is single-quote–escaped (`'\''` for embedded single quotes).

## Layout helpers (`tui/styles.go`)

Three shared package-level functions handle consistent layout across all screens:

- `PageLayout(width, height, body, footer string) string` — wraps `body` in a fixed `Height(height - footerH)` container so the footer is always pinned to the last row of the terminal regardless of content length. All screens call this as their final return.
- `RightPanelWidth(totalWidth int) int` — returns the column width to reserve for a right-hand detail panel (≥ 90 columns required; result clamped to 32–48). Returns 0 on narrow terminals, disabling the panel.
- `RenderEmptyPanel(width, height int) string` — renders a rounded-border placeholder box at the correct dimensions. Used when no detail content is available so the list column width never shifts.

Each sub-model must have `width` and `height` fields populated via `SetSize` — called both on `WindowSizeMsg` and immediately after a sub-model is created in the relevant message handler.

## Dashboard right panel and filter mode

The dashboard splits into a list column and a right detail panel on terminals ≥ 90 columns wide. Both columns are wrapped in explicit `Width`/`Height` containers before `JoinHorizontal` so the panel is always flush with the right terminal edge regardless of list output width. When filter mode is active (`list.Filtering`) the panel is hidden entirely and the list expands to `m.width` — this prevents the empty panel box from appearing while the user types. The bubbles built-in help hint row is suppressed via `SetShowHelp(false)`; the custom footer covers all actions.

## Dashboard submenu

Pressing `Enter` on a profile opens an action submenu in the right panel rather than navigating directly. The submenu supports both cursor navigation (`↑↓` + `Enter`) and direct letter shortcuts. `submenuEntries()` is the single source of truth for the action list — both `Update` and `renderSubmenu` call it so they stay in sync.

Available actions:

| Key | Action | Condition |
|---|---|---|
| `s` | SSH Session | always |
| `m` | Mount SSHFS | only when profile has `MountPoint` or `RemotePath` |
| `t` | Tunnel / Proxy | always |
| `e` | Edit | `SourceApp` only |
| `c` | Duplicate | `SourceApp` only |
| `d` | Delete | `SourceApp` only |

Selecting `d` sets `DashboardModel.confirmDelete` (profile name string) and shows a `[y/N]` inline prompt. The confirmation is handled before filter-state and submenu checks in `Update`.

## Dashboard list rendering

The list uses a custom `profileDelegate` (implements `list.ItemDelegate`) instead of `list.DefaultDelegate`. This prevents the default delegate's filter-match highlighting from corrupting pre-rendered ANSI escape sequences embedded in `Title()` strings. With the custom delegate, `Title()` and `Description()` return plain text (used only by the filter engine via `FilterValue()`); all styling is applied inside `Render()`.

## Dashboard status polling

`tickMsg` / `tickCmd()` (5-second interval) is started in `AppModel.Init()` and handled in `AppModel.Update` — not in `DashboardModel`. This keeps the tick chain alive regardless of which screen is active. The tick handler skips `SetItems` when the list is in `list.Filtering` state to avoid clearing the user's search results.

## SSHFS screen polling

`mountPollMsg` / `mountPollCmd()` (1-second interval) is started when `SSHFSTargetMsg` is processed in `AppModel.Update`. `SSHFSModel.Update` handles `mountPollMsg` by returning another `mountPollCmd()`, keeping the chain alive while the SSHFS screen is active. When the user navigates away, the tick messages are routed to other sub-models which ignore them and the chain dies naturally.

## Profile edit — dirty tracking and discard confirmation

`ProfileEditModel` has two guard fields:
- `dirty bool` — set to `true` whenever a `tea.KeyMsg` reaches the input-update path at the bottom of `Update` (i.e. any key not consumed by the navigation/action switch above). Cleared on a successful `ctrl+s` save.
- `confirmDiscard bool` — set to `true` when `esc` is pressed while `dirty` is true. When active, only `y`/`Y` navigates away; any other key cancels the prompt and returns to editing.

## config package helpers

| Function | Purpose |
|---|---|
| `LoadAppProfilesFrom(path)` | Load profiles from an explicit path |
| `LoadAppProfiles()` | Load from default `~/.config/sshnav/profiles.yaml` |
| `SaveAppProfilesTo(path, profiles)` | Atomic save to an explicit path; creates parent dirs |
| `SaveAppProfiles(profiles)` | Atomic save to default path |
| `LoadSSHConfigProfilesFrom(path)` | Parse an SSH config file at an explicit path |
| `LoadSSHConfigProfiles()` | Parse `~/.ssh/config` |
| `MarshalProfilesSpaced(profiles)` | Serialize as YAML with a blank line between each entry |
| `LoadAllProfiles()` | Merge app + SSH config profiles (app profiles listed first) |

## Dependencies

| Package | Version | Purpose |
|---|---|---|
| `github.com/charmbracelet/bubbletea` | v1.3.10 | TUI framework |
| `github.com/charmbracelet/bubbles` | v1.0.0 | textinput, list components |
| `github.com/charmbracelet/lipgloss` | v1.1.0 | terminal styling |
| `gopkg.in/yaml.v3` | v3.0.1 | YAML serialisation |
