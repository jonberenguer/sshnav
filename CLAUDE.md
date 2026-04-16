# sshnav

A terminal UI (TUI) SSH profile manager built with Go + Bubbletea.

## Build

There is no local Go toolchain — all builds run inside Docker:

```bash
bash build.sh          # produces ./sshnav binary
INSTALL_DIR=/usr/local/bin bash build.sh   # build + install
```

The Dockerfile uses a two-stage build (`golang:1.22-alpine` → `scratch`).
`go mod tidy && go mod download` runs inside the container before the build,
so `go.sum` does not need to be committed.

## Project layout

```
main.go               CLI entry point (TUI launcher + subcommands)
config/config.go      Profile struct, YAML load/save, ~/.ssh/config parser
sshutil/sshutil.go    sshfs mount/unmount, SSH tunnel, session command
tui/app.go            Root Bubbletea model, message types, screen routing
tui/dashboard.go      Main screen — profile list, submenu, right detail panel
tui/profilelist.go    Profile management screen (new/edit/duplicate/delete)
tui/profileedit.go    Profile edit form (static fields + dynamic port forwards)
tui/proxy.go          SSH proxy/tunnel panel with per-forward status
tui/sshfs.go          SSHFS mount panel
tui/styles.go         Colour palette, shared lipgloss styles, HelpLine helper
```

## Profile storage

- App profiles: `~/.config/sshnav/profiles.yaml`
- Format: YAML list of `Profile` structs (see `config/config.go`)
- `~/.ssh/config` entries are read-only and merged at load time (unless `--profiles-only`)

## CLI subcommands

```bash
sshnav                          # launch TUI
sshnav --profiles-only          # launch TUI, ignore ~/.ssh/config
sshnav export-ssh-config        # convert profiles.yaml → ~/.ssh/config format (stdout)
sshnav import-ssh-config        # convert ~/.ssh/config → profiles.yaml format (stdout)
```

## Key architecture notes

- All screen state lives on `AppModel` in `tui/app.go`; sub-models hold a `*AppModel` pointer for shared state access.
- **Shared mutable state (activeTunnels, etc.) must be written through `m.<submodel>.app.<field>` (the heap AppModel pointer), NOT through `m.<field>` inside `AppModel.Update`.** `AppModel.Update` has a value receiver, so `m` is a local copy; sub-models read from the original heap-allocated AppModel set up in `NewApp`. Writing only to the local copy means sub-models never see the change.
- Profile reloads (`ProfilesLoadedMsg`) update both `m.dashboard.list` and `m.profileList` directly in `AppModel.Update` — do not rely on sub-model routing for this.
- Port forwards are stored as `[]string` in `"localPort:remoteHost:remotePort"` format, matching the `-L`/`-R` ssh flag syntax directly. Values parsed from `~/.ssh/config` (which uses a space separator) are normalised to colon-separated by `config.normalizeForwardSpec` at parse time.
- Local forwards are pinned to `127.0.0.1` in `sshutil.buildSSHArgs` via `pinIPv4Bind` to prevent SSH from defaulting to IPv6-only (`::1`) on dual-stack hosts.
- `CheckLocalPort` probes both `127.0.0.1:port` and `[::1]:port` explicitly so it detects listeners on either address family.
- SSHFS mount: directory is created with `os.MkdirAll` on mount and removed with `os.Remove` (empty-dir only) on unmount.

## Tunnel lifecycle

`OpenTunnel` returns two channels:
- `startCh <-chan error` — fires after a 2-second grace period. `nil` = SSH is still running (ports bound); non-nil = SSH exited before establishing (startup failure).
- `doneCh <-chan TunnelResult` — fires when the SSH process exits for any reason.

SSH flags added for reliability: `-o ExitOnForwardFailure=yes`, `-o ConnectTimeout=15`, `-o BatchMode=yes`.

`TunnelStartedMsg` (from `startCh`) adds the session to `activeTunnels` on the heap AppModel.
`TunnelResultMsg` (from `doneCh`) removes it. Early-exit results (`EarlyExit=true`) suppress the redundant disconnect banner since the startup error was already shown.

## Dashboard submenu

Pressing `Enter` on a profile opens an action submenu in the right panel rather than navigating directly. The submenu supports both cursor navigation (`↑↓` + `Enter`) and direct letter shortcuts (`s`, `m`, `t`). The Mount option is conditionally shown only when the profile has SSHFS config. `submenuEntries()` is the single source of truth for the action list — both `Update` and `renderSubmenu` call it so they stay in sync.

## Dashboard list rendering

The list uses a custom `profileDelegate` (implements `list.ItemDelegate`) instead of `list.DefaultDelegate`. This prevents the default delegate's filter-match highlighting from corrupting pre-rendered ANSI escape sequences embedded in `Title()` strings. With the custom delegate, `Title()` and `Description()` return plain text (used only by the filter engine via `FilterValue()`); all styling is applied inside `Render()`.

## Interactive SSH sessions

`sshutil.SessionCommand` builds an `*exec.Cmd` for a plain interactive SSH session (no `-N`, no port forwards, no `BatchMode`). It is invoked via `tea.ExecProcess`, which suspends Bubbletea's event loop, hands the terminal to SSH, and resumes the TUI when the user exits the session.

## Dependencies

| Package | Purpose |
|---|---|
| `github.com/charmbracelet/bubbletea` | TUI framework |
| `github.com/charmbracelet/bubbles` | textinput, list components |
| `github.com/charmbracelet/lipgloss` | terminal styling |
| `gopkg.in/yaml.v3` | YAML serialisation |
