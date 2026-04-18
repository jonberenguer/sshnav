package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sshnav/config"
)

// ProfileListModel is a dedicated profile management screen.
type ProfileListModel struct {
	app      *AppModel
	list     list.Model
	profiles []config.Profile
	width    int
	height   int
	confirm  string // non-empty = showing delete confirmation for this profile name
}

func NewProfileList(app *AppModel) ProfileListModel {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = StyleSelected
	delegate.Styles.SelectedDesc = StyleMuted.Copy().PaddingLeft(1)
	delegate.Styles.NormalTitle = StyleNormal
	delegate.Styles.NormalDesc = StyleMuted

	l := list.New(nil, delegate, 0, 0)
	l.Title = "Profiles"
	l.Styles.Title = StyleTitle
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)

	return ProfileListModel{app: app, list: l}
}

func (m *ProfileListModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *ProfileListModel) refreshItems() {
	items := make([]list.Item, 0, len(m.profiles))
	for _, p := range m.profiles {
		items = append(items, profileItem{profile: p})
	}
	m.list.SetItems(items)
}

func (m ProfileListModel) Update(msg tea.Msg) (ProfileListModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case ProfilesLoadedMsg:
		m.profiles = msg.Profiles
		m.refreshItems()

	case tea.KeyMsg:
		// Handle delete confirmation overlay
		if m.confirm != "" {
			switch msg.String() {
			case "y", "Y":
				name := m.confirm
				m.confirm = ""
				return m, deleteProfileCmd(name, m.profiles)
			default:
				m.confirm = ""
			}
			return m, nil
		}

		switch msg.String() {
		case "esc", "q":
			return m, func() tea.Msg { return NavigateMsg{ScreenDashboard} }
		case "n":
			return m, func() tea.Msg { return EditProfileMsg{nil} }
		case "enter", "e":
			sel, ok := m.list.SelectedItem().(profileItem)
			if ok && sel.profile.Source == config.SourceApp {
				p := sel.profile
				return m, func() tea.Msg { return EditProfileMsg{&p} }
			}
		case "c":
			sel, ok := m.list.SelectedItem().(profileItem)
			if ok && sel.profile.Source == config.SourceApp {
				dup := sel.profile
				dup.Name = dup.Name + " (copy)"
				return m, func() tea.Msg { return EditProfileMsg{&dup} }
			}
		case "d", "delete":
			sel, ok := m.list.SelectedItem().(profileItem)
			if ok && sel.profile.Source == config.SourceApp {
				m.confirm = sel.profile.Name
				return m, nil
			}
		}
	}

	var listCmd tea.Cmd
	m.list, listCmd = m.list.Update(msg)
	cmds = append(cmds, listCmd)
	return m, tea.Batch(cmds...)
}

// renderProfileDetail renders a full profile detail panel for the profile list.
// It shows all populated fields: SSH basics, proxy jump, SSHFS, and forwards.
func (m ProfileListModel) renderProfileDetail(p config.Profile, width, height int) string {
	label := lipgloss.NewStyle().Foreground(colorMuted).Bold(true)
	value := lipgloss.NewStyle().Foreground(colorText)

	var lines []string

	// Header: name + source badge
	badge, badgeSt := "[app]", StyleBadgeApp
	if p.Source == config.SourceSSH {
		badge, badgeSt = "[ssh]", StyleBadgeSSH
	}
	lines = append(lines,
		badgeSt.Render(badge)+" "+lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(p.Name),
		"",
	)

	// SSH
	lines = append(lines, label.Render("SSH"))
	lines = append(lines, "  "+value.Render(addrStr(p)))
	if p.IdentityFile != "" {
		lines = append(lines, "  key  "+value.Render(p.IdentityFile))
	}
	lines = append(lines, "")

	// Proxy Jump
	if p.ProxyJump != "" {
		lines = append(lines, label.Render("Proxy Jump"))
		for _, hop := range strings.Split(p.ProxyJump, ",") {
			lines = append(lines, "  "+value.Render(strings.TrimSpace(hop)))
		}
		lines = append(lines, "")
	}

	// SSHFS
	if p.RemotePath != "" || p.MountPoint != "" {
		lines = append(lines, label.Render("SSHFS"))
		if p.RemotePath != "" {
			lines = append(lines, "  remote  "+value.Render(p.RemotePath))
		}
		if p.MountPoint != "" {
			lines = append(lines, "  mount   "+value.Render(p.MountPoint))
		}
		if p.SSHFSOpts != "" {
			lines = append(lines, "  opts    "+value.Render(p.SSHFSOpts))
		}
		lines = append(lines, "")
	}

	// Local Forwards
	if len(p.LocalForwards) > 0 {
		lines = append(lines, label.Render("Local Forwards"))
		for _, fwd := range p.LocalForwards {
			lines = append(lines, "  "+value.Render(fwd))
		}
		lines = append(lines, "")
	}

	// Remote Forwards
	if len(p.RemoteForwards) > 0 {
		lines = append(lines, label.Render("Remote Forwards"))
		for _, fwd := range p.RemoteForwards {
			lines = append(lines, "  "+value.Render(fwd))
		}
	}

	// Trim trailing blank lines
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	innerW := width - 4
	innerH := height - 2
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(0, 1).
		Width(innerW).
		Height(innerH).
		Render(strings.Join(lines, "\n"))
}

func (m ProfileListModel) View() string {
	help := HelpLine(
		"enter/e", "edit",
		"c", "duplicate",
		"n", "new",
		"d", "delete",
		"esc", "back",
	)
	footer := StyleHelp.Copy().PaddingLeft(1).Render(help)

	contentH := m.height - lipgloss.Height(footer)
	if contentH < 1 {
		contentH = 1
	}

	rpw := RightPanelWidth(m.width)

	if rpw > 0 {
		listW := m.width - rpw
		m.list.SetSize(listW, contentH)

		var rightView string
		if sel, ok := m.list.SelectedItem().(profileItem); ok {
			rightView = m.renderProfileDetail(sel.profile, rpw, contentH)
		} else {
			rightView = RenderEmptyPanel(rpw, contentH)
		}

		leftCol := lipgloss.NewStyle().Width(listW).Height(contentH).Render(m.list.View())
		rightCol := lipgloss.NewStyle().Width(rpw).Height(contentH).Render(rightView)

		body := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, rightCol)
		if m.confirm != "" {
			body += "\n" + StyleWarn.Render(fmt.Sprintf(
				"  Delete profile %q? [y/N]", m.confirm,
			))
		}
		return PageLayout(m.width, m.height, body, footer)
	}

	// Narrow terminal — full-width list.
	m.list.SetSize(m.width, contentH)
	body := m.list.View()
	if m.confirm != "" {
		body += "\n" + StyleWarn.Render(fmt.Sprintf(
			"  Delete profile %q? [y/N]", m.confirm,
		))
	}
	return PageLayout(m.width, m.height, body, footer)
}

// deleteProfileCmd removes a profile by name from the given list and saves.
func deleteProfileCmd(name string, profiles []config.Profile) tea.Cmd {
	return func() tea.Msg {
		filtered := make([]config.Profile, 0, len(profiles))
		for _, p := range profiles {
			if !(p.Source == config.SourceApp && p.Name == name) {
				filtered = append(filtered, p)
			}
		}
		err := config.SaveAppProfiles(filtered)
		return ProfilesSavedMsg{Err: err}
	}
}
