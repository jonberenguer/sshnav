package sshutil

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"sshnav/config"
)

// MountStatus reflects whether a path is currently mounted via sshfs.
type MountStatus int

const (
	MountUnknown   MountStatus = iota
	MountMounted               // present in /proc/mounts
	MountUnmounted             // not in /proc/mounts
)

// CheckMount checks /proc/mounts for the given local path.
func CheckMount(mountPoint string) MountStatus {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return MountUnknown
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		// /proc/mounts: device mountpoint fstype options dump pass
		if len(fields) >= 3 && fields[1] == mountPoint && fields[2] == "fuse.sshfs" {
			return MountMounted
		}
	}
	return MountUnmounted
}

// MountResult carries the outcome of a mount/unmount attempt.
type MountResult struct {
	Profile config.Profile
	Err     error
}

// Mount runs sshfs in a subprocess. Non-blocking — result sent on returned channel.
func Mount(p config.Profile) <-chan MountResult {
	ch := make(chan MountResult, 1)
	go func() {
		if p.MountPoint == "" || p.RemotePath == "" {
			ch <- MountResult{p, fmt.Errorf("mount point and remote path are required")}
			return
		}
		if err := os.MkdirAll(p.MountPoint, 0o755); err != nil {
			ch <- MountResult{p, fmt.Errorf("create mount point: %w", err)}
			return
		}

		// Build sshfs argument list
		remote := fmt.Sprintf("%s:%s", hostSpec(p), p.RemotePath)
		args := []string{remote, p.MountPoint}
		args = append(args, "-o", fmt.Sprintf("port=%d", p.PortOrDefault()))
		if p.User != "" {
			args = append(args, "-o", fmt.Sprintf("ssh_command=ssh -l %s", p.User))
		}
		if p.IdentityFile != "" {
			args = append(args, "-o", fmt.Sprintf("IdentityFile=%s", p.IdentityFile))
		}
		if p.ProxyJump != "" {
			args = append(args, "-o", fmt.Sprintf("ProxyJump=%s", p.ProxyJump))
		}
		if p.SSHFSOpts != "" {
			for _, opt := range strings.Split(p.SSHFSOpts, ",") {
				opt = strings.TrimSpace(opt)
				if opt != "" {
					args = append(args, "-o", opt)
				}
			}
		}

		cmd := exec.Command("sshfs", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			ch <- MountResult{p, fmt.Errorf("sshfs: %w — %s", err, strings.TrimSpace(string(out)))}
			return
		}
		ch <- MountResult{p, nil}
	}()
	return ch
}

// Unmount runs fusermount -u and removes the mount directory if it is empty.
// Non-blocking.
func Unmount(p config.Profile) <-chan MountResult {
	ch := make(chan MountResult, 1)
	go func() {
		if p.MountPoint == "" {
			ch <- MountResult{p, fmt.Errorf("no mount point configured")}
			return
		}
		cmd := exec.Command("fusermount", "-u", p.MountPoint)
		out, err := cmd.CombinedOutput()
		if err != nil {
			ch <- MountResult{p, fmt.Errorf("fusermount: %w — %s", err, strings.TrimSpace(string(out)))}
			return
		}
		// Best-effort removal: os.Remove only succeeds on empty directories,
		// so this is safe even if the path has unrelated contents.
		_ = os.Remove(p.MountPoint)
		ch <- MountResult{p, nil}
	}()
	return ch
}

// ---- ProxyJump tunnel ----

// TunnelSession holds a running SSH ProxyJump process.
type TunnelSession struct {
	Profile config.Profile
	cmd     *exec.Cmd
	mu      sync.Mutex
}

// TunnelResult is sent when a tunnel starts or stops.
type TunnelResult struct {
	Session   *TunnelSession
	Err       error
	EarlyExit bool // true if SSH died before the grace period (startup failure)
}

