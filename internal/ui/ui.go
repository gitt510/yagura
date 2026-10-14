// Package ui renders the tables with bubbletea while collecting data on a
// fixed interval.
package ui

import (
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/gitt510/yagura/internal/discover"
	"github.com/gitt510/yagura/internal/gitinfo"
	"github.com/gitt510/yagura/internal/rows"
)

// Options is the startup configuration.
type Options struct {
	Repos       []discover.Repo
	TmuxSession string   // tmux session that o opens repos into, from config repos.tmux-session
	Roots       int      // Number of declared roots, shown in the bar
	Warnings    []string // discover warnings, kept in the footer
	NoFetch     bool
	Interval    time.Duration // Auto-refresh interval
	Color       bool
}

// Run starts the TUI and blocks until it exits.
func Run(opts Options) error {
	p := tea.NewProgram(newModel(opts), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

// concurrency is how many repos run in parallel during a single refresh.
const concurrency = 12

// sem keeps tea.Batch from spawning a goroutine per repo all at once.
var sem = make(chan struct{}, concurrency)

type repoResultMsg struct {
	gen      int
	index    int
	info     gitinfo.Info
	fetchErr bool
}

// tickMsg signals an auto-refresh.
type tickMsg struct{}

// pane is one screen's table plus the position within it, so stepping into a
// repo's branches and back does not lose the position.
type pane struct {
	tbl    table
	cursor int
	offset int
}

type model struct {
	opts Options
	th   theme

	repos pane
	// branches is the branch list of the repo opened with enter; open is that
	// repo's index, -1 while the repos table is shown
	branches pane
	open     int

	infos []gitinfo.Info

	// The last o (open in tmux) outcome, shown in the footer until the
	// next refresh
	openMsg string
	openErr bool
	// loaded records whether a refresh has completed at least once
	loaded bool

	failed []string // Repos whose fetch failed

	showHelp bool // Whether the help float is shown

	gen         int // Refresh generation, used to discard stale results
	refreshing  bool
	pending     int // Number of repos not yet reported
	lastRefresh time.Time
	spinner     spinner.Model

	width  int
	height int
}

func newModel(opts Options) *model {
	infos := make([]gitinfo.Info, len(opts.Repos))
	for i := range infos {
		infos[i] = rows.PendingInfo()
	}

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	m := &model{
		opts:    opts,
		th:      newTheme(opts.Color),
		infos:   infos,
		open:    -1,
		spinner: sp,
	}
	m.rebuild()
	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.startRefresh(), m.armTimer())
}

func (m *model) armTimer() tea.Cmd {
	return tea.Every(m.opts.Interval, func(time.Time) tea.Msg { return tickMsg{} })
}

// startRefresh re-collects every repo, unless a refresh is already running.
func (m *model) startRefresh() tea.Cmd {
	if m.refreshing {
		return nil
	}
	m.gen++
	m.refreshing = true
	m.openMsg, m.openErr = "", false

	if len(m.opts.Repos) == 0 {
		m.refreshing = false
		m.loaded = true
		return nil
	}
	m.pending = len(m.opts.Repos)
	m.failed = nil

	cmds := make([]tea.Cmd, 0, len(m.opts.Repos)+1)
	cmds = append(cmds, m.spinner.Tick)
	for i, r := range m.opts.Repos {
		cmds = append(cmds, collectCmd(m.gen, i, r.Path, m.opts.NoFetch))
	}
	return tea.Batch(cmds...)
}

// collectCmd handles a single repo. Results are applied in the order they
// arrive.
func collectCmd(gen, index int, path string, noFetch bool) tea.Cmd {
	return func() tea.Msg {
		sem <- struct{}{}
		defer func() { <-sem }()

		fetchErr := false
		if !noFetch {
			fetchErr = gitinfo.FetchRepo(path) != nil
		}
		info := gitinfo.Collect(path)
		info.FetchFailed = fetchErr
		return repoResultMsg{gen: gen, index: index, info: info, fetchErr: fetchErr}
	}
}

// openResultMsg reports how opening a repo in tmux went. err is empty on
// success.
type openResultMsg struct {
	repo    string
	session string
	err     string
}

// openCmd opens path as a new tmux window in session, creating the session
// when it does not exist. tmux itself is required only on this path.
func openCmd(session, path, name string) tea.Cmd {
	return func() tea.Msg {
		// = pins has-session to an exact name; without it, -t matches by prefix
		if exec.Command("tmux", "has-session", "-t", "="+session).Run() != nil {
			out, err := exec.Command("tmux", "new-session", "-d", "-s", session, "-c", path, "-n", name).CombinedOutput()
			return openResult(name, session, out, err)
		}
		out, err := exec.Command("tmux", "new-window", "-t", session+":", "-c", path, "-n", name).CombinedOutput()
		return openResult(name, session, out, err)
	}
}

func openResult(name, session string, out []byte, err error) openResultMsg {
	msg := openResultMsg{repo: name, session: session}
	if err != nil {
		msg.err = strings.TrimSpace(string(out))
		if msg.err == "" {
			msg.err = err.Error()
		}
	}
	return msg
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.rebuild()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		if m.showHelp {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.move(-1)
		case tea.MouseButtonWheelDown:
			m.move(1)
		}
		return m, nil

	case tickMsg:
		// startRefresh drops a tick that fires mid-refresh (never run refreshes
		// on top of each other)
		return m, tea.Batch(m.startRefresh(), m.armTimer())

	case repoResultMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.infos[msg.index] = msg.info
		if msg.fetchErr {
			m.failed = append(m.failed, m.opts.Repos[msg.index].Name)
		}
		m.pending--
		m.rebuild()
		if m.pending == 0 {
			m.finish()
		}
		return m, nil

	case openResultMsg:
		m.openMsg, m.openErr = "opened "+msg.repo+" in tmux session "+msg.session, false
		if msg.err != "" {
			m.openMsg, m.openErr = "tmux: "+msg.err, true
		}
		return m, nil

	case spinner.TickMsg:
		if !m.refreshing {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While help is open it behaves as a modal and ignores every key except the
	// ones that close it
	if m.showHelp {
		switch msg.String() {
		case "?", "q", "esc", "enter":
			m.showHelp = false
		case "ctrl+c":
			return m, tea.Quit
		}
		return m, nil
	}

	switch msg.String() {
	case "esc", "h", "backspace":
		// Inside a repo's branch list these step back to the repos table; on
		// the top screen esc keeps quitting
		if m.inBranches() {
			m.open = -1
			m.rebuild()
			return m, nil
		}
		if msg.String() == "esc" {
			return m, tea.Quit
		}
		return m, nil

	case "q", "ctrl+c":
		return m, tea.Quit

	case "?":
		m.showHelp = true
		return m, nil

	case "r":
		return m, m.startRefresh()

	case "enter", "l":
		m.openBranches()
		return m, nil

	case "o":
		return m, m.openRepo()

	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "ctrl+d", "pgdown":
		m.move(m.bodyHeight() / 2)
	case "ctrl+u", "pgup":
		m.move(-m.bodyHeight() / 2)
	case "g", "home":
		m.pane().cursor = 0
		m.snapCursor(1)
		m.clampView()
	case "G", "end":
		m.pane().cursor = len(m.pane().tbl.lines) - 1
		m.snapCursor(-1)
		m.clampView()
	}
	return m, nil
}

// inBranches reports whether a repo's branch list is on screen.
func (m *model) inBranches() bool { return m.open >= 0 }

// openBranches opens the focused repo's branch list.
func (m *model) openBranches() {
	if m.inBranches() {
		return
	}
	idx, ok := m.focusedRepo()
	if !ok {
		return
	}
	m.open = idx
	m.branches = pane{}
	m.rebuild()
}

// openRepo opens the focused repo (or the one whose branches are shown) in the
// declared tmux session: the path from the lookout to the work site.
func (m *model) openRepo() tea.Cmd {
	idx, ok := m.focusedRepo()
	if m.inBranches() {
		idx, ok = m.open, true
	}
	if !ok {
		return nil
	}
	if m.opts.TmuxSession == "" {
		m.openMsg, m.openErr = "set repos.tmux-session in the config to open repos in tmux", true
		return nil
	}
	r := m.opts.Repos[idx]
	return openCmd(m.opts.TmuxSession, r.Path, r.Base)
}

// focusedRepo resolves the cursor to the repo it sits on. Data rows are 1:1
// with repos, in declaration order.
func (m *model) focusedRepo() (int, bool) {
	p := &m.repos
	if p.cursor < 0 || p.cursor >= len(p.tbl.lines) {
		return 0, false
	}
	l := p.tbl.lines[p.cursor]
	if l.kind != lineRepo || l.ref < 0 || l.ref >= len(m.opts.Repos) {
		return 0, false
	}
	return l.ref, true
}

func (m *model) finish() {
	m.refreshing = false
	m.loaded = true
	m.lastRefresh = time.Now()
}

func (m *model) pane() *pane {
	if m.inBranches() {
		return &m.branches
	}
	return &m.repos
}

// rebuild rebuilds the tables. Not called on cursor movement (only when the
// data or the window changed).
func (m *model) rebuild() {
	rs := rows.Build(m.opts.Repos, m.infos)
	m.repos.tbl = buildTable(rs, m.th)
	if m.open >= 0 && m.open < len(rs) {
		m.branches.tbl = buildBranchTable(rs[m.open].Repo, rs[m.open], rows.Branches(m.infos[m.open]), m.th)
	}
	m.snapCursor(1)
	m.clampView()
}

// move walks the cursor over data lines only.
func (m *model) move(delta int) {
	if delta == 0 {
		return
	}
	p := m.pane()
	step := 1
	if delta < 0 {
		step, delta = -1, -delta
	}
	for ; delta > 0; delta-- {
		next := p.cursor
		for {
			next += step
			if next < 0 || next >= len(p.tbl.lines) {
				next = -1
				break
			}
			if p.tbl.lines[next].kind == lineRepo {
				break
			}
		}
		if next < 0 {
			break
		}
		p.cursor = next
	}
	m.clampView()
}

// snapCursor pulls a cursor sitting on a border or heading onto a data line.
func (m *model) snapCursor(step int) {
	p := m.pane()
	if len(p.tbl.lines) == 0 {
		p.cursor = 0
		return
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor >= len(p.tbl.lines) {
		p.cursor = len(p.tbl.lines) - 1
	}
	for i := p.cursor; i >= 0 && i < len(p.tbl.lines); i += step {
		if p.tbl.lines[i].kind == lineRepo {
			p.cursor = i
			return
		}
	}
	for i := p.cursor; i >= 0 && i < len(p.tbl.lines); i -= step {
		if p.tbl.lines[i].kind == lineRepo {
			p.cursor = i
			return
		}
	}
}

// bodyHeight is the number of lines available for the tables, excluding the
// footer (rule + notices + status bar). Column headings live
// inside each table's border.
func (m *model) bodyHeight() int {
	h := m.height - 2 - len(m.noticeLines())
	if h < 1 {
		h = 1
	}
	return h
}

// clampView moves the offset until the cursor is visible.
func (m *model) clampView() {
	// Before the window size arrives the line count is unknown; moving the offset
	// here would throw it off
	if m.height == 0 {
		return
	}
	p := m.pane()
	h := m.bodyHeight()
	if p.cursor < p.offset {
		// Scroll to the chrome (heading, rule) above the cursor line, not the
		// line itself, so the table's top border and headings stay in view
		top := p.cursor
		for top > 0 && p.tbl.lines[top-1].kind != lineRepo {
			top--
		}
		p.offset = top
	}
	if p.cursor >= p.offset+h {
		p.offset = p.cursor - h + 1
	}
	if last := len(p.tbl.lines) - h; p.offset > last {
		p.offset = last
	}
	if p.offset < 0 {
		p.offset = 0
	}
}

func (m *model) View() string {
	if m.width == 0 || m.height == 0 {
		return "loading…"
	}

	p := m.pane()
	h := m.bodyHeight()
	body := make([]string, 0, h)

	for i := 0; i < h; i++ {
		idx := p.offset + i
		if idx >= len(p.tbl.lines) {
			body = append(body, "")
			continue
		}
		body = append(body, p.tbl.renderLine(idx, idx == p.cursor, m.width))
	}

	if m.showHelp {
		m.overlayHelp(body, h)
	}

	out := body

	// Seat the footer on the board with a rule
	out = append(out, m.th.border.Render(strings.Repeat(barH, m.width)))
	out = append(out, m.noticeLines()...)
	out = append(out, m.statusBar())
	return strings.Join(out, "\n")
}

// helpRows lists the key bindings. It is the source of truth for the help
// float; the status bar hint is trimmed to the entry points (? and q) and
// points here.
var helpRows = []struct{ key, desc string }{
	{"j / k", "move"},
	{"wheel", "move"},
	{"g / G", "top / bottom"},
	{"ctrl+d / u", "half page"},
	{"enter / l", "show branches"},
	{"esc / h", "back to repos"},
	{"o", "open in tmux"},
	{"r", "refresh now"},
	{"?", "toggle help"},
	{"q / esc", "quit (esc: from repos)"},
}

// overlayHelp lays the help float over the center of the body.
func (m *model) overlayHelp(out []string, h int) {
	box := m.helpLines()
	bw := lipgloss.Width(box[0])
	if bw > m.width {
		return
	}
	x := (m.width - bw) / 2
	y := (h - len(box)) / 2
	if y < 0 {
		y = 0
	}
	for i, bl := range box {
		if r := y + i; r < h {
			out[r] = spliceLine(out[r], bl, x, bw)
		}
	}
}

func (m *model) helpLines() []string {
	keyW := 0
	for _, r := range helpRows {
		if w := lipgloss.Width(r.key); w > keyW {
			keyW = w
		}
	}
	lines := make([]string, 0, len(helpRows)+1)
	lines = append(lines, m.th.group.Render("keys"))
	for _, r := range helpRows {
		lines = append(lines, m.th.header.Render(pad(r.key, keyW, false))+"  "+r.desc)
	}
	return strings.Split(m.th.box.Render(strings.Join(lines, "\n")), "\n")
}

// spliceLine replaces w cells of base starting at x with box, for floats.
// If base is shorter than x, it is padded with spaces first.
func spliceLine(base, box string, x, w int) string {
	left := ansi.Truncate(base, x, "")
	if fill := x - ansi.StringWidth(left); fill > 0 {
		left += strings.Repeat(" ", fill)
	}
	return left + box + ansi.TruncateLeft(base, x+w, "")
}

// formatInterval drops the trailing zero units of Duration.String() (1m0s →
// 1m). To avoid cutting into the digits, the trim is kept only when the result
// ends in a unit.
func formatInterval(d time.Duration) string {
	s := d.String()
	for _, zero := range []string{"0s", "0m"} {
		t := strings.TrimSuffix(s, zero)
		if t != s && t != "" && !isDigit(t[len(t)-1]) {
			s = t
		}
	}
	return s
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// stateW is the width of the leading slot on each footer line. idle / warn /
// note all take the same width, so the │ separator after them lines up
// vertically.
const stateW = len(" idle")

// noticeLine turns a notice into a line with the same grammar as the bar
// (state slot + │ + text).
func (m *model) noticeLine(st lipgloss.Style, tag, s string) string {
	sep := m.th.hint.Render(" │ ")
	return clip(st.Render(pad(indent+tag, stateW, false))+sep+st.Render(s), m.width)
}

// noticeLines are the annotations above the status bar.
func (m *model) noticeLines() []string {
	var lines []string
	for _, w := range m.opts.Warnings {
		lines = append(lines, m.noticeLine(m.th.warnNote, "warn", w))
	}
	if len(m.failed) > 0 {
		lines = append(lines, m.noticeLine(m.th.warnNote, "warn",
			"fetch failed: "+strings.Join(m.failed, ", ")))
	}
	if m.openMsg != "" {
		st, tag := m.th.note, "note"
		if m.openErr {
			st, tag = m.th.warnNote, "warn"
		}
		lines = append(lines, m.noticeLine(st, tag, m.openMsg))
	}
	return lines
}

// cleanCount is how many repos are clean and in sync.
func (m *model) cleanCount() int {
	n := 0
	for _, r := range rows.Build(m.opts.Repos, m.infos) {
		if r.Clean {
			n++
		}
	}
	return n
}

func (m *model) statusBar() string {
	// While refreshing, show only the spinner where idle sits; the slot width is
	// fixed, so nothing to the left shifts
	state := m.th.bar.Render(pad(" idle", stateW, false))
	if m.refreshing {
		state = m.th.barOn.Render(pad(" "+m.spinner.View(), stateW, false))
	}

	last := "—"
	if !m.lastRefresh.IsZero() {
		last = m.lastRefresh.Format("15:04:05")
	}

	count := countLabel(len(m.opts.Repos), "repo") + " · " + countLabel(m.opts.Roots, "root")
	if m.loaded {
		count += " · " + strconv.Itoa(m.cleanCount()) + " clean"
	}

	sep := m.th.hint.Render(" │ ")
	left := state + sep +
		m.th.bar.Render("last "+last) + sep +
		m.th.bar.Render("every "+formatInterval(m.opts.Interval)) + sep +
		m.th.bar.Render(count)
	if m.opts.NoFetch {
		left += sep + m.th.bar.Render("no-fetch")
	}

	right := m.th.hint.Render("? help  q quit ")

	// Drop the key hint when there is not enough width; more readable than
	// showing a truncated string
	fill := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if fill < 2 {
		right = ""
		fill = m.width - lipgloss.Width(left)
	}
	if fill < 1 {
		fill = 1
	}
	return clip(left+strings.Repeat(" ", fill)+right, m.width)
}
