package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sshnav/config"
)

// field indices for the static section
const (
	fieldName = iota
	fieldHost
	fieldUser
	fieldPort
	fieldIdentity
	fieldRemotePath
	fieldMountPoint
	fieldSSHFSOpts
	fieldProxyJump
	fieldCount
)

var fieldLabels = [fieldCount]string{
	"Name         ",
	"Host         ",
	"User         ",
	"Port         ",
	"Identity File",
	"Remote Path  ",
	"Mount Point  ",
	"SSHFS Opts   ",
	"Proxy Jump   ",
}

var fieldPlaceholders = [fieldCount]string{
	"my-server",
	"192.168.1.10 or hostname",
	"deploy (leave blank for default)",
	"22",
	"~/.ssh/id_ed25519 (optional)",
	"/home/user (for sshfs)",
	"/mnt/my-server (for sshfs)",
	"reconnect,cache=no (optional)",
	"jump.host.com (optional)",
}

type ProfileEditModel struct {
	app        *AppModel
	inputs     [fieldCount]textinput.Model
	localFwds  []textinput.Model
	remoteFwds []textinput.Model
	focused    int
	isNew      bool
	origName   string
	width      int
	height     int
}

func (m *ProfileEditModel) SetSize(w, h int) { m.width = w; m.height = h }

func newForwardInput(placeholder string) textinput.Model {
	t := textinput.New()
	t.Placeholder = placeholder
	t.CharLimit = 256
	return t
}

func (m *ProfileEditModel) totalFields() int {
	return fieldCount + len(m.localFwds) + len(m.remoteFwds)
}

// focusedInput returns a pointer to whichever textinput currently has focus.
func (m *ProfileEditModel) focusedInput() *textinput.Model {
	if m.focused < fieldCount {
		return &m.inputs[m.focused]
	}
	localIdx := m.focused - fieldCount
	if localIdx < len(m.localFwds) {
		return &m.localFwds[localIdx]
	}
	remoteIdx := localIdx - len(m.localFwds)
	if remoteIdx < len(m.remoteFwds) {
		return &m.remoteFwds[remoteIdx]
	}
	return nil
}

func (m *ProfileEditModel) setFocus(idx int) {
	if cur := m.focusedInput(); cur != nil {
		cur.Blur()
	}
	m.focused = idx
	if next := m.focusedInput(); next != nil {
		next.Focus()
	}
}

func NewProfileEdit(app *AppModel) ProfileEditModel {
	m := ProfileEditModel{app: app, isNew: true}
	for i := 0; i < fieldCount; i++ {
		t := textinput.New()
		t.Placeholder = fieldPlaceholders[i]
		t.CharLimit = 256
		m.inputs[i] = t
	}
	m.inputs[fieldName].Focus()
	m.focused = fieldName
	return m
}

// LoadProfile populates the form from an existing profile for editing.
func (m *ProfileEditModel) LoadProfile(p config.Profile) {
	m.isNew = false
	m.origName = p.Name
	m.inputs[fieldName].SetValue(p.Name)
	m.inputs[fieldHost].SetValue(p.Host)
	m.inputs[fieldUser].SetValue(p.User)
	if p.Port != 0 {
		m.inputs[fieldPort].SetValue(strconv.Itoa(p.Port))
	}
	m.inputs[fieldIdentity].SetValue(p.IdentityFile)
	m.inputs[fieldRemotePath].SetValue(p.RemotePath)
	m.inputs[fieldMountPoint].SetValue(p.MountPoint)
	m.inputs[fieldSSHFSOpts].SetValue(p.SSHFSOpts)
	m.inputs[fieldProxyJump].SetValue(p.ProxyJump)

	m.localFwds = nil
	for _, fwd := range p.LocalForwards {
		t := newForwardInput("8080:localhost:80")
		t.SetValue(fwd)
		m.localFwds = append(m.localFwds, t)
	}
	m.remoteFwds = nil
	for _, fwd := range p.RemoteForwards {
		t := newForwardInput("2222:localhost:22")
		t.SetValue(fwd)
		m.remoteFwds = append(m.remoteFwds, t)
	}
}

