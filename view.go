package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Layout in the style of linear-tui: no boxes (herdr frames the popup), one-line rules, a
// breadcrumb header, key hints in the footer, and a ▌ accent bar plus a background on the
// selected row.
const (
	formMaxWidth   = 110
	formLabelWidth = 9
	previewMinW    = 90 // narrower than this, the menu drops its preview
	minW, minH     = 60, 15
)

var (
	pal                                                       palette
	stText, stMuted, stFaint, stDim, stAccent, stBold, stWarn lipgloss.Style
	stRed, stGreen, stPlain                                   lipgloss.Style
)

func initStyles(p palette) {
	pal = p
	fg := func(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }
	stText, stMuted, stFaint, stDim = fg(p.text), fg(p.muted), fg(p.faint), fg(p.dim)
	stAccent, stWarn, stRed, stGreen = fg(p.accent), fg(p.yellow), fg(p.red), fg(p.green)
	stBold = stText.Bold(true)
	stPlain = lipgloss.NewStyle()
}

// seg is one styled run of plain text in a line.
type seg struct {
	st lipgloss.Style
	s  string
}

// paint lays segments into exactly w columns: left ones truncated with … to make room for the
// right ones. A non-empty bg fills the whole line, behind every segment too (an inner reset
// would otherwise cut a single outer background short).
func paint(w int, bg string, left, right []seg) string {
	rw := 0
	for _, r := range right {
		rw += lipgloss.Width(r.s)
	}
	if rw > w {
		right, rw = nil, 0
	}
	room := w - rw
	if rw > 0 {
		room-- // a gap before the right side
	}
	var b strings.Builder
	style := func(st lipgloss.Style) lipgloss.Style {
		if bg != "" {
			return st.Background(lipgloss.Color(bg))
		}
		return st
	}
	used := 0
	for _, l := range left {
		if used >= room {
			break
		}
		s := l.s
		if lipgloss.Width(s) > room-used {
			s = ansi.Truncate(s, room-used, "…")
		}
		b.WriteString(style(l.st).Render(s))
		used += lipgloss.Width(s)
	}
	b.WriteString(style(stPlain).Render(strings.Repeat(" ", max(0, w-rw-used))))
	for _, r := range right {
		b.WriteString(style(r.st).Render(r.s))
	}
	return b.String()
}

