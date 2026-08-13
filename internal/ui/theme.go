package ui

import "github.com/charmbracelet/lipgloss"

// 色の意味は plain 出力と揃える。
// yellow = 自分が動くべきもの (changed / ahead) と全開の session / cyan = remote との差 (情報)
// green = 稼働中の session / red = detached / dim = 0 と - と idle
// 全色 ANSI-16 の slot 番号のみ (端末テーマに追従させる)。footer に帯 (背景) は
// 敷かない — ANSI の slot 0 は端末の default 背景と別物で、テーマ次第でズレるため。
// cursor 行の背景は "8" だと colDim の前景と同 slot で dim 文字が溶けるので "0" を使う。
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
	remote  lipgloss.Style
	headOn  lipgloss.Style // default 以外の branch
	headOff lipgloss.Style // detached

	header     lipgloss.Style
	border     lipgloss.Style
	group      lipgloss.Style
	groupCount lipgloss.Style

	box lipgloss.Style // help などの float の枠
	// logoRamp は wordmark の段階 gradient (左から順に塗る)。純粋な飾り。
	// 滑らかな gradient は truecolor が要るので、ANSI-16 の明度階段で近似する
	logoRamp []lipgloss.Style

	bar      lipgloss.Style
	barOn    lipgloss.Style
	hint     lipgloss.Style
	warn     lipgloss.Style // 表の cell 用 (x など)。背景なし
	warnNote lipgloss.Style // footer の warn 行
	note     lipgloss.Style
}

func newTheme(color bool) theme {
	plain := lipgloss.NewStyle()
	th := theme{
		color:      color,
		dim:        plain,
		local:      plain,
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
	th.local = plain.Foreground(lipgloss.Color(colLocal))
	th.remote = plain.Foreground(lipgloss.Color(colRemote))
	// 既存の色は全部意味を持っているので、branch には色相を足さず bold だけで立てる
	th.headOn = plain.Bold(true)
	th.headOff = plain.Foreground(lipgloss.Color(colNG)).Bold(true)

	th.border = th.dim
	th.groupCount = th.dim
	th.box = th.box.BorderForeground(lipgloss.Color(colDim))
	// 青 → 明青 → cyan → 明 cyan。全部 ANSI-16 の slot
	th.logoRamp = []lipgloss.Style{
		plain.Foreground(lipgloss.Color("4")).Bold(true),
		plain.Foreground(lipgloss.Color("12")).Bold(true),
		plain.Foreground(lipgloss.Color("6")).Bold(true),
		plain.Foreground(lipgloss.Color("14")).Bold(true),
	}

	th.bar = plain.Foreground(lipgloss.Color(colBarFG))
	th.barOn = th.bar.Bold(true).Foreground(lipgloss.Color(colLocal))
	th.hint = th.bar.Foreground(lipgloss.Color(colDim))
	th.warn = plain.Foreground(lipgloss.Color(colLocal))
	th.warnNote = th.bar.Foreground(lipgloss.Color(colLocal))
	th.note = th.hint
	return th
}

// selected は cursor 行ぶんの背景を足す。色が無い端末では反転で代用する。
func (t theme) selected(st lipgloss.Style) lipgloss.Style {
	if !t.color {
		return st.Reverse(true)
	}
	return st.Background(lipgloss.Color(colCursorBG))
}
