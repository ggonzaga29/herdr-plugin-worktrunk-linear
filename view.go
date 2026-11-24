package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	cAccent = lipgloss.Color("12")
	cDim    = lipgloss.Color("8")
	cWarn   = lipgloss.Color("11")
	cSel    = lipgloss.Color("237")

	stAccent = lipgloss.NewStyle().Foreground(cAccent)
	stDim    = lipgloss.NewStyle().Foreground(cDim)
	stWarn   = lipgloss.NewStyle().Foreground(cWarn)
	stBold   = lipgloss.NewStyle().Bold(true)
	stBorder = lipgloss.NewStyle().Foreground(cDim)
)

// box draws a rounded box w×h with an optional title set into the top border.
func box(title string, lines []string, w, h int) string {
	if w < 6 || h < 2 {
		return ""
	}
	inner := w - 4
	var b strings.Builder
	if title != "" {
		t := ansi.Truncate(title, w-6, "…")
		b.WriteString(stBorder.Render("╭─ ") + stBold.Render(t) +
			stBorder.Render(" "+strings.Repeat("─", max(0, w-5-lipgloss.Width(t)))+"╮") + "\n")
	} else {
		b.WriteString(stBorder.Render("╭"+strings.Repeat("─", w-2)+"╮") + "\n")
	}
	for i := 0; i < h-2; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		l = ansi.Truncate(l, inner, "…")
		pad := inner - lipgloss.Width(l)
		b.WriteString(stBorder.Render("│") + " " + l + strings.Repeat(" ", max(0, pad)) + " " + stBorder.Render("│") + "\n")
	}
	b.WriteString(stBorder.Render("╰" + strings.Repeat("─", w-2) + "╯"))
	return b.String()
}

// line pads s to w columns, or truncates it.
func fit(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}

// leftRight puts r flush right of l within w columns.
func leftRight(l, r string, w int) string {
	rw := lipgloss.Width(r)
	if rw >= w {
		return ansi.Truncate(r, w, "…")
	}
	return fit(l, w-rw-1) + " " + r
}

// selected renders a highlighted row: accent pointer + background across the row.
func selected(s string, w int) string {
	return stAccent.Render("▌") + lipgloss.NewStyle().Background(cSel).Render(fit(s, w-1))
}

func plain(s string, w int) string { return " " + fit(s, w-1) }

func wrap(s string, w int) []string {
	if w < 4 {
		return nil
	}
	return strings.Split(lipgloss.NewStyle().Width(w).Render(s), "\n")
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	switch m.screen {
	case scForm:
		return m.viewForm()
	case scPick:
		return m.viewPick()
	}
	return m.viewMenu()
}

// layout: left column width, right (preview) width; preview hidden on narrow screens.
func (m model) columns() (int, int) {
	if m.width < 90 {
		return m.width, 0
	}
	left := m.width * 56 / 100
	return left, m.width - left
}

func (m model) footer(keys string) string {
	lines := []string{stDim.Render(keys)}
	if m.flash != "" {
		lines = []string{stWarn.Render(m.flash) + "  " + stDim.Render(keys)}
	}
	return box("", lines, m.width, 3)
}

func (m model) viewMenu() string {
	left, right := m.columns()
	bodyH := max(5, m.height-3-3)
	listH := bodyH - 3

	// Input box with the count on the right.
	count := ""
	if !m.issuesLoading && len(m.issues) > 0 {
		count = stDim.Render(fmt.Sprintf("%d/%d", m.matchedIssues(), len(m.issues)))
	}
	m.input.Width = max(10, left-10-lipgloss.Width(count))
	inLine := leftRight(stAccent.Bold(true).Render(">")+" "+m.input.View(), count, left-4)
	title := "Worktrees · " + m.repoName(m.repo)
	inBox := box(title, []string{inLine}, left, 3)

	// List rows.
	inner := left - 4
	var lines []string
	start := 0
	if m.cursor >= listH-2 {
		start = m.cursor - (listH - 3)
	}
	for i := start; i < len(m.rows) && len(lines) < listH-2; i++ {
		r := m.rows[i]
		var text, side string
		switch r.kind {
		case rowNew:
			text = "+ New worktree: " + strings.TrimSpace(m.input.Value())
		case rowIssue:
			text = "+ " + r.issue.ID + "  " + r.issue.Title
			side = stDim.Render(r.issue.State)
		case rowClean:
			text = "- Clean worktrees…"
			side = stDim.Render("asks first")
		}
		content := leftRight(text, side, inner-1)
		if i == m.cursor {
			lines = append(lines, selected(content, inner))
		} else {
			lines = append(lines, plain(content, inner))
		}
	}
	if m.issuesLoading {
		lines = append(lines, " "+m.spin.View()+stDim.Render(" loading your Linear issues…"))
	} else if len(m.issues) == 0 && m.repo != "" {
		lines = append(lines, "", stDim.Render(" No Linear issues for this repo"))
		if m.issuesErr != "" {
			lines = append(lines, stDim.Render(" ("+m.issuesErr+")"))
		}
	}
	if m.repo == "" {
		lines = append(lines, "", stDim.Render(" Not in a git repository: type new work,"), stDim.Render(" then pick a project in the form"))
	}
	listBox := box("", lines, left, listH)
	leftCol := lipgloss.JoinVertical(lipgloss.Left, inBox, listBox)

	body := leftCol
	if right > 0 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, leftCol, m.menuPreview(right, bodyH))
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, m.footer("enter choose · type to start new work · alt-c clean · esc close"))
}