func (m ProfileEditModel) validate() (config.Profile, error) {
	name := strings.TrimSpace(m.inputs[fieldName].Value())
	host := strings.TrimSpace(m.inputs[fieldHost].Value())
	if name == "" {
		return config.Profile{}, fmt.Errorf("name is required")
	}
	if host == "" {
		return config.Profile{}, fmt.Errorf("host is required")
	}

	var port int
	if raw := strings.TrimSpace(m.inputs[fieldPort].Value()); raw != "" {
		p, err := strconv.Atoi(raw)
		if err != nil || p < 1 || p > 65535 {
			return config.Profile{}, fmt.Errorf("port must be 1-65535")
		}
		port = p
	}

	var localFwds []string
	for _, t := range m.localFwds {
		if v := strings.TrimSpace(t.Value()); v != "" {
			localFwds = append(localFwds, v)
		}
	}
	var remoteFwds []string
	for _, t := range m.remoteFwds {
		if v := strings.TrimSpace(t.Value()); v != "" {
			remoteFwds = append(remoteFwds, v)
		}
	}

	return config.Profile{
		Name:           name,
		Host:           host,
		User:           strings.TrimSpace(m.inputs[fieldUser].Value()),
		Port:           port,
		IdentityFile:   strings.TrimSpace(m.inputs[fieldIdentity].Value()),
		RemotePath:     strings.TrimSpace(m.inputs[fieldRemotePath].Value()),
		MountPoint:     strings.TrimSpace(m.inputs[fieldMountPoint].Value()),
		SSHFSOpts:      strings.TrimSpace(m.inputs[fieldSSHFSOpts].Value()),
		ProxyJump:      strings.TrimSpace(m.inputs[fieldProxyJump].Value()),
		LocalForwards:  localFwds,
		RemoteForwards: remoteFwds,
		Source:         config.SourceApp,
	}, nil
}

func (m ProfileEditModel) Update(msg tea.Msg) (ProfileEditModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return NavigateMsg{ScreenProfileList} }
		case "tab", "down":
			m.setFocus((m.focused + 1) % m.totalFields())
			return m, nil
		case "shift+tab", "up":
			m.setFocus((m.focused - 1 + m.totalFields()) % m.totalFields())
			return m, nil
		case "enter":
			m.setFocus((m.focused + 1) % m.totalFields())
			return m, nil
		case "ctrl+s":
			p, err := m.validate()
			if err != nil {
				return m, func() tea.Msg { return BannerMsg{err.Error(), bannerError} }
			}
			return m, saveProfileCmd(m.app, p, m.origName)
		case "ctrl+l":
			t := newForwardInput("8080:localhost:80")
			m.localFwds = append(m.localFwds, t)
			m.setFocus(fieldCount + len(m.localFwds) - 1)
			return m, nil
		case "ctrl+r":
			t := newForwardInput("2222:localhost:22")
			m.remoteFwds = append(m.remoteFwds, t)
			m.setFocus(fieldCount + len(m.localFwds) + len(m.remoteFwds) - 1)
			return m, nil
		case "ctrl+x":
			if m.focused >= fieldCount {
				localIdx := m.focused - fieldCount
				if cur := m.focusedInput(); cur != nil {
					cur.Blur()
				}
				if localIdx < len(m.localFwds) {
					m.localFwds = append(m.localFwds[:localIdx], m.localFwds[localIdx+1:]...)
				} else {
					remoteIdx := localIdx - len(m.localFwds)
					if remoteIdx < len(m.remoteFwds) {
						m.remoteFwds = append(m.remoteFwds[:remoteIdx], m.remoteFwds[remoteIdx+1:]...)
					}
				}
				if m.focused >= m.totalFields() {
					m.focused = m.totalFields() - 1
				}
				if next := m.focusedInput(); next != nil {
					next.Focus()
				}
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	if m.focused < fieldCount {
		m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
	} else {
		localIdx := m.focused - fieldCount
		if localIdx < len(m.localFwds) {
			m.localFwds[localIdx], cmd = m.localFwds[localIdx].Update(msg)
		} else {
			remoteIdx := localIdx - len(m.localFwds)
			if remoteIdx < len(m.remoteFwds) {
				m.remoteFwds[remoteIdx], cmd = m.remoteFwds[remoteIdx].Update(msg)
			}
		}
	}
	return m, cmd
}

