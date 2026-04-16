package tui

import (
	"fmt"
	"io"
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

// Title and Description return plain text used by the list for accessibility /
// search. Actual rendering is handled by profileDelegate.Render.
func (i profileItem) Title() string       { return i.profile.Name }
func (i profileItem) Description() string { return i.profile.Host }

// profileDelegate is a custom list.ItemDelegate. It renders each item from
// scratch so that pre-rendered ANSI codes never mix with the default
// delegate's filter-match highlighting (which corrupted escape sequences).
type profileDelegate struct{}

func (profileDelegate) Height() int                              { return 2 }
func (profileDelegate) Spacing() int                             { return 1 }
func (profileDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (profileDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	pi, ok := item.(profileItem)
	if !ok {
		return
	}
	selected := index == m.Index()

	prefix := "  "
	if selected {
		prefix = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("> ")
	}

	badge := "[app]"
	badgeSt := StyleBadgeApp
	if pi.profile.Source == config.SourceSSH {
		badge = "[ssh]"
		badgeSt = StyleBadgeSSH
	}

	nameFg := colorText
	nameBold := false
	if selected {
		nameFg = colorAccent
		nameBold = true
	}
	name := lipgloss.NewStyle().Foreground(nameFg).Bold(nameBold).Render(pi.profile.Name)

	var indicators []string
	if pi.profile.MountPoint != "" {
		indicators = append(indicators, StatusIcon(pi.mountStatus == sshutil.MountMounted)+" sshfs")
	}
	if pi.profile.ProxyJump != "" || pi.tunnelAlive {
		indicators = append(indicators, StatusIcon(pi.tunnelAlive)+" proxy")
	}
	ind := ""
	if len(indicators) > 0 {
		ind = "  " + lipgloss.NewStyle().Foreground(colorMuted).Render(strings.Join(indicators, "  "))
	}

	addr := pi.profile.Host
	if pi.profile.User != "" {
		addr = pi.profile.User + "@" + addr
	}
	if pi.profile.Port != 0 && pi.profile.Port != 22 {
		addr = fmt.Sprintf("%s:%d", addr, pi.profile.Port)
	}
	if pi.profile.ProxyJump != "" {
		addr += "  via " + pi.profile.ProxyJump
	}
	desc := lipgloss.NewStyle().Foreground(colorMuted).Render(addr)

	fmt.Fprintf(w, "%s%s %s%s\n   %s", prefix, badgeSt.Render(badge), name, ind, desc)
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
	app         *AppModel
	list        list.Model
	profiles    []config.Profile
	width       int
	height      int
	showSubmenu bool
	submenuSel  profileItem
	submenuIdx  int // cursor position within the submenu
}

// submenuEntry is one selectable item in the action submenu.
type submenuEntry struct{ key, label string }

// submenuEntries returns the ordered action list for the currently open
// submenu. Mount is only offered when the profile has SSHFS config.
func (m DashboardModel) submenuEntries() []submenuEntry {
	p := m.submenuSel.profile
	entries := []submenuEntry{{"s", "SSH Session"}}
	if p.MountPoint != "" || p.RemotePath != "" {
		entries = append(entries, submenuEntry{"m", "Mount SSHFS"})
	}
	entries = append(entries, submenuEntry{"t", "Tunnel / Proxy"})
	return entries
}

// execSubmenuKey executes a submenu action by its key letter and closes the
// submenu. Shared by direct-key and Enter-on-cursor paths.
func (m DashboardModel) execSubmenuKey(key string) (DashboardModel, tea.Cmd) {
	p := m.submenuSel.profile
	m.showSubmenu = false
	switch key {
	case "s":
		if p.Host != "" {
			cmd := sshutil.SessionCommand(p)
			return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
				if err != nil {
					return BannerMsg{"SSH: " + err.Error(), bannerError}
				}
				return BannerMsg{"Session closed.", bannerInfo}
			})
		}
	case "m":
		return m, func() tea.Msg { return SSHFSTargetMsg{p} }
	case "t":
		return m, func() tea.Msg { return ProxyTargetMsg{p} }
	}
	return m, nil
}

func NewDashboard(app *AppModel) DashboardModel {
	l := list.New(nil, profileDelegate{}, 0, 0)
	l.Title = "sshnav"
	l.Styles.Title = StyleTitle
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
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
		if m.list.FilterState() == list.Filtering {
			break
		}

		// Submenu is open — handle its keys only.
		if m.showSubmenu {
			entries := m.submenuEntries()
			switch msg.String() {
			case "esc", "q":
				m.showSubmenu = false
			case "up", "k":
				if m.submenuIdx > 0 {
					m.submenuIdx--
				}
			case "down", "j":
				if m.submenuIdx < len(entries)-1 {
					m.submenuIdx++
				}
			case "enter":
				return m.execSubmenuKey(entries[m.submenuIdx].key)
			case "s", "m", "t":
				return m.execSubmenuKey(msg.String())
			}
			return m, nil // block list navigation while submenu is open
		}

		// Normal dashboard keys.
		switch msg.String() {
		case "enter":
			if sel, ok := m.list.SelectedItem().(profileItem); ok {
				m.showSubmenu = true
				m.submenuSel = sel
				m.submenuIdx = 0
			}
		case "p":
			return m, func() tea.Msg { return NavigateMsg{ScreenProfileList} }
		case "n":
			return m, func() tea.Msg { return EditProfileMsg{nil} }
		case "e":
			sel, ok := m.list.SelectedItem().(profileItem)
			if ok && sel.profile.Source == config.SourceApp {
				p := sel.profile
				return m, func() tea.Msg { return EditProfileMsg{&p} }
			}
		}
	}

	var listCmd tea.Cmd
	m.list, listCmd = m.list.Update(msg)
	cmds = append(cmds, listCmd)
	return m, tea.Batch(cmds...)
}

