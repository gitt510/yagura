// Package ui は bubbletea で表を出しつつ、一定間隔で収集を回す。
package ui

import (
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

// Options は起動時の設定。
type Options struct {
	Repos        []discover.Repo
	Commands     []string // 監視する process 名。config の sessions.commands 由来
	Roots        int      // 宣言された root の数。bar に出す
	Warnings     []string // discover の警告。repos view の footer に出し続ける
	ReposNote    string   // repos が 0 のときの案内 (root 未宣言 / query 不一致)
	NoFetch      bool
	WithSessions bool // sessions view から始める (既定は repos)
	// 自動更新間隔は view ごと。procs は fetch を伴わないので速く回せる
	SessionsInterval time.Duration
	ReposInterval    time.Duration
	Color            bool
}

// Run は TUI を起動し、終了までブロックする。
func Run(opts Options) error {
	p := tea.NewProgram(newModel(opts), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// concurrency は 1 回の refresh で同時に走らせる repo 数。
const concurrency = 12

// sem は tea.Batch が repo ぶんの goroutine を一斉に立てるのを抑える。
var sem = make(chan struct{}, concurrency)

// view は表示中の画面。repos が既定の入口で、tab の並びもこの順。
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

// tickMsg は自動更新の合図。gen は armTimer の世代で、view 切り替えで
// 間隔を変えたあとに旧 timer の残りが発火しても捨てられるようにする。
type tickMsg struct{ gen int }

// pane は 1 画面ぶんの表と、その中での位置。view を往復しても位置を失わない。
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
	// loaded は view ごとに 1 度でも収集したか。切り替えた先が空のままにならないようにする
	loaded [viewCount]bool

	failed []string // repos 側: fetch に失敗した repo

	showHelp bool // help float を出しているか

	// refresh の状態は view ごとに独立。repos の fetch が長くても
	// sessions 側の収集を待たせない
	gens        [viewCount]int // refresh 世代。古い結果を捨てるために使う
	refreshing  [viewCount]bool
	timerGen    int // timer 世代。view 切り替えで乗り換えた旧 timer を捨てる
	pending     int // repos 側: 未着の repo 数
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
		spinner: sp,
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

// interval は今の view の自動更新間隔。
func (m *model) interval() time.Duration {
	if m.view == viewSessions {
		return m.opts.SessionsInterval
	}
	return m.opts.ReposInterval
}

// startRefresh は今見ている view のぶんだけ引き直す。裏の view は触らない。
func (m *model) startRefresh() tea.Cmd {
	v := m.view
	if m.refreshing[v] {
		return nil
	}
	m.gens[v]++
	m.refreshing[v] = true

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

	cmds := make([]tea.Cmd, 0, len(m.opts.Repos)+1)
	cmds = append(cmds, m.spinner.Tick)
	for i, r := range m.opts.Repos {
		cmds = append(cmds, collectCmd(m.gens[v], i, r.Path, m.opts.NoFetch))
	}
	return tea.Batch(cmds...)
}

// collectCmd は repo 1 件ぶん。結果は届いた順に反映される。
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

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.rebuild()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tickMsg:
		// 旧世代の timer と、refresh 中に発火した tick は捨てる (重ねて走らせない)
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
	// help が開いている間は modal として振る舞い、閉じる key 以外は無視する
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

	case "p":
		m.view = (m.view + 1) % viewCount
		m.rebuild()
		// timer は新しい view の間隔で乗り換え、初めて来た view はその場で引く
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

// rebuild は表を作り直す。cursor 移動では呼ばない (データか窓が変わったときだけ)。
func (m *model) rebuild() {
	m.sessions.tbl = buildSessionTable(m.procList, m.th)
	m.repos.tbl = buildTable(rows.Build(m.opts.Repos, m.infos), m.th)
	m.snapCursor(1)
	m.clampView()
}

// move は cursor をデータ行だけ辿って動かす。
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

// snapCursor は枠や見出しの上に居る cursor をデータ行へ寄せる。
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

// bodyHeight は表に使える行数。header と footer (区切り線 + notice +
// status bar) を除く。列見出しは各表が枠の中に持つ。
func (m *model) bodyHeight() int {
	h := m.height - len(m.headerLines()) - 2 - len(m.noticeLines())
	if h < 1 {
		h = 1
	}
	return h
}

// logoLines は toilet (pagga) で一度だけ生成した wordmark (shade は落とした)。
// 実行時生成の library を足すほどのものではないので const で埋める。
var logoLines = []string{
	" █ █ █▀█ █▀▀ █ █ █▀▄ █▀█",
	"  █  █▀█ █ █ █ █ █▀▄ █▀█",
	"  ▀  ▀ ▀ ▀▀▀ ▀▀▀ ▀ ▀ ▀ ▀",
}

// gradientLine は line を ramp の段数に等分し、左から順に塗る。
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

// headerLines は tab 行と logo を組み、区切り線で board に接地させる。
// tab は logo の最終行に下揃え。logo は飾りなので、幅が足りない端末では
// 黙って消えて tab 行だけになる。
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

// tabLine は view の一覧 (feature 名で固定)。居る場所が bold、他は dim。
// どの command の session かは表の CMD 列が行ごとに持つ。
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

// clampView は cursor が見えるところまで offset を寄せる。
func (m *model) clampView() {
	// 窓の大きさが来る前は行数が分からない。ここで動かすと offset が狂う
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

	// footer は区切り線で board に接地させる
	out = append(out, m.th.border.Render(strings.Repeat(barH, m.width)))
	out = append(out, m.noticeLines()...)
	out = append(out, m.statusBar())
	return strings.Join(out, "\n")
}

// helpRows は key binding の一覧。help float の原本で、status bar の hint は
// 入口 (? と q) だけに絞ってこちらへ誘導する。
var helpRows = []struct{ key, desc string }{
	{"j / k", "move"},
	{"g / G", "top / bottom"},
	{"ctrl+d / u", "half page"},
	{"p", "switch view"},
	{"r", "refresh now"},
	{"?", "toggle help"},
	{"q / esc", "quit"},
}

// overlayHelp は body の中央に help の float を重ねる。
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

// spliceLine は base の x から幅 w の cell を box で置き換える。float 用。
// base が x に満たない短さなら space で埋めてから置く。
func spliceLine(base, box string, x, w int) string {
	left := ansi.Truncate(base, x, "")
	if fill := x - ansi.StringWidth(left); fill > 0 {
		left += strings.Repeat(" ", fill)
	}
	return left + box + ansi.TruncateLeft(base, x+w, "")
}

// formatInterval は Duration.String() の末尾に付く 0 単位を落とす (1m0s → 1m)。
// 数字を削ってしまわないよう、削った結果が単位で終わるときだけ採用する。
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

// stateW は footer 各行の先頭 slot の幅。idle / warn / note が同じ幅で並び、
// 続く区切り │ が縦に揃う。
const stateW = len(" idle")

// noticeLine は notice を bar と同じ文法 (state slot + │ + 本文) の行にする。
func (m *model) noticeLine(st lipgloss.Style, tag, s string) string {
	sep := m.th.hint.Render(" │ ")
	return clip(st.Render(pad(indent+tag, stateW, false))+sep+st.Render(s), m.width)
}

// noticeLines は view ごとの補足。root まわりの警告は repos view にだけ出し、
// sessions view を root 無しでも静かに使えるようにする。
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
	return lines
}

func (m *model) statusBar() string {
	// refresh 中は idle の位置に spinner だけを出す。slot 幅が固定なので左側はずれない
	state := m.th.bar.Render(pad(" idle", stateW, false))
	if m.refreshing[m.view] {
		state = m.th.barOn.Render(pad(" "+m.spinner.View(), stateW, false))
	}

	last := "—"
	if !m.lastRefresh.IsZero() {
		last = m.lastRefresh.Format("15:04:05")
	}

	// view 名の segment は置かない。count の単位 (session / repo) が view を語る
	count := countLabel(len(m.procList), "session")
	if m.view == viewRepos {
		count = countLabel(len(m.opts.Repos), "repo") + " · " + countLabel(m.opts.Roots, "root")
	}

	sep := m.th.hint.Render(" │ ")
	left := state + sep +
		m.th.bar.Render("last "+last) + sep +
		m.th.bar.Render("every "+formatInterval(m.interval())) + sep +
		m.th.bar.Render(count)
	if m.view == viewRepos && m.opts.NoFetch {
		left += sep + m.th.bar.Render("no-fetch")
	}

	right := m.th.hint.Render("? help  q quit ")

	// 幅が足りないときは key hint を落とす。切れた文字列を晒すより読める
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
