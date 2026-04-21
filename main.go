package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"sshnav/config"
	"sshnav/tui"
)

func main() {
	// Subcommands that don't need flags
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "export-ssh-config":
			exportSSHConfig()
			return
		case "import-ssh-config":
			var path string
			if len(os.Args) > 2 {
				path = os.Args[2]
			}
			importSSHConfig(path)
			return
		}
	}

	fs := flag.NewFlagSet("sshnav", flag.ExitOnError)
	profilesOnly := fs.Bool("profiles-only", false, "only load profiles from profiles.yaml, ignore ~/.ssh/config")
	profilesFile := fs.String("profiles", "", "path to a custom profiles.yaml file (implies --profiles-only)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: sshnav [--profiles-only] [--profiles /path/to/profiles.yaml]\n")
		fmt.Fprintf(os.Stderr, "       sshnav export-ssh-config\n")
		fmt.Fprintf(os.Stderr, "       sshnav import-ssh-config [/path/to/ssh_config]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "sshnav: unknown argument %q\n", fs.Arg(0))
		fs.Usage()
		os.Exit(1)
	}

	app := tui.NewApp(*profilesOnly, *profilesFile)
	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "sshnav: %v\n", err)
		os.Exit(1)
	}
}

// stripControls removes ASCII control characters from s to prevent newline
// injection when writing SSH config output.
func stripControls(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

func importSSHConfig(path string) {
	var profiles []config.Profile
	var err error
	if path != "" {
		profiles, err = config.LoadSSHConfigProfilesFrom(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sshnav: read %s: %v\n", path, err)
			os.Exit(1)
		}
	} else {
		profiles, err = config.LoadSSHConfigProfiles()
		if err != nil {
			fmt.Fprintf(os.Stderr, "sshnav: read ~/.ssh/config: %v\n", err)
			os.Exit(1)
		}
	}
	if len(profiles) == 0 {
		src := "~/.ssh/config"
		if path != "" {
			src = path
		}
		fmt.Fprintf(os.Stderr, "sshnav: no hosts found in %s\n", src)
		return
	}
	// Strip the SourceSSH marker — output is treated as app profiles
	for i := range profiles {
		profiles[i].Source = config.SourceApp
	}
	data, err := config.MarshalProfilesSpaced(profiles)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sshnav: marshal yaml: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(string(data))
}

func exportSSHConfig() {
	profiles, err := config.LoadAppProfiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sshnav: load profiles: %v\n", err)
		os.Exit(1)
	}
	if len(profiles) == 0 {
		fmt.Fprintln(os.Stderr, "sshnav: no app profiles found")
		return
	}

	var sb strings.Builder
	for i, p := range profiles {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString("Host " + stripControls(p.Name) + "\n")
		sb.WriteString("    HostName " + stripControls(p.Host) + "\n")
		if p.User != "" {
			sb.WriteString("    User " + stripControls(p.User) + "\n")
		}
		if p.Port != 0 && p.Port != 22 {
			sb.WriteString(fmt.Sprintf("    Port %d\n", p.Port))
		}
		if p.IdentityFile != "" {
			sb.WriteString("    IdentityFile " + stripControls(p.IdentityFile) + "\n")
		}
		if p.ProxyJump != "" {
			sb.WriteString("    ProxyJump " + stripControls(p.ProxyJump) + "\n")
		}
		for _, fwd := range p.LocalForwards {
			if fwd != "" {
				sb.WriteString("    LocalForward " + stripControls(fwd) + "\n")
			}
		}
		for _, fwd := range p.RemoteForwards {
			if fwd != "" {
				sb.WriteString("    RemoteForward " + stripControls(fwd) + "\n")
			}
		}
	}
	fmt.Print(sb.String())
}
