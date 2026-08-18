// Package ui renders the tables with bubbletea while collecting data on a
// fixed interval.
package ui

import (
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/gitt510/yagura/internal/discover"
	"github.com/gitt510/yagura/internal/gitinfo"
	"github.com/gitt510/yagura/internal/render"
	"github.com/gitt510/yagura/internal/rows"
)

// Options is the startup configuration.
type Options struct {
	Repos        []discover.Repo
	TmuxSession  string   // tmux session that enter opens repos into, from config repos.tmux-session
	Commands     []string // Process names to watch, from config sessions.commands
	Roots        int      // Number of declared roots, shown in the bar
	Warnings     []string // discover warnings, kept in the repos view footer
	ReposNote    string   // Hint when there are no repos (no root declared / query mismatch)
	NoFetch      bool
	WithSessions bool // Start on the sessions view (repos by default)
	// The auto-refresh interval is per view; procs involves no fetch, so it can
	// run faster
	SessionsInterval time.Duration
	ReposInterval    time.Duration
	Color            bool
}

// Run starts the TUI and blocks until it exits.
func Run(opts Options) error {
	p := tea.NewProgram(newModel(opts), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// concurrency is how many repos run in parallel during a single refresh.
const concurrency = 12

// sem keeps tea.Batch from spawning a goroutine per repo all at once.
var sem = make(chan struct{}, concurrency)

// view is the screen being displayed. repos is the default entry point, and
// the tabs are ordered the same way.
type view int

const (
	viewRepos view = iota
	viewSessions
	viewCount
)

type repoResultMsg struct {
	gen      int
	index    int
	info     gitinfo.Info
	fetchErr bool
}

// branchesResultMsg carries one repo's other local branches. It has no
// generation: the newest list simply replaces the previous one.
type branchesResultMsg struct {
	index int
	list  []gitinfo.BranchInfo
}

// tickMsg signals an auto-refresh. gen is the armTimer generation, so a
// leftover timer that fires after a view switch changed the interval can be
// discarded.
type tickMsg struct{ gen int }

// pane is one screen's table plus the position within it, so moving back and
// forth between views does not lose the position.
type pane struct {
	tbl    table
	cursor int
	offset int
}

type model struct {
	opts Options
	th   theme

	view     view
	sessions pane
	repos    pane

	infos    []gitinfo.Info
	procList []render.SessionRow

	// branchMode is the repos view's second display mode: one row per local
	// branch instead of one per repo. branches doubles as the loaded marker:
	// a key that is present but empty means "collected, none". reposRefs maps
	// each repos-table data row back to its repo index
	branchMode bool
	branches   map[int][]gitinfo.BranchInfo
	reposRefs  []int

	// The last enter (open in tmux) outcome, shown in the footer until the
	// next refresh
	openMsg string
	openErr bool
	// loaded records whether each view has collected at least once, so switching
	// to a view never leaves it empty
	loaded [viewCount]bool

	failed []string // repos side: repos whose fetch failed

	showHelp bool // Whether the help float is shown

	// Refresh state is per view, so a slow repos fetch never holds up the
	// sessions collection
	gens        [viewCount]int // Refresh generation, used to discard stale results
	refreshing  [viewCount]bool
	timerGen    int // Timer generation, discards the old timer replaced on a view switch
	pending     int // repos side: number of repos not yet reported
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
		opts:     opts,
		th:       newTheme(opts.Color),
		infos:    infos,
		branches: map[int][]gitinfo.BranchInfo{},
		spinner:  sp,
	}
	if opts.WithSessions {
		m.view = viewSessions
	}
	m.rebuild()
	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.startRefresh(), m.armTimer())
}

func (m *model) armTimer() tea.Cmd {
	m.timerGen++
	gen := m.timerGen
	return tea.Every(m.interval(), func(time.Time) tea.Msg { return tickMsg{gen: gen} })
}

// interval is the auto-refresh interval of the current view.
func (m *model) interval() time.Duration {
	if m.view == viewSessions {
		return m.opts.SessionsInterval
	}
	return m.opts.ReposInterval
}

