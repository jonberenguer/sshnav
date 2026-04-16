package tui

import "github.com/charmbracelet/lipgloss"

// Palette — dark terminal aesthetic with cyan accent
var (
	colorBg      = lipgloss.Color("235")
	colorSurface = lipgloss.Color("237")
	colorBorder  = lipgloss.Color("240")
	colorAccent  = lipgloss.Color("86")  // cyan-green
	colorMuted   = lipgloss.Color("244")
	colorText    = lipgloss.Color("252")
	colorError   = lipgloss.Color("203")
	colorWarn    = lipgloss.Color("214")
	colorSuccess = lipgloss.Color("76")

	// Source badge colours
	colorSourceApp = lipgloss.Color("111") // blue-ish
	colorSourceSSH = lipgloss.Color("180") // tan
)

var (
	StyleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent).
			PaddingLeft(1)

	StyleSubtitle = lipgloss.NewStyle().
			Foreground(colorMuted).
			PaddingLeft(1)

	StyleSelected = lipgloss.NewStyle().
			Background(colorSurface).
			Foreground(colorAccent).
			Bold(true).
			PaddingLeft(1).
			PaddingRight(1)

	StyleNormal = lipgloss.NewStyle().
			Foreground(colorText).
			PaddingLeft(1).
			PaddingRight(1)

	StyleMuted = lipgloss.NewStyle().
			Foreground(colorMuted).
			PaddingLeft(1)

	StyleError = lipgloss.NewStyle().
			Foreground(colorError).
			Bold(true).
			PaddingLeft(1)

	StyleSuccess = lipgloss.NewStyle().
			Foreground(colorSuccess).
			PaddingLeft(1)

	StyleWarn = lipgloss.NewStyle().
			Foreground(colorWarn).
			PaddingLeft(1)

	StyleBadgeApp = lipgloss.NewStyle().
			Foreground(colorSourceApp).
			Bold(true)

	StyleBadgeSSH = lipgloss.NewStyle().
			Foreground(colorSourceSSH).
			Bold(true)

	StylePanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	StylePanelActive = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorAccent).
				Padding(0, 1)

	StyleKey = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	StyleHelp = lipgloss.NewStyle().
			Foreground(colorMuted)
)

// StatusIcon returns a coloured indicator for mount/tunnel state.
func StatusIcon(active bool) string {
	if active {
		return lipgloss.NewStyle().Foreground(colorSuccess).Render("●")
	}
	return lipgloss.NewStyle().Foreground(colorMuted).Render("○")
}

// RightPanelWidth returns the width to allocate for a right-hand detail panel,
// or 0 if the terminal is too narrow.
func RightPanelWidth(totalWidth int) int {
	if totalWidth < 90 {
		return 0
	}
	w := totalWidth / 3
	if w < 32 {
		w = 32
	}
	if w > 48 {
		w = 48
	}
	return w
}

// RenderEmptyPanel renders a placeholder panel box with a rounded border so
// the list column width stays constant when there is no content to display.
func RenderEmptyPanel(width, height int) string {
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

// PageLayout pins footer to the bottom of the terminal. body fills the
// remaining height above it, padded/trimmed to an exact height so the footer
// is always at row m.height regardless of how much content body contains.
func PageLayout(width, height int, body, footer string) string {
	footerH := lipgloss.Height(footer)
	bodyH := height - footerH
	if bodyH < 1 {
		bodyH = 1
	}
	b := lipgloss.NewStyle().Width(width).Height(bodyH).Render(body)
	return lipgloss.JoinVertical(lipgloss.Left, b, footer)
}

// HelpLine renders a row of key=action pairs for the help bar.
func HelpLine(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		k := StyleKey.Render(pairs[i])
		v := StyleHelp.Render(pairs[i+1])
		parts = append(parts, k+" "+v)
	}
	sep := StyleHelp.Render("  ·  ")
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += sep
		}
		result += p
	}
	return result
}