// fit truncates or pads an already-styled string to w columns.
func fit(s string, w int) string {
	if lipgloss.Width(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}

func wrap(s string, w int) []string {
	if w < 4 || s == "" {
		return nil
	}
	ls := strings.Split(lipgloss.NewStyle().Width(w).Render(s), "\n")
	for i := range ls {
		ls[i] = strings.TrimRight(ls[i], " ") // lipgloss pads to the width; callers pad themselves
	}
	return ls
}

func rule(w int) string { return stDim.Render(strings.Repeat("─", w)) }

type hint struct{ key, label string }

// chrome frames a screen: breadcrumb and meta, a rule, the body, a rule, flash and key hints.
func (m model) chrome(crumbs []string, meta string, body []string, keys []hint) string {
	w := m.width
	head := []seg{{stAccent, " ◆ "}}
	for i, c := range crumbs {
		if i > 0 {
			head = append(head, seg{stFaint, " › "})
		}
		st := stMuted
		if i == len(crumbs)-1 {
			st = stBold
		}
		head = append(head, seg{st, c})
	}
	lines := []string{paint(w, "", head, []seg{{stFaint, meta + " "}}), rule(w)}
	for i := 0; i < m.height-4; i++ {
		l := ""
		if i < len(body) {
			l = body[i]
		}
		lines = append(lines, l)
	}
	foot := []seg{{stPlain, " "}}
	if m.flash != "" {
		foot = append(foot, seg{stWarn, m.flash + "   "})
	}
	for _, k := range keys {
		foot = append(foot, seg{stBold, k.key}, seg{stFaint, " " + k.label + "   "})
	}
	lines = append(lines, rule(w), paint(w, "", foot, nil))
	return strings.Join(lines, "\n")
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.width < minW || m.height < minH {
		msg := stMuted.Render("make the popup bigger")
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
	}
	switch m.screen {
	case scForm:
		return m.viewForm()
	case scPick:
		return m.viewPick()
	}
	return m.viewMenu()
}

// ---------- glyphs ----------

func hexStyle(c string, fallback lipgloss.Style) lipgloss.Style {
	if c == "" {
		return fallback
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(c))
}

func stateGlyph(typ, color string) seg {
	g := map[string]string{"started": "◐", "unstarted": "○", "backlog": "◌", "triage": "◇", "completed": "●", "canceled": "⊘"}[typ]
	if g == "" {
		g = "○"
	}
	return seg{hexStyle(color, stMuted), g}
}

func priorityGlyph(p int) seg {
	switch p {
	case 1:
		return seg{stRed.Bold(true), "!"}
	case 2:
		return seg{stText, "▪"}
	case 3:
		return seg{stMuted, "▪"}
	case 4:
		return seg{stDim, "▪"}
	}
	return seg{stDim, "·"}
}

func labelSegs(ls []Label, max int) []seg {
	var out []seg
	for i, l := range ls {
		if i == max {
			out = append(out, seg{stFaint, fmt.Sprintf("+%d ", len(ls)-max)})
			break
		}
		out = append(out, seg{hexStyle(l.Color, stMuted), "●"}, seg{stMuted, " " + l.Name + "  "})
	}
	return out
}

func segsWidth(ss []seg) int {
	w := 0
	for _, x := range ss {
		w += lipgloss.Width(x.s)
	}
	return w
}

func (m model) agentLabel(i int) string {
	if i < len(m.agents) {
		return m.agents[i].Label
	}
	return "no agent"
}

func (m model) naming(key string) []seg {
	return []seg{{stAccent, m.spin.View() + " naming with Claude"}, {stFaint, fmt.Sprintf(" · %ds", int(m.nameWait(key).Seconds()))}}
}

// ---------- menu ----------

func (m model) columns() (int, int) {
	if m.width < previewMinW {
		return m.width, 0
	}
	left := m.width * 55 / 100
	return left, m.width - left
}

func (m model) viewMenu() string {
	left, right := m.columns()
	bodyH := m.height - 4
	q := strings.TrimSpace(m.input.Value())

	var count []seg
	switch {
	case m.issuesLoading:
		count = []seg{{stAccent, m.spin.View()}, {stFaint, " loading issues "}}
	case q != "" && len(m.issues) > 0:
		count = []seg{{stFaint, fmt.Sprintf("%d/%d ", m.matchedIssues(), len(m.issues))}}
	}
	in := m.input
	in.Width = max(10, left-6-segsWidth(count))
	lines := []string{paint(left, "", []seg{{stAccent.Bold(true), " ❯ "}, {stPlain, in.View()}}, count), ""}

	for _, t := range m.missing {
		lines = append(lines, paint(left, "", []seg{{stWarn, "   " + t}}, nil))
	}
	listH := bodyH - len(lines)
	start := 0
	if m.cursor >= listH {
		start = m.cursor - listH + 1
	}
	for i := start; i < len(m.rows) && len(lines) < bodyH; i++ {
		lines = append(lines, m.rowLine(m.rows[i], i == m.cursor, left))
	}
	if !m.issuesLoading && len(m.issues) == 0 && m.repo != "" {
		lines = append(lines, "", paint(left, "", []seg{{stFaint, "   no Linear issues here"}}, nil))
		if m.issuesErr != "" {
			for _, l := range wrap(m.issuesErr, left-6) {
				lines = append(lines, paint(left, "", []seg{{stDim, "   " + l}}, nil))
			}
		}
	}
	if m.repo == "" {
		lines = append(lines, "", paint(left, "", []seg{{stFaint, "   not in a git repository: type new work, then pick a project"}}, nil))
	}

	body := lines
	if right > 0 {
		pv := m.menuPreview(right - 3)
		body = make([]string, bodyH)
		for i := range body {
			l, r := "", ""
			if i < len(lines) {
				l = lines[i]
			}
			if i < len(pv) {
				r = pv[i]
			}
			body[i] = fit(l, left) + stDim.Render("│") + "  " + fit(r, right-3)
		}
	}

	enter := "open"
	if m.cursor < len(m.rows) {
		switch m.rows[m.cursor].kind {
		case rowNew:
			enter = "new worktree"
		case rowClean:
			enter = "clean"
		}
	}
	meta := ""
	if !m.issuesLoading && len(m.issues) > 0 {
		meta = fmt.Sprintf("%d issues · ", len(m.issues))
	}
	meta += m.agentLabel(m.defaultAgent(m.repo))
	return m.chrome([]string{"Worktrees", m.repoName(m.repo)}, meta, body,
		[]hint{{"⏎", enter}, {"↑↓", "move"}, {"⌥c", "clean"}, {"esc", "close"}})
}

func (m model) rowLine(r row, sel bool, w int) string {
	bg, bar := "", seg{stPlain, " "}
	title := stText
	if sel {
		bg, bar, title = pal.sel, seg{stAccent, "▌"}, stBold
	}
	switch r.kind {
	case rowNew:
		return paint(w, bg, []seg{bar, {stAccent, " + "}, {title, "New worktree  "}, {stMuted, "“" + strings.TrimSpace(m.input.Value()) + "”"}}, nil)
	case rowGroup:
		is := r.issue
		return paint(w, "", []seg{{stPlain, " "}, {stDim, "▾ "}, stateGlyph(is.StateType, is.StateColor), {stBold, " " + is.State}, {stFaint, fmt.Sprintf("  %d", r.count)}}, nil)
	case rowIssue:
		is := r.issue
		var labels []seg
		if w >= 80 {
			// A fixed-width label column keeps the titles' right edges in line.
			lw := min(30, w/4)
			n := 2 // as many labels as fit, then "+N"
			for n > 0 && segsWidth(labelSegs(is.Labels, n)) > lw {
				n--
			}
			labels = []seg{{stPlain, paint(lw, bg, labelSegs(is.Labels, n), nil)}}
		}
		tags, rest := splitTags(is.Title)
		left := []seg{bar, {stPlain, " "}, priorityGlyph(is.Priority), {stFaint, " " + fmt.Sprintf("%-9s", is.ID)}, stateGlyph(is.StateType, is.StateColor), {stPlain, " "}}
		if tags != "" {
			left = append(left, seg{stFaint, tags + " "})
		}
		return paint(w, bg, append(left, seg{title, rest}), labels)
	case rowClean:
		return paint(w, bg, []seg{bar, {stFaint, " ⌫ "}, {title, "Clean worktrees…"}}, []seg{{stFaint, "asks first "}})
	}
	return ""
}

func (m model) menuPreview(w int) []string {
	if m.cursor >= len(m.rows) {
		return nil
	}
	var out []string
	add := func(s ...seg) { out = append(out, paint(w, "", s, nil)) }
	text := func(st lipgloss.Style, s string) {
		for _, l := range wrap(s, w) {
			add(seg{st, l})
		}
	}
	switch r := m.rows[m.cursor]; r.kind {
	case rowNew:
		add(seg{stBold, "New worktree"})
		text(stMuted, strings.TrimSpace(m.input.Value()))
		out = append(out, "")
		text(stFaint, "Claude names the branch and title when you create. An exact branch name, with no spaces, is used as typed.")
	case rowIssue:
		is := r.issue
		meta := []seg{{stFaint, is.ID + "  "}, stateGlyph(is.StateType, is.StateColor), {stMuted, " " + is.State}}
		if is.PriorityLabel != "" && is.Priority != 0 {
			meta = append(meta, seg{stFaint, " · "}, priorityGlyph(is.Priority), seg{stMuted, " " + is.PriorityLabel})
		}
		add(meta...)
		out = append(out, "")
		text(stBold, is.Title)
		if len(is.Labels) > 0 {
			out = append(out, "")
			add(labelSegs(is.Labels, 6)...)
		}
		d, ok := m.details[is.ID]
		switch {
		case !ok:
			out = append(out, "")
			add(seg{stAccent, m.spin.View()}, seg{stFaint, " loading the description"})
		case d.Err != "":
			out = append(out, "")
			text(stWarn, d.Err)
		case strings.TrimSpace(d.Description) != "":
			out = append(out, "")
			for _, para := range strings.Split(strings.TrimSpace(d.Description), "\n") {
				if strings.TrimSpace(para) == "" {
					out = append(out, "")
					continue
				}
				text(mdLine(para))
			}
		}
	case rowClean:
		add(seg{stBold, "Clean worktrees"})
		out = append(out, "")
		text(stMuted, "Shows what would go in "+m.repoName(m.repo)+": idle worktrees, stale records, empty leftover folders and orphan Xcode caches, with sizes and the reason for every skip. Nothing is deleted until you say yes.")
	}
	return out
}

// splitTags splits leading bracket tags ("[iOS] [Parity] ") from a title, to draw them dimmer.
func splitTags(t string) (string, string) {
	i := 0
	for i < len(t) && t[i] == '[' {
		j := strings.IndexByte(t[i:], ']')
		if j < 0 {
			break
		}
		i += j + 1
		for i < len(t) && t[i] == ' ' {
			i++
		}
	}
	return strings.TrimSpace(t[:i]), t[i:]
}

// mdLine draws one line of a Markdown description simply: headings bold, bullets as •, and
// emphasis and code marks dropped.
func mdLine(l string) (lipgloss.Style, string) {
	l = strings.NewReplacer("**", "", "__", "", "`", "", "\\~", "~").Replace(l)
	t := strings.TrimSpace(l)
	switch {
	case strings.HasPrefix(t, "#"):
		return stMuted.Bold(true), strings.TrimSpace(strings.TrimLeft(t, "#"))
	case strings.HasPrefix(t, "- [ ] "), strings.HasPrefix(t, "* [ ] "):
		return stFaint, "☐ " + t[6:]
	case strings.HasPrefix(t, "* "), strings.HasPrefix(t, "- "):
		return stFaint, "• " + t[2:]
	}
	return stFaint, l
}

func whereBadge(where string) []seg {
	switch where {
	case "new":
		return []seg{{stGreen, "new"}}
	case "local", "remote":
		return []seg{{stWarn, "exists"}}
	case "main":
		return []seg{{stRed, "main checkout"}}
	}
	return nil
}

// ---------- form ----------

func (m model) viewForm() string {
	f := m.form
	w := min(m.width, formMaxWidth)
	var lines []string
	add := func(left, right []seg) { lines = append(lines, paint(w, "", left, right)) }

	// What you are creating.
	if is, ok := m.issue(f.issueID); ok {
		add([]seg{{stPlain, "  "}, {stFaint, is.ID + "  "}, stateGlyph(is.StateType, is.StateColor), {stBold, " " + is.Title}}, nil)
		meta := []seg{{stMuted, is.State}}
		if is.Priority != 0 {
			meta = append(meta, seg{stFaint, " · "}, priorityGlyph(is.Priority), seg{stMuted, " " + is.PriorityLabel + " "})
		}
		add(append([]seg{{stPlain, "  " + strings.Repeat(" ", len(is.ID)+2)}}, labelSegs(is.Labels, 4)...), meta)
	} else {
		add([]seg{{stPlain, "  "}, {stFaint, "Task  "}, {stBold, f.input}}, nil)
	}
	lines = append(lines, "")

	labels := map[int]string{fTitle: "Title", fBranch: "Branch", fHint: "Naming", fBase: "Base", fProject: "Project", fAgent: "Agent", fPrompt: "Prompt"}
	for _, fld := range []int{fTitle, fBranch, fHint, fBase, fProject, fAgent, fPrompt} {
		sel := f.cursor == fld
		bar, label := seg{stPlain, " "}, stMuted
		if sel {
			bar, label = seg{stAccent, "▌"}, stAccent
		}
		head := []seg{bar, {stPlain, " "}, {label, fmt.Sprintf("%-*s", formLabelWidth, labels[fld])}}
		badge := m.badge(fld)
		if fld == fPrompt {
			for i, l := range m.promptLines(sel, w-formLabelWidth-22) { // 22: label gutter + badge
				if i > 0 {
					head = []seg{bar, {stPlain, strings.Repeat(" ", formLabelWidth+1)}}
					badge = nil
				}
				add(append(head, seg{stPlain, l}), badge)
			}
			continue
		}
		add(append(head, m.fieldValue(fld, sel)...), badge)
	}
	lines = append(lines, "")

	btn := lipgloss.NewStyle().Background(lipgloss.Color(pal.surface)).Foreground(lipgloss.Color(pal.text))
	label := " Create "
	if f.cursor == fCreate {
		btn = lipgloss.NewStyle().Background(lipgloss.Color(pal.accent)).Foreground(lipgloss.Color("#101010")).Bold(true)
		label = " Create ⏎ "
	}
	note := []seg{{stFaint, "worktree → herdr workspace → " + m.agentLabel(m.form.agent) + ", prompt typed, not sent"}}
	switch {
	case f.queued:
		note = append(m.naming(f.key), seg{stFaint, ", then creating · esc cancels"})
	case f.naming:
		note = []seg{{stFaint, "Claude is still naming; Create waits for it"}}
	}
	bar := seg{stPlain, " "}
	if f.cursor == fCreate {
		bar = seg{stAccent, "▌"}
	}
	add(append([]seg{bar, {stPlain, " "}, {btn, label}, {stPlain, "  "}}, note...), nil)
	if f.nameErr != "" {
		lines = append(lines, "")
		for _, l := range wrap("Naming failed: "+f.nameErr+". Fill in the branch and title yourself.", w-4) {
			add([]seg{{stWarn, "  " + l}}, nil)
		}
	}

	keys := []hint{{"^s", "create"}, {"↑↓", "field"}}
	switch {
	case f.cursor == fBase || f.cursor == fProject || f.cursor == fAgent:
		keys = append(keys, hint{"←→", "choose"}, hint{"⏎", "list"})
	case f.cursor == fCreate:
		keys = append(keys, hint{"⏎", "create"})
	default:
		keys = append(keys, hint{"type", "to edit"}, hint{"⏎", "next"})
	}
	if f.queued {
		keys = append(keys, hint{"esc", "cancel create"})
	} else {
		keys = append(keys, hint{"esc", "back"})
	}
	return m.chrome([]string{"Worktrees", m.repoName(f.project), "New worktree"}, m.agentLabel(f.agent), lines, keys)
}

// fieldValue renders one field's value: the live input when focused, else its text.
func (m model) fieldValue(fld int, sel bool) []seg {
	f := m.form
	switch fld {
	case fTitle, fBranch:
		in := m.inTitle
		if fld == fBranch {
			in = m.inBranch
		}
		empty := in.Value() == ""
		switch {
		case empty && f.naming && !f.edited[fld]:
			if sel { // keep the cursor visible
				return append([]seg{{stPlain, "▏"}}, m.naming(f.key)...)
			}
			return m.naming(f.key)
		case sel:
			return []seg{{stPlain, in.View()}}
		case empty:
			return []seg{{stAccent, "✦"}, {stFaint, " " + in.Placeholder}}
		}
		return []seg{{stText, in.Value()}}
	case fHint:
		switch {
		case sel:
			return []seg{{stPlain, m.inHint.View()}}
		case m.inHint.Value() == "":
			return []seg{{stFaint, m.inHint.Placeholder}}
		}
		return []seg{{stText, m.inHint.Value()}}
	}
	var v string
	switch fld {
	case fBase:
		v = f.base
		if v == "" {
			v = "default"
		}
	case fProject:
		v = m.repoName(f.project)
		if f.project == "" {
			return []seg{{stWarn, "pick a project  ⏎"}}
		}
	case fAgent:
		v = m.agentLabel(f.agent)
	}
	if sel {
		return []seg{{stAccent, "‹ "}, {stBold, v}, {stAccent, " ›"}}
	}
	return []seg{{stText, v}}
}

// promptLines is the prompt as up to three lines: the live textarea when focused.
func (m model) promptLines(sel bool, w int) []string {
	if sel {
		return strings.Split(m.inPrompt.View(), "\n")
	}
	v := strings.TrimSpace(m.inPrompt.Value())
	if v == "" {
		return []string{stDim.Render("none")}
	}
	ls := wrap(v, w)
	if len(ls) > 3 {
		ls = append(ls[:2], ansi.Truncate(ls[2], w-1, "")+"…")
	}
	for i := range ls {
		ls[i] = stText.Render(ls[i])
	}
	return ls
}

func (m model) badge(fld int) []seg {
	f := m.form
	src := func(s string) []seg {
		switch {
		case f.edited[fld]:
			return []seg{{stFaint, "edited "}}
		case f.naming:
			return nil
		case s == "claude":
			return []seg{{stAccent, "✦ Claude "}}
		case s == "fallback":
			return []seg{{stWarn, "fallback "}}
		}
		return nil
	}
	switch fld {
	case fTitle:
		return src(f.names.TitleSource)
	case fBranch:
		b := src(f.names.BranchSource)
		if !f.edited[fBranch] && !f.naming {
			if w := whereBadge(f.names.Where); w != nil {
				b = append(b, seg{stFaint, "· "}, w[0], seg{stPlain, " "})
			}
		}
		return b
	case fBase:
		if f.base == "" {
			return []seg{{stFaint, "worktrunk's default "}}
		}
	case fPrompt:
		return []seg{{stFaint, "typed, not sent "}}
	}
	return nil
}

// ---------- pick ----------

func (m model) viewPick() string {
	p := m.pick
	names := map[int]string{fBase: "Base", fProject: "Project", fAgent: "Agent"}
	w := min(m.width, formMaxWidth)
	bodyH := m.height - 4
	lines := []string{""}
	start := 0
	if p.cursor >= bodyH-1 {
		start = p.cursor - bodyH + 2
	}
	for i := start; i < len(p.options) && len(lines) < bodyH; i++ {
		bg, bar, st := "", seg{stPlain, " "}, stText
		if i == p.cursor {
			bg, bar, st = pal.sel, seg{stAccent, "▌"}, stBold
		}
		label, side := p.options[i], ""
		switch p.field {
		case fProject:
			label, side = m.repoName(p.options[i]), p.options[i]
		case fBase:
			if label == "" {
				label, side = "default", "worktrunk's default branch"
			}
		}
		lines = append(lines, paint(w, bg, []seg{bar, {stPlain, " "}, {st, label}}, []seg{{stFaint, side + " "}}))
	}
	if len(p.options) == 0 {
		lines = append(lines, paint(w, "", []seg{{stFaint, "  nothing to choose from"}}, nil))
	}
	return m.chrome([]string{"Worktrees", m.repoName(m.form.project), "New worktree", names[p.field]}, "", lines,
		[]hint{{"⏎", "choose"}, {"↑↓", "move"}, {"esc", "back"}})
}