func (m model) menuPreview(w, h int) string {
	inner := w - 4
	if len(m.rows) == 0 {
		return box("", nil, w, h)
	}
	r := m.rows[m.cursor]
	var lines []string
	title := ""
	switch r.kind {
	case rowNew:
		title = "New worktree"
		lines = append(lines, wrap(strings.TrimSpace(m.input.Value()), inner)...)
		lines = append(lines, "",
			stDim.Render("Claude names the branch and title"), stDim.Render("(about 4 s). An exact branch name,"),
			stDim.Render("with no spaces, is kept as typed."), "",
			stAccent.Render("Enter: the form, filled in"))
	case rowIssue:
		is := r.issue
		title = is.ID
		lines = append(lines, wrap(is.Title, inner)...)
		lines = append(lines, "", stDim.Render("State   ")+is.State)
		if len(is.Labels) > 0 {
			lines = append(lines, stDim.Render("Labels  ")+strings.Join(is.Labels, ", "))
		}
		lines = append(lines, "", stDim.Render("Branch, from Linear"))
		d, ok := m.details[is.ID]
		switch {
		case !ok:
			lines = append(lines, m.spin.View()+stDim.Render(" loading…"))
		case d.Err != "":
			lines = append(lines, stWarn.Render(d.Err))
		case d.Branch != "":
			lines = append(lines, d.Branch)
		default:
			lines = append(lines, stDim.Render("none; the ID is used"))
		}
		if ok && strings.TrimSpace(d.Description) != "" {
			lines = append(lines, "")
			for _, l := range wrap(strings.TrimSpace(d.Description), inner) {
				lines = append(lines, stDim.Render(l))
			}
		}
		// Keep the hint visible at the bottom.
		room := h - 2 - 2
		if len(lines) > room {
			lines = lines[:max(0, room)]
		}
		for len(lines) < room {
			lines = append(lines, "")
		}
		lines = append(lines, stAccent.Render("Enter: the form, with a"), stAccent.Render("short title from Claude"))
	case rowClean:
		title = "Clean worktrees"
		lines = append(lines, wrap("Shows what would be removed in "+m.repoName(m.repo)+", with sizes and the reason for every skip, then asks before deleting anything.", inner)...)
		lines = append(lines, "", stDim.Render("Idle worktrees, stale records, empty"), stDim.Render("leftover folders, orphan Xcode caches."))
	}
	return box(title, lines, w, h)
}

func (m model) viewForm() string {
	f := m.form
	left, right := m.columns()
	bodyH := max(fieldCount+2, m.height-3)
	inner := left - 4
	labels := [fieldCount]string{"", "Title", "Branch", "Base", "Project", "Agent", "Prompt"}
	var lines []string
	for i := 0; i < fieldCount; i++ {
		var content string
		if i == fCreate {
			c := "▶ Create"
			if f.naming {
				c += stDim.Render("  waits for the names")
			}
			content = stBold.Render(c)
		} else {
			label := fit(labels[i], 9)
			value, badge := m.fieldValue(i)
			if f.editing && f.cursor == i {
				value = m.edit.View()
				badge = ""
			}
			content = leftRight(stDim.Render(label)+value, badge, inner-1)
		}
		if i == f.cursor {
			lines = append(lines, selected(content, inner))
		} else {
			lines = append(lines, plain(content, inner))
		}
	}
	if f.nameErr != "" {
		lines = append(lines, "")
		for _, l := range wrap("Naming failed: "+f.nameErr, inner-2) {
			lines = append(lines, " "+stWarn.Render(l))
		}
	}
	formBox := box("New worktree · "+m.repoName(f.project), lines, left, bodyH)
	body := formBox
	if right > 0 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, formBox, m.formPreview(right, bodyH))
	}
	keys := "enter edit a field, or Create on the first row · esc back to the menu"
	if f.editing {
		keys = "enter save · esc cancel"
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, m.footer(keys))
}

