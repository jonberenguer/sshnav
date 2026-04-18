package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"sshnav/config"
	"sshnav/sshutil"
)

// Screen identifies which view is active.
type Screen int

const (
	ScreenDashboard Screen = iota
	ScreenProfileList
	ScreenProfileEdit
	ScreenSSHFS
	ScreenProxy
)

// ActiveTunnel tracks a live SSH tunnel session.
type ActiveTunnel struct {
	Profile config.Profile
	Session *sshutil.TunnelSession
}

// AppModel is the root Bubbletea model that owns all sub-models.
type AppModel struct {
	screen Screen
	width  int
	height int

	// Sub-models (only the active one is updated/drawn)
	dashboard   DashboardModel
	profileList ProfileListModel
	profileEdit ProfileEditModel
	sshfsPanel  SSHFSModel
	proxyPanel  ProxyModel

	// Shared mutable state
	profiles      []config.Profile
	activeTunnels []ActiveTunnel

	// Error/status banner (transient)
	banner     string
	bannerType bannerKind

	// Options
	profilesOnly bool // when true, ~/.ssh/config is not loaded
}

type bannerKind int

const (
	bannerInfo bannerKind = iota
	bannerSuccess
	bannerError
)

// ---- Messages ----

// ProfilesLoadedMsg is sent after reloading profiles from disk.
type ProfilesLoadedMsg struct{ Profiles []config.Profile }

// ProfilesSavedMsg is sent after writing app profiles.
type ProfilesSavedMsg struct{ Err error }

// MountResultMsg wraps an sshutil.MountResult.
type MountResultMsg struct{ Result sshutil.MountResult }

// TunnelResultMsg wraps an sshutil.TunnelResult.
type TunnelResultMsg struct{ Result sshutil.TunnelResult }

// NavigateMsg switches the active screen.
type NavigateMsg struct{ To Screen }

// BannerMsg sets the transient status banner.
type BannerMsg struct {
	Text string
	Kind bannerKind
}

// EditProfileMsg carries a profile into the edit screen (nil = new profile).
type EditProfileMsg struct{ Profile *config.Profile }

// SSHFSTargetMsg selects a profile for the SSHFS panel.
type SSHFSTargetMsg struct{ Profile config.Profile }

// ProxyTargetMsg selects a profile for the proxy panel.
type ProxyTargetMsg struct{ Profile config.Profile }

// TunnelStartedMsg is sent after the tunnel grace period confirms SSH is running.
type TunnelStartedMsg struct {
	Profile config.Profile
	Session *sshutil.TunnelSession
	Err     error
}

// TunnelClosedMsg is sent when a tunnel process exits.
type TunnelClosedMsg struct{ Profile config.Profile }

// ---- Init / Update / View ----

func NewApp(profilesOnly bool) AppModel {
	m := AppModel{screen: ScreenDashboard, profilesOnly: profilesOnly}
	m.dashboard = NewDashboard(&m)
	m.profileList = NewProfileList(&m)
	m.profileEdit = NewProfileEdit(&m)
	m.sshfsPanel = NewSSHFS(&m)
	m.proxyPanel = NewProxy(&m)
	return m
}

