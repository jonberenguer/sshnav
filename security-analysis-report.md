# sshnav — Security Analysis Report

**Date:** 2026-04-21
**Scope:** Static code review of the sshnav codebase (`main.go`, `config/config.go`, `sshutil/sshutil.go`, `tui/`)
**Model:** claude-sonnet-4-6

---

## Summary

| # | Severity | Area | Status |
|---|----------|------|--------|
| 1 | Medium | `export-ssh-config` newline injection | Fixed |
| 2 | Medium | `sshfs_opts` flag injection | Fixed |
| 3 | Medium | ProxyJump / IdentityFile flag injection | Fixed |
| 4 | Low | Port forward spec not validated | Fixed |
| 5 | Low | `~/.ssh/config` `Port` field silently ignored | Fixed |
| 6 | Low | Duplicate profile names not prevented | Fixed |
| 7 | Low | PID filename collision after sanitization | Fixed |
| 8 | Low | `remote_dir` shell quoting | Open |
| 9 | Low | Mount-point TOCTOU | Fixed |
| 10 | Info | PID reuse race on tunnel reattach | Open |
| 11 | Info | `~/.ssh/config` `Include` directive not handled | Open |
| 12 | Info | SSH args visible in process list | Open |
| 13 | Info | No reconnect rate limiting on tunnels | Fixed |

---

## Findings

### [1] Medium — `export-ssh-config` newline injection

**File:** `main.go` — `exportSSHConfig()`

Profile names and field values are written directly into SSH config output without any escaping or sanitization:

```go
sb.WriteString("Host " + p.Name + "\n")
sb.WriteString("    HostName " + p.Host + "\n")
```

A profile name containing an embedded newline — for example `safe\nHost *\n    ForwardAgent yes` — would inject arbitrary SSH config directives into the output. The risk is highest when users pipe the result directly to `~/.ssh/config` using a shared `--profiles` file they do not fully control.

**Recommendation:** Strip or reject control characters (including `\n`, `\r`) from all field values before writing the SSH config output.

---

### [2] Medium — `sshfs_opts` flag injection

**File:** `sshutil/sshutil.go` — `Mount()`

SSHFS options are split on commas and passed directly to `sshfs -o` with no allowlist:

```go
for _, opt := range strings.Split(p.SSHFSOpts, ",") {
    args = append(args, "-o", opt)
}
```

A crafted profile could supply dangerous sshfs flags (e.g. `allow_other`, `-f`, `-d`). Only use profiles from sources you trust.

**Recommendation:** Validate each option against a known-safe allowlist, or reject options that begin with `-`.

---

### [3] Medium — ProxyJump / IdentityFile flag injection

**File:** `sshutil/sshutil.go` — `buildSSHArgs()`, `SessionCommand()`

Neither `ProxyJump` nor `IdentityFile` are validated before being passed to the SSH subprocess:

```go
args = append(args, "-J", p.ProxyJump)
args = append(args, "-i", p.IdentityFile)
```

A value beginning with `-` could inject unexpected SSH flags. Only use profiles from sources you trust.

**Recommendation:** Validate that `ProxyJump` matches a `host[:port]` pattern and that `IdentityFile` is a plausible file path (does not start with `-`).

---

### [4] Low — Port forward spec not validated

**File:** `sshutil/sshutil.go` — `buildSSHArgs()`; `tui/profileedit.go` — `validate()`

`LocalForwards` and `RemoteForwards` values accept any string and are passed verbatim to `ssh -L` / `ssh -R`:

```go
args = append(args, "-L", pinIPv4Bind(fwd))
args = append(args, "-R", fwd)
```

There is no shell injection risk because `exec.Command` is used without a shell, but there is no format validation. An invalid or adversarial spec causes confusing SSH errors rather than a user-friendly validation message in the UI.

**Recommendation:** Validate forward specs in `validate()` against a simple regex such as `^\d+:[^:]+:\d+$` before saving.

---

### [5] Low — `~/.ssh/config` `Port` field silently ignored

**File:** `config/config.go` — `LoadSSHConfigProfilesFrom()`

The SSH config parser handles `hostname`, `user`, `identityfile`, `proxyjump`, and port forwards, but the `Port` directive is not implemented. Hosts loaded from `~/.ssh/config` that listen on non-22 ports silently use port 22, causing connection failures with no obvious error.

**Recommendation:** Add a `case "port":` branch to the SSH config parser that parses the value as an integer and sets `current.Port`.

---

### [6] Low — Duplicate profile names not prevented

**File:** `tui/profileedit.go` — `validate()`; `tui/dashboard.go` — `deleteProfileCmd()`

