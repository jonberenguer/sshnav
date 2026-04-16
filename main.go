package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"gopkg.in/yaml.v3"
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
			importSSHConfig()
			return
		}
	}

	fs := flag.NewFlagSet("sshnav", flag.ExitOnError)
	profilesOnly := fs.Bool("profiles-only", false, "only load profiles from profiles.yaml, ignore ~/.ssh/config")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: sshnav [--profiles-only]\n")
		fmt.Fprintf(os.Stderr, "       sshnav export-ssh-config\n")
		fmt.Fprintf(os.Stderr, "       sshnav import-ssh-config\n")
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

	app := tui.NewApp(*profilesOnly)
	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "sshnav: %v\n", err)
		os.Exit(1)
	}
}

func importSSHConfig() {
	profiles, err := config.LoadSSHConfigProfiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sshnav: read ~/.ssh/config: %v\n", err)
		os.Exit(1)
	}
	if len(profiles) == 0 {
		fmt.Fprintln(os.Stderr, "sshnav: no hosts found in ~/.ssh/config")
		return
	}
	// Strip the SourceSSH marker — these will be treated as app profiles in the output
	for i := range profiles {
		profiles[i].Source = config.SourceApp
	}
	data, err := yaml.Marshal(profiles)
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
		sb.WriteString("Host " + p.Name + "\n")
		sb.WriteString("    HostName " + p.Host + "\n")
		if p.User != "" {
			sb.WriteString("    User " + p.User + "\n")
		}
		if p.Port != 0 && p.Port != 22 {
			sb.WriteString(fmt.Sprintf("    Port %d\n", p.Port))
		}
		if p.IdentityFile != "" {
			sb.WriteString("    IdentityFile " + p.IdentityFile + "\n")
		}
		if p.ProxyJump != "" {
			sb.WriteString("    ProxyJump " + p.ProxyJump + "\n")
		}
		for _, fwd := range p.LocalForwards {
			if fwd != "" {
				sb.WriteString("    LocalForward " + fwd + "\n")
			}
		}
		for _, fwd := range p.RemoteForwards {
			if fwd != "" {
				sb.WriteString("    RemoteForward " + fwd + "\n")
			}
		}
	}
	fmt.Print(sb.String())
}