// startRefresh re-collects only for the view being shown; the background view
// is left alone.
func (m *model) startRefresh() tea.Cmd {
	v := m.view
	if m.refreshing[v] {
		return nil
	}
	m.gens[v]++
	m.refreshing[v] = true
	m.openMsg, m.openErr = "", false

	if v == viewSessions {
		return tea.Batch(m.spinner.Tick, sessionsCmd(m.gens[v], m.opts.Commands))
	}

	if len(m.opts.Repos) == 0 {
		m.refreshing[v] = false
		m.loaded[viewRepos] = true
		return nil
	}
	m.pending = len(m.opts.Repos)
	m.failed = nil

	cmds := make([]tea.Cmd, 0, 2*len(m.opts.Repos)+1)
	cmds = append(cmds, m.spinner.Tick)
	for i, r := range m.opts.Repos {
		cmds = append(cmds, collectCmd(m.gens[v], i, r.Path, m.opts.NoFetch))
	}
	// Branch rows ride the same refresh, so they never go stale on screen
	if m.branchMode {
		for i, r := range m.opts.Repos {
			cmds = append(cmds, branchesCmd(i, r.Path))
		}
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

// branchesCmd collects one repo's other local branches. Fast enough to run on
// demand: it reads refs only and never fetches.
func branchesCmd(index int, path string) tea.Cmd {
	return func() tea.Msg {
		sem <- struct{}{}
		defer func() { <-sem }()
		return branchesResultMsg{index: index, list: gitinfo.Branches(path)}
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.rebuild()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tickMsg:
		// Drop ticks from an old timer generation and ticks that fire mid-refresh
		// (never run refreshes on top of each other)
		if msg.gen != m.timerGen {
			return m, nil
		}
		return m, tea.Batch(m.startRefresh(), m.armTimer())

	case repoResultMsg:
		if msg.gen != m.gens[viewRepos] {
			return m, nil
		}
		m.infos[msg.index] = msg.info
		if msg.fetchErr {
			m.failed = append(m.failed, m.opts.Repos[msg.index].Name)
		}
		m.pending--
		m.rebuild()
		if m.pending == 0 {
			m.finish(viewRepos)
		}
		return m, nil

	case branchesResultMsg:
		m.branches[msg.index] = msg.list
		m.rebuild()
		return m, nil

	case openResultMsg:
		m.openMsg, m.openErr = "opened "+msg.repo+" in tmux session "+msg.session, false
		if msg.err != "" {
			m.openMsg, m.openErr = "tmux: "+msg.err, true
		}
		return m, nil

	case sessionsResultMsg:
		if msg.gen != m.gens[viewSessions] {
			return m, nil
		}
		m.procList = msg.list
		m.finish(viewSessions)
		m.rebuild()
		return m, nil

	case spinner.TickMsg:
		if !m.refreshing[viewRepos] && !m.refreshing[viewSessions] {
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
	case "q", "ctrl+c", "esc":
		return m, tea.Quit

	case "?":
		m.showHelp = true
		return m, nil

	case "r":
		return m, m.startRefresh()

	case "tab":
		return m, m.toggleBranchMode()

	case "enter":
		return m, m.openRepo()

	case "p":
		m.view = (m.view + 1) % viewCount
		m.rebuild()
		// Switch the timer to the new view's interval, and collect right away for a
		// view visited for the first time
		cmds := []tea.Cmd{m.armTimer()}
		if !m.loaded[m.view] {
			cmds = append(cmds, m.startRefresh())
		}
		return m, tea.Batch(cmds...)

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

// openRepo opens the focused repo in the declared tmux session: the path from
// the lookout to the work site.
func (m *model) openRepo() tea.Cmd {
	if m.view != viewRepos {
		return nil
	}
	idx, ok := m.focusedRepo()
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

// focusedRepo resolves the cursor to the repo it sits on. Focusable rows are
// 1:1 with repos in both display modes.
func (m *model) focusedRepo() (int, bool) {
	p := &m.repos
	if p.cursor < 0 || p.cursor >= len(p.tbl.lines) {
		return 0, false
	}
	l := p.tbl.lines[p.cursor]
	if l.kind != lineRepo || l.ref < 0 || l.ref >= len(m.reposRefs) {
		return 0, false
	}
	return m.reposRefs[l.ref], true
}

// toggleBranchMode switches the repos view between one row per repo and one
// row per branch. The first switch collects the branches; after that the rows
// reuse what the refresh keeps up to date.
func (m *model) toggleBranchMode() tea.Cmd {
	if m.view != viewRepos {
		return nil
	}
	m.branchMode = !m.branchMode
	m.rebuild()
	if !m.branchMode {
		return nil
	}
	var cmds []tea.Cmd
	for i, r := range m.opts.Repos {
		if _, loaded := m.branches[i]; !loaded {
			cmds = append(cmds, branchesCmd(i, r.Path))
		}
	}
	return tea.Batch(cmds...)
}

func (m *model) finish(v view) {
	m.refreshing[v] = false
	m.loaded[v] = true
	m.lastRefresh = time.Now()
}

func (m *model) pane() *pane {
	if m.view == viewRepos {
		return &m.repos
	}
	return &m.sessions
}

// rebuild rebuilds the tables. Not called on cursor movement (only when the
// data or the window changed).
func (m *model) rebuild() {
	m.sessions.tbl = buildSessionTable(m.procList, m.th)
	rs := rows.Build(m.opts.Repos, m.infos)
	if m.branchMode {
		rs = rows.BranchView(m.opts.Repos, m.infos, m.branches)
	}
	// Focusable (non-Sub) rows are 1:1 with repos in declaration order, so
	// counting them recovers each row's repo index
	m.reposRefs = make([]int, len(rs))
	idx := -1
	for i, r := range rs {
		if !r.Sub {
			idx++
		}
		m.reposRefs[i] = idx
	}
	m.repos.tbl = buildTable(rs, m.th)
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
// header and the footer (rule + notices + status bar). Column headings live
// inside each table's border.
func (m *model) bodyHeight() int {
	h := m.height - len(m.headerLines()) - 2 - len(m.noticeLines())
	if h < 1 {
		h = 1
	}
	return h
}

// logoLines is the wordmark, generated once with toilet (pagga) and stripped
// of its shading. Not worth a runtime-generation library, so it is baked in as
// a constant.
var logoLines = []string{
	" █ █ █▀█ █▀▀ █ █ █▀▄ █▀█",
	"  █  █▀█ █ █ █ █ █▀▄ █▀█",
	"  ▀  ▀ ▀ ▀▀▀ ▀▀▀ ▀ ▀ ▀ ▀",
}

// gradientLine splits line into as many equal parts as the ramp has steps and
// paints them from left to right.
func gradientLine(line string, ramp []lipgloss.Style) string {
	runes := []rune(line)
	per := (len(runes) + len(ramp) - 1) / len(ramp)
	var b strings.Builder
	for i, st := range ramp {
		lo := min(i*per, len(runes))
		hi := min(lo+per, len(runes))
		b.WriteString(st.Render(string(runes[lo:hi])))
	}
	return b.String()
}

// headerLines lays out the tab line and the logo, seating them on the board
// with a rule. The tabs are bottom-aligned to the logo's last line. The logo is
// decoration, so on a terminal too narrow for it, it quietly disappears and
// only the tab line remains.
func (m *model) headerLines() []string {
	tabs := m.tabLine()
	rule := m.th.border.Render(strings.Repeat(barH, m.width))

	logoW := lipgloss.Width(logoLines[0])
	if m.width < lipgloss.Width(tabs)+logoW+4 {
		return []string{clip(tabs, m.width), rule}
	}

	lines := make([]string, 0, len(logoLines)+1)
	for i, l := range logoLines {
		left := ""
		if i == len(logoLines)-1 {
			left = tabs
		}
		fill := m.width - lipgloss.Width(left) - lipgloss.Width(l) - 1
		lines = append(lines, left+strings.Repeat(" ", fill)+gradientLine(l, m.th.logoRamp))
	}
	return append(lines, rule)
}

// tabLine lists the views (fixed feature names). The current one is bold, the
// rest are dim. Which command a session belongs to is carried per row by the
// table's CMD column.
func (m *model) tabLine() string {
	repos := "repos"
	sessions := "sessions"
	if m.view == viewSessions {
		sessions = m.th.group.Render(sessions)
		repos = m.th.dim.Render(repos)
	} else {
		sessions = m.th.dim.Render(sessions)
		repos = m.th.group.Render(repos)
	}
	return indent + repos + m.th.dim.Render(" │ ") + sessions
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
		p.offset = p.cursor
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

	out := append(m.headerLines(), body...)

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
	{"g / G", "top / bottom"},
	{"ctrl+d / u", "half page"},
	{"tab", "toggle branches (repos)"},
	{"enter", "open in tmux (repos)"},
	{"p", "switch view"},
	{"r", "refresh now"},
	{"?", "toggle help"},
	{"q / esc", "quit"},
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

// noticeLines are the per-view annotations. Root-related warnings appear only
// in the repos view, so the sessions view stays quiet even without a root.
func (m *model) noticeLines() []string {
	var lines []string

	if m.view == viewSessions {
		if m.loaded[viewSessions] && len(m.procList) == 0 {
			lines = append(lines, m.noticeLine(m.th.note, "note", "no sessions running"))
		}
		return lines
	}

	for _, w := range m.opts.Warnings {
		lines = append(lines, m.noticeLine(m.th.warnNote, "warn", w))
	}
	if len(m.opts.Repos) == 0 && m.opts.ReposNote != "" {
		lines = append(lines, m.noticeLine(m.th.note, "note", m.opts.ReposNote))
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

func (m *model) statusBar() string {
	// While refreshing, show only the spinner where idle sits; the slot width is
	// fixed, so nothing to the left shifts
	state := m.th.bar.Render(pad(" idle", stateW, false))
	if m.refreshing[m.view] {
		state = m.th.barOn.Render(pad(" "+m.spinner.View(), stateW, false))
	}

	last := "—"
	if !m.lastRefresh.IsZero() {
		last = m.lastRefresh.Format("15:04:05")
	}

	// No segment for the view name; the unit of the count (session / repo) says
	// which view this is
	count := countLabel(len(m.procList), "session")
	if m.view == viewRepos {
		count = countLabel(len(m.opts.Repos), "repo") + " · " + countLabel(m.opts.Roots, "root")
	}

	sep := m.th.hint.Render(" │ ")
	left := state + sep +
		m.th.bar.Render("last "+last) + sep +
		m.th.bar.Render("every "+formatInterval(m.interval())) + sep +
		m.th.bar.Render(count)
	if m.view == viewRepos && m.branchMode {
		left += sep + m.th.bar.Render("branches")
	}
	if m.view == viewRepos && m.opts.NoFetch {
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