No uniqueness check is enforced on profile names at save time. Two profiles with the same name result in:
- The same PID file being shared between their tunnels (the second open tunnel overwrites the first's PID).
- Ambiguous edit and delete behaviour (the first match wins).

**Recommendation:** In `saveProfileCmd`, check whether any existing profile (other than `origName`) already uses the new name and return an error if so.

---

### [7] Low — PID filename collision after sanitization

**File:** `sshutil/sshutil.go` — `pidFileName()`

The PID filename sanitization replaces `/` and `\` with `_` but no other characters:

```go
safe := strings.Map(func(r rune) rune {
    if r == '/' || r == '\\' || r == 0 {
        return '_'
    }
    return r
}, profileName)
```

Two profiles whose names differ only by `/` vs `_` (e.g. `foo/bar` and `foo_bar`) resolve to the same filename `foo_bar.pid`. The second tunnel silently clobbers the first's PID entry.

**Recommendation:** Use a content-addressing scheme (e.g. hex-encode the profile name, or use a hash) instead of relying on character replacement for filename safety.

---

### [8] Low — `remote_dir` shell quoting

**File:** `sshutil/sshutil.go` — `SessionCommand()`

The `remote_dir` field is single-quote–escaped before being sent as a remote command:

```go
escaped := "'" + strings.ReplaceAll(p.RemoteDir, "'", "'\\''") + "'"
args = append(args, "cd "+escaped+" && exec $SHELL -l")
```

Standard POSIX escaping is applied. However, unusual remote shell configurations or non-POSIX shells could interpret the string differently.

**Recommendation:** Document and accept this as a known limitation given that `remote_dir` is app-managed user input.

---

### [9] Low — Mount-point TOCTOU

**File:** `sshutil/sshutil.go` — `Mount()`

`os.MkdirAll` is called on the mount point before SSHFS runs. There is a narrow race window where a symlink could be created at that path between the directory creation and the SSHFS mount, redirecting the mount to an unintended target.

**Recommendation:** Check that the resolved path matches the expected mount point after `MkdirAll` (e.g. using `os.Lstat` to confirm it is a real directory and not a symlink).

---

### [10] Info — PID reuse race on tunnel reattach

**File:** `sshutil/sshutil.go` — `AttachTunnel()`, `IsRunning()`; `tui/app.go` — `loadTunnelsCmd()`

Between reading a PID file and calling `kill -0` on the PID to check liveness, the original SSH process could die and a new, unrelated process could be assigned the same PID. The reattached `TunnelSession` would then wrap the wrong process.

This is practically unexploitable on modern Linux (the PID namespace wraps at 32768 and wrap-around is slow under normal workloads), but the window exists.

**Recommendation:** Accept as a known limitation; document in code. A mitigation would be to also record the process start time (`/proc/<pid>/stat` field 22) in the PID file and verify it on reattach.

---

### [11] Info — `~/.ssh/config` `Include` directive not handled

**File:** `config/config.go` — `LoadSSHConfigProfilesFrom()`

SSH config files that use `Include` directives to pull in additional files have those included files silently skipped. Hosts defined only in included files never appear in sshnav.

**Recommendation:** Implement recursive include support, or document the limitation clearly.

---

### [12] Info — SSH args visible in process list

**File:** `sshutil/sshutil.go` — `buildSSHArgs()`, `SessionCommand()`

Identity-file paths and proxy-jump hosts appear in the process argument list while sessions or tunnels are active and are visible to any user who can run `ps` on the same host.

**Recommendation:** This is inherent to the SSH CLI and cannot be addressed at the application level. No action required.

---

### [13] Info — No reconnect rate limiting on tunnels

**File:** `tui/proxy.go` — `Update()` `"t"` key handler

Each failed tunnel open attempt spawns a new short-lived `ssh` process immediately with no backoff or debounce. A user clicking the `t` key rapidly while a connection is failing could briefly hammer an SSH server with repeated connection attempts.

**Recommendation:** Enforce a minimum delay (e.g. 5 seconds) between successive tunnel open attempts for the same profile, or disable the action button while `m.loading` is true (it already is — but the `loading` flag is cleared on `TunnelResultMsg`, not on the start of the next attempt).

---

## Dependencies

All direct dependencies were upgraded to their current major versions as of this review. No known CVEs were identified in the current dependency set.

| Package | Version | Notes |
|---------|---------|-------|
| `github.com/charmbracelet/bubbletea` | v1.3.10 | Current |
| `github.com/charmbracelet/bubbles` | v1.0.0 | Current |
| `github.com/charmbracelet/lipgloss` | v1.1.0 | Current |
| `gopkg.in/yaml.v3` | v3.0.1 | Current |