// OpenTunnel starts an SSH connection through ProxyJump hosts.
// Returns the session plus two channels:
//   - startCh: fires once after a 2-second grace period; nil = confirmed
//     running (ports should be bound), non-nil = startup failure.
//   - doneCh: fires once when the SSH process exits.
func OpenTunnel(p config.Profile) (*TunnelSession, <-chan error, <-chan TunnelResult) {
	startCh := make(chan error, 1)
	doneCh := make(chan TunnelResult, 1)
	sess := &TunnelSession{Profile: p}

	go func() {
		args := buildSSHArgs(p)
		args = append(args,
			"-o", "ServerAliveInterval=30",
			"-o", "ServerAliveCountMax=3",
			"-o", "ExitOnForwardFailure=yes", // exit if a port forward can't bind
			"-o", "ConnectTimeout=15",
			"-o", "BatchMode=yes", // never prompt — fail fast
			"-N",                  // no remote command
		)
		cmd := exec.Command("ssh", args...)
		sess.mu.Lock()
		sess.cmd = cmd
		sess.mu.Unlock()

		if err := cmd.Start(); err != nil {
			startErr := fmt.Errorf("ssh start: %w", err)
			startCh <- startErr
			doneCh <- TunnelResult{Session: sess, Err: startErr, EarlyExit: true}
			return
		}

		// Race: did SSH exit before the grace period?
		exitCh := make(chan error, 1)
		go func() { exitCh <- cmd.Wait() }()

		timer := time.NewTimer(2 * time.Second)
		select {
		case err := <-exitCh:
			// Exited before grace period — startup failure.
			timer.Stop()
			var startErr error
			if err != nil {
				startErr = fmt.Errorf("ssh: %w", err)
			} else {
				startErr = fmt.Errorf("tunnel closed before establishing")
			}
			startCh <- startErr
			doneCh <- TunnelResult{Session: sess, EarlyExit: true}

		case <-timer.C:
			// Still running — declare connected.
			startCh <- nil
			if err := <-exitCh; err != nil {
				doneCh <- TunnelResult{Session: sess, Err: fmt.Errorf("ssh exited: %w", err)}
			} else {
				doneCh <- TunnelResult{Session: sess}
			}
		}
	}()

	return sess, startCh, doneCh
}

// SessionCommand returns an exec.Cmd for an interactive SSH session with the
// given profile. It is intended for use with tea.ExecProcess — the TUI suspends
// and the user gets a full PTY shell. No port forwards or -N are added; those
// belong to OpenTunnel.
func SessionCommand(p config.Profile) *exec.Cmd {
	args := []string{
		"-p", fmt.Sprintf("%d", p.PortOrDefault()),
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
	}
	if p.IdentityFile != "" {
		args = append(args, "-i", p.IdentityFile)
	}
	if p.ProxyJump != "" {
		args = append(args, "-J", p.ProxyJump)
	}
	args = append(args, hostSpec(p))
	return exec.Command("ssh", args...)
}

// CheckLocalPort returns true if something is already listening on the given
// local TCP port on either IPv4 (127.0.0.1) or IPv6 (::1).
// SSH may bind local forwards to either family depending on the system, so
// we probe each address explicitly rather than relying on the ambiguous ":"
// wildcard that Go resolves to a single socket type.
func CheckLocalPort(port int) bool {
	if port <= 0 {
		return false
	}
	for _, addr := range []string{
		fmt.Sprintf("127.0.0.1:%d", port),
		fmt.Sprintf("[::1]:%d", port),
	} {
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return true // that address:port is in use
		}
		l.Close()
	}
	return false
}

// Close terminates the tunnel process.
func (s *TunnelSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

// IsRunning returns true if the process is still alive.
func (s *TunnelSession) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd == nil || s.cmd.Process == nil {
		return false
	}
	// Signal 0: check existence without sending a real signal
	return s.cmd.ProcessState == nil
}

// ---- helpers ----

func hostSpec(p config.Profile) string {
	if p.User != "" {
		return p.User + "@" + p.Host
	}
	return p.Host
}

func buildSSHArgs(p config.Profile) []string {
	args := []string{
		"-p", fmt.Sprintf("%d", p.PortOrDefault()),
	}
	if p.IdentityFile != "" {
		args = append(args, "-i", p.IdentityFile)
	}
	if p.ProxyJump != "" {
		args = append(args, "-J", p.ProxyJump)
	}
	for _, fwd := range p.LocalForwards {
		if fwd != "" {
			args = append(args, "-L", pinIPv4Bind(fwd))
		}
	}
	for _, fwd := range p.RemoteForwards {
		if fwd != "" {
			args = append(args, "-R", fwd)
		}
	}
	args = append(args, hostSpec(p))
	return args
}

// pinIPv4Bind ensures a local-forward spec binds explicitly to 127.0.0.1.
// SSH may default to the IPv6 loopback (::1) on dual-stack hosts when no bind
// address is given. We detect specs that already contain an explicit bind
// address (anything whose first colon-delimited segment is not a bare port
// number) and leave those unchanged.
//
//   "8080:host:80"              → "127.0.0.1:8080:host:80"
//   "127.0.0.1:8080:host:80"   → unchanged
//   "[::1]:8080:host:80"       → unchanged
func pinIPv4Bind(fwd string) string {
	i := strings.Index(fwd, ":")
	if i < 0 {
		return fwd
	}
	// If the first segment parses as a plain integer, there is no bind address.
	if _, err := strconv.Atoi(fwd[:i]); err == nil {
		return "127.0.0.1:" + fwd
	}
	return fwd
}
