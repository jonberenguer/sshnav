package sshutil

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

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
	Session *TunnelSession
	Err     error
}

// OpenTunnel starts an SSH connection through ProxyJump hosts.
// The process is kept alive with ServerAliveInterval; result sent once the
// process exits or fails to start.
func OpenTunnel(p config.Profile) (*TunnelSession, <-chan TunnelResult) {
	ch := make(chan TunnelResult, 1)
	sess := &TunnelSession{Profile: p}

	go func() {
		args := buildSSHArgs(p)
		// Keep the tunnel alive without allocating a PTY
		args = append(args,
			"-o", "ServerAliveInterval=30",
			"-o", "ServerAliveCountMax=3",
			"-N", // no remote command
		)
		cmd := exec.Command("ssh", args...)
		sess.mu.Lock()
		sess.cmd = cmd
		sess.mu.Unlock()

		if err := cmd.Start(); err != nil {
			ch <- TunnelResult{sess, fmt.Errorf("ssh start: %w", err)}
			return
		}
		// Block until process exits
		err := cmd.Wait()
		if err != nil {
			ch <- TunnelResult{sess, fmt.Errorf("ssh exited: %w", err)}
		} else {
			ch <- TunnelResult{sess, nil}
		}
	}()
	return sess, ch
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
			args = append(args, "-L", fwd)
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
