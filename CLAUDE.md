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
sshutil/sshutil.go    sshfs mount/unmount, SSH tunnel via exec
tui/app.go            Root Bubbletea model, message types, screen routing
tui/dashboard.go      Main screen — profile list with status indicators
tui/profilelist.go    Profile management screen (new/edit/delete)
tui/profileedit.go    Profile edit form (static fields + dynamic port forwards)
tui/proxy.go          SSH proxy/tunnel panel
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
- Profile reloads (`ProfilesLoadedMsg`) update both `m.dashboard.list` and `m.profileList` directly in `AppModel.Update` — do not rely on sub-model routing for this.
- Port forwards are stored as `[]string` in `"localPort:remoteHost:remotePort"` format, matching the `-L`/`-R` ssh flag syntax directly.
- SSHFS mount: directory is created with `os.MkdirAll` on mount and removed with `os.Remove` (empty-dir only) on unmount.

## Dependencies

| Package | Purpose |
|---|---|
| `github.com/charmbracelet/bubbletea` | TUI framework |
| `github.com/charmbracelet/bubbles` | textinput, list components |
| `github.com/charmbracelet/lipgloss` | terminal styling |
| `gopkg.in/yaml.v3` | YAML serialisation |
