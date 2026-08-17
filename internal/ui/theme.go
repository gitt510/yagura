package ui

import "github.com/charmbracelet/lipgloss"

// The meaning of the colors matches the plain output.
// yellow = things you have to act on (changed / ahead) and fully open sessions / cyan = drift from remote (informational)
// green = running session / red = detached / dim = 0, -, and idle
// Every color is an ANSI-16 slot number only (so it follows the terminal
// theme). The footer gets no band (background) — ANSI slot 0 is not the same as
// the terminal's default background and drifts from it depending on the theme.
// The cursor line's background uses "0": with "8" it would share a slot with
// colDim's foreground and dim text would dissolve into it.
const (
	colDim      = "8"
	colLocal    = "3"
	colRemote   = "6"
	colOK       = "2"
	colNG       = "1"
	colCursorBG = "0"
	colBarFG    = "7"
)

type theme struct {
	color bool

	dim     lipgloss.Style
	local   lipgloss.Style
	danger  lipgloss.Style // Inconsistency that already happened (unpushed commits, etc.)
	remote  lipgloss.Style
	headOn  lipgloss.Style // Branch other than the default
	headOff lipgloss.Style // detached

	header     lipgloss.Style
	border     lipgloss.Style
	group      lipgloss.Style
	groupCount lipgloss.Style

	box lipgloss.Style // Border of floats such as help
	// logoRamp is the wordmark's stepped gradient (painted left to right). Pure
	// decoration. A smooth gradient would need truecolor, so it is approximated
	// with a brightness staircase of ANSI-16 slots
	logoRamp []lipgloss.Style

	bar      lipgloss.Style
	barOn    lipgloss.Style
	hint     lipgloss.Style
	warn     lipgloss.Style // For table cells (x and such); no background
	warnNote lipgloss.Style // The warn line in the footer
	note     lipgloss.Style
}

func newTheme(color bool) theme {
	plain := lipgloss.NewStyle()
	th := theme{
		color:      color,
		dim:        plain,
		local:      plain,
		danger:     plain,
		remote:     plain,
		headOn:     plain,
		headOff:    plain,
		header:     plain.Bold(true),
		border:     plain,
		group:      plain.Bold(true),
		groupCount: plain,
		box:        plain.Border(lipgloss.RoundedBorder()).Padding(0, 2),
		logoRamp:   []lipgloss.Style{plain.Bold(true)},
		bar:        plain,
		barOn:      plain.Bold(true),
		hint:       plain,
		warn:       plain,
		warnNote:   plain,
		note:       plain,
	}
	if !color {
		return th
	}

	th.dim = plain.Foreground(lipgloss.Color(colDim))
	// Color tokens for out-of-normal values carry bold as well, doubling up the
	// "deviation from normal" signal that the color conveys (0 stays plain and
	// silent)
	th.local = plain.Foreground(lipgloss.Color(colLocal)).Bold(true)
	th.danger = plain.Foreground(lipgloss.Color(colNG)).Bold(true)
	th.remote = plain.Foreground(lipgloss.Color(colRemote)).Bold(true)
	// Branch names stand out in the brand primary (the same blue as the logo).
	// The functional colors (yellow/cyan/green/red) are already taken by meanings
	th.headOn = plain.Foreground(lipgloss.Color("12")).Bold(true)
	th.headOff = plain.Foreground(lipgloss.Color(colNG)).Bold(true)

	th.border = th.dim
	th.box = th.box.BorderForeground(lipgloss.Color(colDim))
	// blue → bright blue → cyan → bright cyan; all ANSI-16 slots
	th.logoRamp = []lipgloss.Style{
		plain.Foreground(lipgloss.Color("4")).Bold(true),
		plain.Foreground(lipgloss.Color("12")).Bold(true),
		plain.Foreground(lipgloss.Color("6")).Bold(true),
		plain.Foreground(lipgloss.Color("14")).Bold(true),
	}

	th.bar = plain.Foreground(lipgloss.Color(colBarFG))
	// The spinner during a refresh is not "needs action" but activity = an active
	// state, so it uses primary
	th.barOn = th.bar.Bold(true).Foreground(lipgloss.Color("12"))
	th.hint = th.bar.Foreground(lipgloss.Color(colDim))
	th.warn = plain.Foreground(lipgloss.Color(colLocal)).Bold(true)
	th.warnNote = th.bar.Foreground(lipgloss.Color(colLocal))
	th.note = th.hint
	return th
}

// selected adds the background for the cursor line. On a terminal without
// color it falls back to reverse video.
func (t theme) selected(st lipgloss.Style) lipgloss.Style {
	if !t.color {
		return st.Reverse(true)
	}
	return st.Background(lipgloss.Color(colCursorBG))
}
