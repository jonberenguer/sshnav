package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sshnav/config"
	"sshnav/sshutil"
)

type ProxyModel struct {
	app     *AppModel
	profile config.Profile
	loading bool
	width   int
	height  int
}

func NewProxy(app *AppModel) ProxyModel {
	return ProxyModel{app: app}
}

func (m *ProxyModel) SetSize(w, h int) { m.width = w; m.height = h }

func (m *ProxyModel) SetProfile(p config.Profile) {
	m.profile = p
	m.loading = false
}

func (m ProxyModel) activeTunnel() *ActiveTunnel {
	for i := range m.app.activeTunnels {
		t := &m.app.activeTunnels[i]
		if t.Profile.Name == m.profile.Name && t.Session.IsRunning() {
			return t
		}
	}
	return nil
}

func (m ProxyModel) Update(msg tea.Msg) (ProxyModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q":
			return m, func() tea.Msg { return NavigateMsg{ScreenDashboard} }
		case "t":
			if m.loading {
				return m, nil
			}
			at := m.activeTunnel()
			if at != nil {
				// Close existing tunnel
				_ = at.Session.Close()
				m.app.activeTunnels = filterTunnels(m.app.activeTunnels, at.Session)
				return m, func() tea.Msg { return BannerMsg{"Tunnel closed.", bannerInfo} }
			}
			if m.profile.Host == "" {
				return m, func() tea.Msg { return BannerMsg{"No host configured.", bannerError} }
			}
			m.loading = true
			sess, ch := sshutil.OpenTunnel(m.profile)
			m.app.activeTunnels = append(m.app.activeTunnels, ActiveTunnel{
				Profile: m.profile,
				Session: sess,
			})
			return m, waitTunnelResult(ch)
		case "e":
			if m.profile.Source == config.SourceApp {
				p := m.profile
				return m, func() tea.Msg { return EditProfileMsg{&p} }
			}
		}
	}
	return m, nil
}

func (m ProxyModel) View() string {
	p := m.profile
	at := m.activeTunnel()
	alive := at != nil

	tunnelLabel := "Open Tunnel"
	if alive {
		tunnelLabel = "Close Tunnel"
	}
	if m.loading && !alive {
		tunnelLabel = "Connecting…"
	}

	statusIcon := StatusIcon(alive)
	statusText := StyleMuted.Render("tunnel inactive")
	if alive {
		statusText = StyleSuccess.Render("tunnel active")
	}

	// Render the jump-chain visually
	jumpChain := ""
	if p.ProxyJump != "" {
		hops := strings.Split(p.ProxyJump, ",")
		parts := make([]string, 0, len(hops)+1)
		for _, h := range hops {
			parts = append(parts, StyleWarn.Render(strings.TrimSpace(h)))
		}
		parts = append(parts, StyleAccentText(p.Host))
		jumpChain = "\n" + StyleMuted.Render("  chain: ") + strings.Join(parts, StyleMuted.Render(" → "))
	}

	rows := []string{
		fmtRow("Host", addrStr(p)),
		fmtRow("Proxy Jump", orDash(p.ProxyJump)),
		fmtRow("Identity", orDash(p.IdentityFile)),
	}
	for i, fwd := range p.LocalForwards {
		rows = append(rows, fmtRow(fmt.Sprintf("Local  %-2d", i+1), fwd))
	}
	for i, fwd := range p.RemoteForwards {
		rows = append(rows, fmtRow(fmt.Sprintf("Remote %-2d", i+1), fwd))
	}

	box := StylePanel.Render(strings.Join(rows, "\n") + jumpChain)

	actionStyle := lipgloss.NewStyle().
		Foreground(colorBg).
		Background(colorAccent).
		Bold(true).
		Padding(0, 2)
	if alive {
		actionStyle = actionStyle.Background(colorError)
	}
	if m.loading && !alive {
		actionStyle = actionStyle.Background(colorMuted)
	}

	help := HelpLine(
		"t", tunnelLabel,
		"e", "edit profile",
		"esc", "back",
	)

	return lipgloss.JoinVertical(lipgloss.Left,
		StyleTitle.Render("⬡ SSH Proxy  "+p.Name),
		StyleSubtitle.Render("  "+statusIcon+" "+statusText),
		"",
		box,
		"",
		"  "+actionStyle.Render(" t → "+tunnelLabel+" "),
		"",
		StyleHelp.Copy().PaddingLeft(1).Render(help),
	)
}

// StyleAccentText applies accent colour to a string without a full style struct.
func StyleAccentText(s string) string {
	return lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(s)
}

// waitTunnelResult converts a tunnel result channel into a tea.Cmd.
func waitTunnelResult(ch <-chan sshutil.TunnelResult) tea.Cmd {
	return func() tea.Msg {
		return TunnelResultMsg{<-ch}
	}
}

func filterTunnels(tunnels []ActiveTunnel, sess *sshutil.TunnelSession) []ActiveTunnel {
	out := tunnels[:0]
	for _, t := range tunnels {
		if t.Session != sess {
			out = append(out, t)
		}
	}
	return out
}
