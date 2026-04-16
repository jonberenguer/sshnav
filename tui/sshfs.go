package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sshnav/config"
	"sshnav/sshutil"
)

type SSHFSModel struct {
	app     *AppModel
	profile config.Profile
	loading bool
	width   int
	height  int
}

func NewSSHFS(app *AppModel) SSHFSModel {
	return SSHFSModel{app: app}
}

func (m *SSHFSModel) SetSize(w, h int) { m.width = w; m.height = h }

func (m *SSHFSModel) SetProfile(p config.Profile) {
	m.profile = p
	m.loading = false
}

func (m SSHFSModel) isMounted() bool {
	if m.profile.MountPoint == "" {
		return false
	}
	return sshutil.CheckMount(m.profile.MountPoint) == sshutil.MountMounted
}

func (m SSHFSModel) Update(msg tea.Msg) (SSHFSModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q":
			return m, func() tea.Msg { return NavigateMsg{ScreenDashboard} }
		case "m":
			if m.loading {
				return m, nil
			}
			if m.isMounted() {
				m.loading = true
				ch := sshutil.Unmount(m.profile)
				return m, waitMountResult(ch)
			}
			if m.profile.MountPoint == "" || m.profile.RemotePath == "" {
				return m, func() tea.Msg {
					return BannerMsg{"Set remote_path and mount_point in the profile first.", bannerError}
				}
			}
			m.loading = true
			ch := sshutil.Mount(m.profile)
			return m, waitMountResult(ch)
		case "e":
			if m.profile.Source == config.SourceApp {
				p := m.profile
				return m, func() tea.Msg { return EditProfileMsg{&p} }
			}
		}
	}
	return m, nil
}

func (m SSHFSModel) View() string {
	p := m.profile
	mounted := m.isMounted()
	mountLabel := "Mount"
	if mounted {
		mountLabel = "Unmount"
	}
	if m.loading {
		mountLabel = "Working…"
	}

	// Profile summary box
	rows := []string{
		fmtRow("Host", addrStr(p)),
		fmtRow("Remote Path", orDash(p.RemotePath)),
		fmtRow("Mount Point", orDash(p.MountPoint)),
		fmtRow("SSHFS Opts", orDash(p.SSHFSOpts)),
		fmtRow("Proxy Jump", orDash(p.ProxyJump)),
		fmtRow("Identity", orDash(p.IdentityFile)),
	}

	statusIcon := StatusIcon(mounted)
	statusText := StyleMuted.Render("not mounted")
	if mounted {
		statusText = StyleSuccess.Render("mounted at " + p.MountPoint)
	}

	box := StylePanel.Render(strings.Join(rows, "\n"))

	actionStyle := lipgloss.NewStyle().
		Foreground(colorBg).
		Background(colorAccent).
		Bold(true).
		Padding(0, 2)
	if mounted {
		actionStyle = actionStyle.Background(colorError)
	}
	if m.loading {
		actionStyle = actionStyle.Background(colorMuted)
	}

	help := HelpLine(
		"m", mountLabel,
		"e", "edit profile",
		"esc", "back",
	)

	return lipgloss.JoinVertical(lipgloss.Left,
		StyleTitle.Render("⬡ SSHFS  "+p.Name),
		StyleSubtitle.Render("  "+statusIcon+" "+statusText),
		"",
		box,
		"",
		"  "+actionStyle.Render(" m → "+mountLabel+" "),
		"",
		StyleHelp.Copy().PaddingLeft(1).Render(help),
	)
}

// waitMountResult converts a mount result channel into a tea.Cmd.
func waitMountResult(ch <-chan sshutil.MountResult) tea.Cmd {
	return func() tea.Msg {
		return MountResultMsg{<-ch}
	}
}

func fmtRow(label, val string) string {
	l := lipgloss.NewStyle().Foreground(colorMuted).Width(14).Render(label)
	v := lipgloss.NewStyle().Foreground(colorText).Render(val)
	return fmt.Sprintf("%s  %s", l, v)
}

func addrStr(p config.Profile) string {
	s := p.Host
	if p.User != "" {
		s = p.User + "@" + s
	}
	if p.Port != 0 && p.Port != 22 {
		s = fmt.Sprintf("%s:%d", s, p.Port)
	}
	return s
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