func (m ProfileEditModel) View() string {
	title := "New Profile"
	if !m.isNew {
		title = "Edit Profile: " + m.origName
	}

	var sb strings.Builder
	sb.WriteString(StyleTitle.Render("⬡ " + title))
	sb.WriteString("\n\n")

	for i := 0; i < fieldCount; i++ {
		label := fieldLabels[i]
		var labelStyle lipgloss.Style
		if i == m.focused {
			labelStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		} else {
			labelStyle = lipgloss.NewStyle().Foreground(colorMuted)
		}
		if i == fieldRemotePath {
			sb.WriteString(StyleMuted.Render("  ─── SSHFS ────────────────────────────────") + "\n")
		}
		if i == fieldProxyJump {
			sb.WriteString(StyleMuted.Render("  ─── SSH Proxy ────────────────────────────") + "\n")
		}
		sb.WriteString(fmt.Sprintf("  %s  %s\n", labelStyle.Render(label), m.inputs[i].View()))
	}

	// Local forwards
	sb.WriteString(StyleMuted.Render("  ─── Local Forwards  ctrl+l=add ───────────") + "\n")
	for i, t := range m.localFwds {
		focusIdx := fieldCount + i
		var labelStyle lipgloss.Style
		if m.focused == focusIdx {
			labelStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		} else {
			labelStyle = lipgloss.NewStyle().Foreground(colorMuted)
		}
		label := fmt.Sprintf("Local  %-2d    ", i+1)
		hint := StyleMuted.Render(" ctrl+x")
		sb.WriteString(fmt.Sprintf("  %s  %s%s\n", labelStyle.Render(label), t.View(), hint))
	}
	if len(m.localFwds) == 0 {
		sb.WriteString(StyleMuted.Render("  (none — ctrl+l to add)") + "\n")
	}

	// Remote forwards
	sb.WriteString(StyleMuted.Render("  ─── Remote Forwards  ctrl+r=add ──────────") + "\n")
	for i, t := range m.remoteFwds {
		focusIdx := fieldCount + len(m.localFwds) + i
		var labelStyle lipgloss.Style
		if m.focused == focusIdx {
			labelStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		} else {
			labelStyle = lipgloss.NewStyle().Foreground(colorMuted)
		}
		label := fmt.Sprintf("Remote %-2d    ", i+1)
		hint := StyleMuted.Render(" ctrl+x")
		sb.WriteString(fmt.Sprintf("  %s  %s%s\n", labelStyle.Render(label), t.View(), hint))
	}
	if len(m.remoteFwds) == 0 {
		sb.WriteString(StyleMuted.Render("  (none — ctrl+r to add)") + "\n")
	}

	help := HelpLine(
		"tab/↓", "next",
		"shift+tab/↑", "prev",
		"ctrl+l", "add local fwd",
		"ctrl+r", "add remote fwd",
		"ctrl+x", "remove fwd",
		"ctrl+s", "save",
		"esc", "cancel",
	)
	footer := StyleHelp.Copy().PaddingLeft(1).Render(help)

	return PageLayout(m.width, m.height, sb.String(), footer)
}

// saveProfileCmd upserts the profile and persists to disk.
func saveProfileCmd(app *AppModel, newProfile config.Profile, origName string) tea.Cmd {
	return func() tea.Msg {
		updated := make([]config.Profile, 0, len(app.profiles)+1)
		replaced := false
		for _, p := range app.profiles {
			if p.Source != config.SourceApp {
				continue
			}
			if p.Name == origName {
				updated = append(updated, newProfile)
				replaced = true
			} else {
				updated = append(updated, p)
			}
		}
		if !replaced {
			updated = append(updated, newProfile)
		}
		err := config.SaveAppProfiles(updated)
		return ProfilesSavedMsg{Err: err}
	}
}