func (m AppModel) Init() tea.Cmd {
	return tea.Batch(m.loadProfilesCmd(), tickCmd())
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Propagate to sub-models
		m.dashboard.SetSize(m.width, m.height)
		m.profileList.SetSize(m.width, m.height)
		m.profileEdit.SetSize(m.width, m.height)
		m.sshfsPanel.SetSize(m.width, m.height)
		m.proxyPanel.SetSize(m.width, m.height)

	case ProfilesLoadedMsg:
		m.profiles = msg.Profiles
		m.dashboard.profiles = m.profiles
		m.dashboard.list.SetItems(m.dashboard.buildItems())
		m.profileList.profiles = m.profiles
		m.profileList.refreshItems()

	case ProfilesSavedMsg:
		if msg.Err != nil {
			m.banner = "Save failed: " + msg.Err.Error()
			m.bannerType = bannerError
		} else {
			m.banner = "Profile saved."
			m.bannerType = bannerSuccess
			m.screen = ScreenProfileList
			cmds = append(cmds, m.loadProfilesCmd())
		}

	case MountResultMsg:
		if msg.Result.Err != nil {
			m.banner = msg.Result.Err.Error()
			m.bannerType = bannerError
		} else {
			m.banner = "Mounted " + msg.Result.Profile.MountPoint
			m.bannerType = bannerSuccess
		}
		m.sshfsPanel.loading = false

	case TunnelStartedMsg:
		if msg.Err != nil {
			m.banner = "Tunnel: " + msg.Err.Error()
			m.bannerType = bannerError
		} else {
			// Sub-models share state via m.proxyPanel.app (heap AppModel),
			// NOT via m.activeTunnels (bubbletea-stored copy). Write there so
			// activeTunnel() / buildItems() see the new entry immediately.
			m.proxyPanel.app.activeTunnels = append(m.proxyPanel.app.activeTunnels, ActiveTunnel{
				Profile: msg.Profile,
				Session: msg.Session,
			})
			m.activeTunnels = m.proxyPanel.app.activeTunnels // keep bubbletea copy in sync
			m.banner = "Tunnel connected."
			m.bannerType = bannerSuccess
		}
		m.proxyPanel.loading = false

	case TunnelResultMsg:
		r := msg.Result
		// Only report an error for unexpected disconnects (not startup failures,
		// which are already reported via TunnelStartedMsg, and not user-closed
		// tunnels that were already removed from activeTunnels).
		wasActive := m.hasTunnel(r.Session)
		m.removeTunnel(r.Session)
		m.proxyPanel.loading = false
		if !r.EarlyExit && r.Err != nil && wasActive {
			m.banner = "Tunnel disconnected: " + r.Err.Error()
			m.bannerType = bannerError
		}

	case tickMsg:
		m.dashboard.list.SetItems(m.dashboard.buildItems())
		cmds = append(cmds, tickCmd())

	case BannerMsg:
		m.banner = msg.Text
		m.bannerType = msg.Kind

	case NavigateMsg:
		m.screen = msg.To
		m.banner = ""

	case EditProfileMsg:
		m.profileEdit = NewProfileEdit(&m)
		m.profileEdit.SetSize(m.width, m.height)
		if msg.Profile != nil {
			m.profileEdit.LoadProfile(*msg.Profile)
		}
		m.screen = ScreenProfileEdit

	case SSHFSTargetMsg:
		m.sshfsPanel.SetProfile(msg.Profile)
		m.screen = ScreenSSHFS
		cmds = append(cmds, mountPollCmd())

	case ProxyTargetMsg:
		m.proxyPanel.SetProfile(msg.Profile)
		m.screen = ScreenProxy

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			// Close all tunnels before quitting
			for _, t := range m.activeTunnels {
				_ = t.Session.Close()
			}
			return m, tea.Quit
		}
	}

	// Route remaining input to active sub-model
	var subCmd tea.Cmd
	switch m.screen {
	case ScreenDashboard:
		m.dashboard, subCmd = m.dashboard.Update(msg)
	case ScreenProfileList:
		m.profileList, subCmd = m.profileList.Update(msg)
	case ScreenProfileEdit:
		m.profileEdit, subCmd = m.profileEdit.Update(msg)
	case ScreenSSHFS:
		m.sshfsPanel, subCmd = m.sshfsPanel.Update(msg)
	case ScreenProxy:
		m.proxyPanel, subCmd = m.proxyPanel.Update(msg)
	}
	cmds = append(cmds, subCmd)
	return m, tea.Batch(cmds...)
}

func (m AppModel) View() string {
	var body string
	switch m.screen {
	case ScreenDashboard:
		body = m.dashboard.View()
	case ScreenProfileList:
		body = m.profileList.View()
	case ScreenProfileEdit:
		body = m.profileEdit.View()
	case ScreenSSHFS:
		body = m.sshfsPanel.View()
	case ScreenProxy:
		body = m.proxyPanel.View()
	}

	if m.banner != "" {
		var style = StyleSuccess
		switch m.bannerType {
		case bannerError:
			style = StyleError
		case bannerInfo:
			style = StyleMuted
		}
		body = body + "\n" + style.Render("  "+m.banner)
	}
	return body
}

// ---- helpers ----

func (m *AppModel) hasTunnel(sess *sshutil.TunnelSession) bool {
	// Read from the shared heap AppModel (what sub-models actually see).
	for _, t := range m.proxyPanel.app.activeTunnels {
		if t.Session == sess {
			return true
		}
	}
	return false
}

func (m *AppModel) removeTunnel(sess *sshutil.TunnelSession) {
	// Update the shared heap AppModel so sub-models see the removal immediately.
	app := m.proxyPanel.app
	filtered := make([]ActiveTunnel, 0, len(app.activeTunnels))
	for _, t := range app.activeTunnels {
		if t.Session != sess {
			filtered = append(filtered, t)
		}
	}
	app.activeTunnels = filtered
	m.activeTunnels = filtered // keep bubbletea copy in sync
}

func (m AppModel) loadProfilesCmd() tea.Cmd {
	profilesOnly := m.profilesOnly
	return func() tea.Msg {
		var profiles []config.Profile
		var err error
		if profilesOnly {
			profiles, err = config.LoadAppProfiles()
		} else {
			profiles, err = config.LoadAllProfiles()
		}
		if err != nil {
			return BannerMsg{"Load profiles: " + err.Error(), bannerError}
		}
		return ProfilesLoadedMsg{profiles}
	}
}