// rightPanelWidth returns the width reserved for the right panel, or 0 if the
// terminal is too narrow to bother.
func (m DashboardModel) rightPanelWidth() int {
	if m.width < 90 {
		return 0
	}
	w := m.width / 3
	if w < 32 {
		w = 32
	}
	if w > 48 {
		w = 48
	}
	return w
}

// hasDetails reports whether a profile has any right-panel–worthy fields.
func hasDetails(p config.Profile) bool {
	return p.ProxyJump != "" ||
		p.MountPoint != "" || p.RemotePath != "" ||
		len(p.LocalForwards) > 0 || len(p.RemoteForwards) > 0
}

// renderDetails renders the profile detail panel (shown when no submenu is active).
func (m DashboardModel) renderDetails(p config.Profile, width, height int) string {
	sectionLabel := lipgloss.NewStyle().Foreground(colorMuted).Bold(true)
	value := lipgloss.NewStyle().Foreground(colorText)

	var lines []string

	if p.ProxyJump != "" {
		lines = append(lines, sectionLabel.Render("Proxy Jump"))
		for _, hop := range strings.Split(p.ProxyJump, ",") {
			lines = append(lines, "  "+value.Render(strings.TrimSpace(hop)))
		}
		lines = append(lines, "")
	}

	if p.MountPoint != "" || p.RemotePath != "" {
		lines = append(lines, sectionLabel.Render("SSHFS"))
		if p.RemotePath != "" {
			lines = append(lines, "  remote  "+value.Render(p.RemotePath))
		}
		if p.MountPoint != "" {
			lines = append(lines, "  mount   "+value.Render(p.MountPoint))
		}
		lines = append(lines, "")
	}

	if len(p.LocalForwards) > 0 {
		lines = append(lines, sectionLabel.Render("Local Forwards"))
		for _, fwd := range p.LocalForwards {
			lines = append(lines, "  "+value.Render(fwd))
		}
		lines = append(lines, "")
	}

	if len(p.RemoteForwards) > 0 {
		lines = append(lines, sectionLabel.Render("Remote Forwards"))
		for _, fwd := range p.RemoteForwards {
			lines = append(lines, "  "+value.Render(fwd))
		}
	}

	// Trim trailing blank line
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	innerW := width - 4  // border(2) + padding(2)
	innerH := height - 2 // border top+bottom
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

// renderSubmenu renders the action-picker that appears after Enter.
func (m DashboardModel) renderSubmenu(sel profileItem, width, height int) string {
	title := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(sel.profile.Name)

	entries := m.submenuEntries()
	var rows []string
	for i, e := range entries {
		cursor := "  "
		labelSt := lipgloss.NewStyle().Foreground(colorText)
		if i == m.submenuIdx {
			cursor = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("> ")
			labelSt = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		}
		rows = append(rows, cursor+StyleKey.Render(e.key)+"  "+labelSt.Render(e.label))
	}

	content := title + "\n\n" +
		strings.Join(rows, "\n") + "\n\n" +
		StyleMuted.Render("↑↓ / letter  ·  enter select  ·  esc close")

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
		BorderForeground(colorAccent).
		Padding(0, 1).
		Width(innerW).
		Height(innerH).
		Render(content)
}

// renderEmptyPanel renders an always-visible placeholder panel so the list
// width stays constant regardless of whether the selected profile has details
// or the list is in filter mode.
func (m DashboardModel) renderEmptyPanel(width, height int) string {
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
		Render("")
}

func (m DashboardModel) View() string {
	tunnelCount := len(m.app.activeTunnels)
	subtitle := fmt.Sprintf("  %d profile(s)  ·  %d active tunnel(s)", len(m.profiles), tunnelCount)

	help := HelpLine(
		"↑↓", "navigate",
		"enter", "open",
		"p", "profiles",
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

	contentH := m.height - lipgloss.Height(header) - lipgloss.Height(footer) - 2
	if contentH < 1 {
		contentH = 1
	}

	rpw := m.rightPanelWidth()

	if rpw > 0 {
		listW := m.width - rpw
		m.list.SetSize(listW, contentH)

		// In filter mode give the list full width — no panel to show.
		if m.list.FilterState() == list.Filtering {
			m.list.SetSize(m.width, contentH)
			return lipgloss.JoinVertical(lipgloss.Left, header, m.list.View(), footer)
		}

		var rightView string
		if m.showSubmenu {
			rightView = m.renderSubmenu(m.submenuSel, rpw, contentH)
		} else if sel, ok := m.list.SelectedItem().(profileItem); ok && hasDetails(sel.profile) {
			rightView = m.renderDetails(sel.profile, rpw, contentH)
		} else {
			rightView = m.renderEmptyPanel(rpw, contentH)
		}

		content := lipgloss.JoinHorizontal(lipgloss.Top, m.list.View(), rightView)
		return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
	}

	// Narrow terminal — full-width list.
	m.list.SetSize(m.width, contentH)
	return lipgloss.JoinVertical(lipgloss.Left, header, m.list.View(), footer)
}
