package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sshnav/config"
	"sshnav/sshutil"
)

// ---- list item ----

type profileItem struct {
	profile     config.Profile
	mountStatus sshutil.MountStatus
	tunnelAlive bool
}

func (i profileItem) FilterValue() string { return i.profile.Name + " " + i.profile.Host }
func (i profileItem) Title() string {
	badge := StyleBadgeApp.Render("[app]")
	if i.profile.Source == config.SourceSSH {
		badge = StyleBadgeSSH.Render("[ssh]")
	}
	name := StyleNormal.Render(i.profile.Name)

	var indicators []string
	if i.profile.MountPoint != "" {
		indicators = append(indicators, StatusIcon(i.mountStatus == sshutil.MountMounted)+" sshfs")
	}
	if i.profile.ProxyJump != "" || i.tunnelAlive {
		indicators = append(indicators, StatusIcon(i.tunnelAlive)+" proxy")
	}
	ind := ""
	if len(indicators) > 0 {
		ind = "  " + StyleMuted.Render(strings.Join(indicators, "  "))
	}
	return badge + " " + name + ind
}
func (i profileItem) Description() string {
	addr := i.profile.Host
	if i.profile.User != "" {
		addr = i.profile.User + "@" + addr
	}
	if i.profile.Port != 0 && i.profile.Port != 22 {
		addr = fmt.Sprintf("%s:%d", addr, i.profile.Port)
	}
	if i.profile.ProxyJump != "" {
		addr += StyleMuted.Render("  via "+i.profile.ProxyJump)
	}
	return StyleMuted.Render("  " + addr)
}

// ---- tick for status polling ----

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// ---- DashboardModel ----

type DashboardModel struct {
	app      *AppModel
	list     list.Model
	profiles []config.Profile
	width    int
	height   int
}

func NewDashboard(app *AppModel) DashboardModel {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = StyleSelected
	delegate.Styles.SelectedDesc = StyleMuted.Copy().PaddingLeft(1)
	delegate.Styles.NormalTitle = StyleNormal
	delegate.Styles.NormalDesc = StyleMuted

	l := list.New(nil, delegate, 0, 0)
	l.Title = "sshnav"
	l.Styles.Title = StyleTitle
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)

	return DashboardModel{app: app, list: l}
}

func (m *DashboardModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.list.SetSize(w, h-3)
}

func (m DashboardModel) buildItems() []list.Item {
	items := make([]list.Item, 0, len(m.profiles))
	for _, p := range m.profiles {
		item := profileItem{profile: p}
		if p.MountPoint != "" {
			item.mountStatus = sshutil.CheckMount(p.MountPoint)
		}
		for _, t := range m.app.activeTunnels {
			if t.Profile.Name == p.Name && t.Session.IsRunning() {
				item.tunnelAlive = true
				break
			}
		}
		items = append(items, item)
	}
	return items
}

func (m DashboardModel) Init() tea.Cmd {
	return tickCmd()
}

func (m DashboardModel) Update(msg tea.Msg) (DashboardModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case ProfilesLoadedMsg:
		m.profiles = msg.Profiles
		m.list.SetItems(m.buildItems())

	case tickMsg:
		m.list.SetItems(m.buildItems())
		cmds = append(cmds, tickCmd())

	case tea.KeyMsg:
		// Don't intercept when list is filtering
		if m.list.FilterState() == list.Filtering {
			break
		}
		switch msg.String() {
		case "enter":
			sel, ok := m.list.SelectedItem().(profileItem)
			if ok {
				// Go to SSHFS panel if the profile has mount config, else proxy
				if sel.profile.MountPoint != "" || sel.profile.RemotePath != "" {
					return m, func() tea.Msg { return SSHFSTargetMsg{sel.profile} }
				}
				return m, func() tea.Msg { return ProxyTargetMsg{sel.profile} }
			}
		case "m":
			sel, ok := m.list.SelectedItem().(profileItem)
			if ok {
				return m, func() tea.Msg { return SSHFSTargetMsg{sel.profile} }
			}
		case "p":
			sel, ok := m.list.SelectedItem().(profileItem)
			if ok {
				return m, func() tea.Msg { return ProxyTargetMsg{sel.profile} }
			}
		case "e":
			sel, ok := m.list.SelectedItem().(profileItem)
			if ok && sel.profile.Source == config.SourceApp {
				p := sel.profile
				return m, func() tea.Msg { return EditProfileMsg{&p} }
			}
		case "n":
			return m, func() tea.Msg { return EditProfileMsg{nil} }
		case "l":
			return m, func() tea.Msg { return NavigateMsg{ScreenProfileList} }
		}
	}

	var listCmd tea.Cmd
	m.list, listCmd = m.list.Update(msg)
	cmds = append(cmds, listCmd)
	return m, tea.Batch(cmds...)
}

func (m DashboardModel) View() string {
	tunnelCount := len(m.app.activeTunnels)
	subtitle := fmt.Sprintf("  %d profile(s)  ·  %d active tunnel(s)", len(m.profiles), tunnelCount)

	help := HelpLine(
		"↑↓", "navigate",
		"enter", "open",
		"m", "sshfs",
		"p", "proxy",
		"n", "new",
		"e", "edit",
		"/", "filter",
		"ctrl+c", "quit",
	)

	header := lipgloss.JoinVertical(lipgloss.Left,
		StyleTitle.Render("⬡ sshnav"),
		StyleSubtitle.Render(subtitle),
	)

	footer := StyleHelp.Copy().PaddingLeft(1).Render(help)

	// Stack: header + list + footer
	listHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer) - 2
	if listHeight < 1 {
		listHeight = 1
	}
	m.list.SetSize(m.width, listHeight)

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		m.list.View(),
		footer,
	)
}
