package tui

import (
	"fmt"

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
				return m, deleteProfileCmd(m.app, name)
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

func (m ProfileListModel) View() string {
	listHeight := m.height - 6
	if listHeight < 1 {
		listHeight = 1
	}
	m.list.SetSize(m.width, listHeight)

	help := HelpLine(
		"enter/e", "edit",
		"n", "new",
		"d", "delete",
		"esc", "back",
	)

	body := lipgloss.JoinVertical(lipgloss.Left,
		m.list.View(),
		StyleHelp.Copy().PaddingLeft(1).Render(help),
	)

	if m.confirm != "" {
		overlay := StyleWarn.Render(fmt.Sprintf(
			"  Delete profile %q? [y/N]", m.confirm,
		))
		body = body + "\n" + overlay
	}
	return body
}

// deleteProfileCmd removes a profile by name from app-managed profiles and saves.
func deleteProfileCmd(app *AppModel, name string) tea.Cmd {
	return func() tea.Msg {
		filtered := make([]config.Profile, 0, len(app.profiles))
		for _, p := range app.profiles {
			if !(p.Source == config.SourceApp && p.Name == name) {
				filtered = append(filtered, p)
			}
		}
		err := config.SaveAppProfiles(filtered)
		return ProfilesSavedMsg{Err: err}
	}
}
