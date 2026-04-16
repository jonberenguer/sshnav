package config

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Source indicates where a profile came from.
type Source int

const (
	SourceApp Source = iota // ~/.config/sshnav/profiles.json
	SourceSSH               // ~/.ssh/config (read-only)
)

// Profile represents a saved SSH/SSHFS host configuration.
type Profile struct {
	Name         string `yaml:"name"`
	Host         string `yaml:"host"`
	User         string `yaml:"user,omitempty"`
	Port         int    `yaml:"port,omitempty"`
	IdentityFile string `yaml:"identity_file,omitempty"`

	// SSHFS-specific
	RemotePath string `yaml:"remote_path,omitempty"`
	MountPoint string `yaml:"mount_point,omitempty"`
	SSHFSOpts  string `yaml:"sshfs_opts,omitempty"`

	// ProxyJump: comma-separated list of jump hosts (e.g. "jump1.example.com,jump2.example.com")
	ProxyJump string `yaml:"proxy_jump,omitempty"`

	// Port forwards — each entry is "localPort:remoteHost:remotePort" passed to -L/-R
	LocalForwards  []string `yaml:"local_forwards,omitempty"`
	RemoteForwards []string `yaml:"remote_forwards,omitempty"`

	Source Source `yaml:"-"` // not persisted
}

func (p Profile) PortOrDefault() int {
	if p.Port == 0 {
		return 22
	}
	return p.Port
}

// ---- App-managed profiles (read/write) ----

func appConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "sshnav")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "profiles.yaml"), nil
}

// LoadAppProfiles reads ~/.config/sshnav/profiles.yaml.
// Returns empty slice (not error) if file doesn't exist yet.
func LoadAppProfiles() ([]Profile, error) {
	path, err := appConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Profile{}, nil
	}
	if err != nil {
		return nil, err
	}
	var profiles []Profile
	if err := yaml.Unmarshal(data, &profiles); err != nil {
		return nil, err
	}
	for i := range profiles {
		profiles[i].Source = SourceApp
	}
	return profiles, nil
}

// SaveAppProfiles writes the full app-managed profile list atomically.
func SaveAppProfiles(profiles []Profile) error {
	path, err := appConfigPath()
	if err != nil {
		return err
	}
	// Filter to only app profiles before saving
	var toSave []Profile
	for _, p := range profiles {
		if p.Source == SourceApp {
			toSave = append(toSave, p)
		}
	}
	data, err := yaml.Marshal(toSave)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---- ~/.ssh/config parser (read-only) ----

// LoadSSHConfigProfiles parses ~/.ssh/config and returns Host entries.
// Wildcard (*) hosts are skipped.
func LoadSSHConfigProfiles() ([]Profile, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".ssh", "config")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Profile{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var profiles []Profile
	var current *Profile

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])

		switch key {
		case "host":
			if val == "*" {
				current = nil
				continue
			}
			if current != nil {
				profiles = append(profiles, *current)
			}
			current = &Profile{
				Name:   val,
				Host:   val,
				Source: SourceSSH,
			}
		case "hostname":
			if current != nil {
				current.Host = val
			}
		case "user":
			if current != nil {
				current.User = val
			}
		case "identityfile":
			if current != nil {
				current.IdentityFile = val
			}
		case "proxyjump":
			if current != nil {
				current.ProxyJump = val
			}
		case "localforward":
			if current != nil {
				current.LocalForwards = append(current.LocalForwards, normalizeForwardSpec(val))
			}
		case "remoteforward":
			if current != nil {
				current.RemoteForwards = append(current.RemoteForwards, normalizeForwardSpec(val))
			}
		}
	}
	if current != nil {
		profiles = append(profiles, *current)
	}
	return profiles, scanner.Err()
}

// normalizeForwardSpec converts ~/.ssh/config space-separated port-forward
// specs into the colon-separated format required by ssh(1) -L / -R flags.
//
//	"9090 localhost:80"           → "9090:localhost:80"
//	"127.0.0.1:8080 localhost:80" → "127.0.0.1:8080:localhost:80"
//	"9090:localhost:80"           → "9090:localhost:80"  (already correct, unchanged)
func normalizeForwardSpec(val string) string {
	i := strings.Index(val, " ")
	if i >= 0 {
		return val[:i] + ":" + val[i+1:]
	}
	return val
}

// LoadAllProfiles merges app + ssh/config profiles. App profiles listed first.
func LoadAllProfiles() ([]Profile, error) {
	app, err := LoadAppProfiles()
	if err != nil {
		return nil, err
	}
	sshCfg, err := LoadSSHConfigProfiles()
	if err != nil {
		// Non-fatal: just skip ssh/config on error
		sshCfg = nil
	}
	return append(app, sshCfg...), nil
}