// fieldValue returns a field's shown value and its badge.
func (m model) fieldValue(i int) (string, string) {
	f := m.form
	naming := m.spin.View() + stAccent.Render(" naming with Claude…")
	switch i {
	case fTitle:
		if f.naming && !f.edited[fTitle] && f.issueID == "" {
			return naming, ""
		}
		return f.title, sourceBadge(f.edited[fTitle], f.naming, f.names.TitleSource)
	case fBranch:
		if f.naming && !f.edited[fBranch] && f.branch == "" {
			return naming, ""
		}
		badge := sourceBadge(f.edited[fBranch], f.naming, f.names.BranchSource)
		if !f.edited[fBranch] && !f.naming {
			switch f.names.Where {
			case "new":
				badge = joinBadge(badge, "new")
			case "local", "remote":
				badge = joinBadge(badge, "exists")
			case "main":
				badge = stWarn.Render("main checkout!")
			}
		}
		return f.branch, badge
	case fBase:
		if f.base == "" {
			return "default", stDim.Render("worktrunk")
		}
		return f.base, ""
	case fProject:
		if f.project == "" {
			return stWarn.Render("pick a project"), ""
		}
		return m.repoName(f.project), ""
	case fAgent:
		if len(m.agents) == 0 {
			return stWarn.Render("no agent CLI found"), ""
		}
		return m.agents[f.agent].Label, ""
	case fPrompt:
		if f.prompt == "" {
			return stDim.Render("none"), ""
		}
		badge := stDim.Render("typed, unsent")
		if f.naming && f.issueID == "" && !f.edited[fPrompt] {
			badge = ""
		}
		return f.prompt, badge
	}
	return "", ""
}

func sourceBadge(edited, naming bool, src string) string {
	if edited {
		return stDim.Render("edited")
	}
	if naming {
		return ""
	}
	switch src {
	case "claude":
		return stAccent.Render("Claude")
	case "linear":
		return stAccent.Render("Linear")
	case "fallback":
		return stWarn.Render("fallback")
	}
	return ""
}

func joinBadge(a, b string) string {
	if a == "" {
		return stDim.Render(b)
	}
	return a + stDim.Render(" · "+b)
}

func (m model) formPreview(w, h int) string {
	f := m.form
	inner := w - 4
	launcher := "agent"
	if len(m.agents) > 0 {
		a := m.agents[f.agent]
		launcher = a.Args[1]
		if a.Args[0] == "-a" {
			launcher = "claude"
			if a.Args[1] != "personal" {
				launcher = "claude-" + a.Args[1]
			}
		}
	}
	verb := "wt switch --create"
	switch f.names.Where {
	case "local", "remote":
		verb = "wt switch"
	}
	if f.edited[fBranch] {
		verb = "wt switch (--create if new)"
	}
	base := "base: default branch"
	if f.base != "" {
		base = "base: " + f.base
	}
	branch, title := f.branch, f.title
	if branch == "" {
		branch = "…"
	}
	if title == "" {
		title = "…"
	}
	lines := []string{
		"1  " + verb,
		stDim.Render("   " + branch),
		stDim.Render("   " + base + ", hooks run"),
		"",
		"2  herdr worktree open",
		stDim.Render("   " + title),
		"",
		"3  " + launcher + " in the new pane",
	}
	if f.prompt != "" {
		lines = append(lines, stDim.Render("   prompt typed, not sent"))
	} else {
		lines = append(lines, stDim.Render("   no prompt"))
	}
	lines = append(lines, "")
	for _, l := range wrap("Popup closes after step 3; answer the agent in its pane.", inner) {
		lines = append(lines, stAccent.Render(l))
	}
	return box("What Create runs", lines, w, h)
}

func (m model) viewPick() string {
	p := m.pick
	names := map[int]string{fBase: "Base", fProject: "Project", fAgent: "Agent"}
	w := min(m.width, 70)
	inner := w - 4
	h := min(m.height-3, len(p.options)+2)
	var lines []string
	start := 0
	if p.cursor >= h-2 {
		start = p.cursor - (h - 3)
	}
	for i := start; i < len(p.options) && len(lines) < h-2; i++ {
		label := p.options[i]
		if p.field == fProject {
			label = m.repoName(label) + stDim.Render("  "+label)
		}
		if i == p.cursor {
			lines = append(lines, selected(label, inner))
		} else {
			lines = append(lines, plain(label, inner))
		}
	}
	if len(p.options) == 0 {
		lines = append(lines, stDim.Render(" nothing to choose from"))
	}
	pickBox := box(names[p.field], lines, w, max(3, h))
	pad := lipgloss.NewStyle().Height(max(0, m.height-3-lipgloss.Height(pickBox))).Render("")
	return lipgloss.JoinVertical(lipgloss.Left, pickBox, pad, m.footer("enter choose · esc back to the form"))
}
