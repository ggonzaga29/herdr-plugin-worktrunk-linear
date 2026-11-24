package main

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type screen int

const (
	scMenu screen = iota
	scForm
	scPick
)

type rowKind int

const (
	rowNew rowKind = iota
	rowGroup
	rowIssue
	rowClean
)

type row struct {
	kind  rowKind
	issue Issue // rowIssue; for rowGroup, the group's first issue (its state name, type, colour)
	count int   // rowGroup: issues in the group
}

func (r row) selectable() bool { return r.kind != rowGroup }

// Form fields, in display order.
const (
	fTitle = iota
	fBranch
	fHint // how Claude should name this one
	fBase
	fProject
	fAgent
	fPrompt
	fCreate
	fieldCount
)

func isText(f int) bool { return f == fTitle || f == fBranch || f == fHint || f == fPrompt }

// Action is what runs after the TUI exits, with the terminal handed over.
type Action struct {
	Kind string // "create" or "clean"
	Dir  string
	Args []string
}

type form struct {
	input, issueID string // what was picked: the typed text, or a ticket ID
	naming         bool
	nameErr        string
	names          Names
	base           string // "" = worktrunk's default
	project        string
	agent          int
	edited         [fieldCount]bool // fields you changed: a late naming answer leaves them alone
	cursor         int
	key            string // the naming job this form waits on (repo, input, hint, agent)
	queued         bool   // Create was pressed while naming: launch as soon as the names arrive
}

// nameJob is one `herdr-start --names` call. Jobs are cached by key and started early (while you
// are still in the menu), so the form is usually filled the moment it opens.
type nameJob struct {
	start time.Time
	done  bool
	names Names
	err   error
}

func nameKey(dir, input, hint string, a Agent) string {
	return dir + "\x00" + input + "\x00" + hint + "\x00" + a.Label
}

type pick struct {
	field   int
	options []string
	cursor  int
}

type model struct {
	width, height int
	screen        screen

	repo          string
	input         textinput.Model
	issues        []Issue
	issuesErr     string
	issuesLoading bool
	rows          []row
	cursor        int
	details       map[string]IssueDetail
	detailWanted  map[string]bool

	agents   []Agent
	projects []string
	bases    map[string][]string // recent branches per project, for the Base choice
	prefs    prefs

	form     form
	inTitle  textinput.Model
	inBranch textinput.Model
	inHint   textinput.Model
	inPrompt textarea.Model
	jobs     map[string]*nameJob
	pick     pick
	spin     spinner.Model
	flash    string
	missing  []string // required CLIs not on PATH, shown above the list

	action *Action
}

type issuesMsg struct {
	issues []Issue
	err    error
}
type detailMsg struct {
	id     string
	detail IssueDetail
}
type namesMsg struct {
	key   string
	names Names
	err   error
}

func newModel(repo string) model {
	initStyles(loadTheme())

	in := textinput.New()
	in.Placeholder = "type a task or branch, or filter issues"
	in.Prompt = ""
	in.PlaceholderStyle = stFaint
	in.Focus()

	field := func() textinput.Model {
		t := textinput.New()
		t.Prompt = ""
		t.Placeholder = "Claude names it on Create, or type your own"
		t.PlaceholderStyle = stFaint
		return t
	}
	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(3)
	ta.FocusedStyle.CursorLine = ta.FocusedStyle.CursorLine.UnsetBackground()

	hint := field()
	hint.Placeholder = "optional: tell Claude how to name this one"

	m := model{
		repo:          repo,
		input:         in,
		inTitle:       field(),
		inBranch:      field(),
		inHint:        hint,
		inPrompt:      ta,
		spin:          spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(stAccent)),
		details:       map[string]IssueDetail{},
		detailWanted:  map[string]bool{},
		jobs:          map[string]*nameJob{},
		bases:         map[string][]string{},
		agents:        listAgents(),
		missing:       missingTools(),
		prefs:         loadPrefs(),
		issuesLoading: repo != "",
	}
	m.refilter()
	return m
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{textinput.Blink, m.spin.Tick}
	if m.repo != "" {
		repo := m.repo
		cmds = append(cmds, func() tea.Msg {
			is, err := loadIssues(repo)
			return issuesMsg{is, err}
		})
	}
	return tea.Batch(cmds...)
}

func (m model) repoName(p string) string {
	if p == "" {
		return "no project"
	}
	return filepath.Base(p)
}

// stateRank orders groups the way Linear does: work in progress first.
func stateRank(t string) int {
	switch t {
	case "started":
		return 0
	case "unstarted":
		return 1
	case "triage":
		return 2
	case "backlog":
		return 3
	}
	return 4
}

// refilter rebuilds the menu rows from the query: New (when typing), issues grouped by state, Clean.
func (m *model) refilter() {
	q := strings.TrimSpace(m.input.Value())
	lq := strings.ToLower(q)
	words := strings.Fields(lq)
	m.rows = m.rows[:0]
	if q != "" {
		m.rows = append(m.rows, row{kind: rowNew})
	}
	// Group matching issues by state: groups in Linear's order, issues in the CLI's order.
	var order []string
	groups := map[string][]Issue{}
	for _, is := range m.issues {
		hay := strings.ToLower(is.ID + " " + is.Title)
		ok := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if _, seen := groups[is.State]; !seen {
			order = append(order, is.State)
		}
		groups[is.State] = append(groups[is.State], is)
	}
	for i := 1; i < len(order); i++ { // insertion sort by rank, stable
		for j := i; j > 0 && stateRank(groups[order[j]][0].StateType) < stateRank(groups[order[j-1]][0].StateType); j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	for _, st := range order {
		g := groups[st]
		m.rows = append(m.rows, row{kind: rowGroup, issue: g[0], count: len(g)})
		for _, is := range g {
			m.rows = append(m.rows, row{kind: rowIssue, issue: is})
		}
	}
	if m.repo != "" && (q == "" || strings.Contains("clean worktrees", lq)) {
		m.rows = append(m.rows, row{kind: rowClean})
	}
	if m.cursor >= len(m.rows) {
		m.cursor = max(0, len(m.rows)-1)
	}
	m.settle(1)
}

// settle moves the cursor off a group header, in direction d, or the other way at an end.
func (m *model) settle(d int) {
	for _, dir := range []int{d, -d} {
		for c := m.cursor; c >= 0 && c < len(m.rows); c += dir {
			if m.rows[c].selectable() {
				m.cursor = c
				return
			}
		}
	}
}

func (m *model) move(d int) {
	for c := m.cursor + d; c >= 0 && c < len(m.rows); c += d {
		if m.rows[c].selectable() {
			m.cursor = c
			return
		}
	}
}

func (m model) matchedIssues() int {
	n := 0
	for _, r := range m.rows {
		if r.kind == rowIssue {
			n++
		}
	}
	return n
}

// wantDetail starts fetching the highlighted issue's description, once.
func (m *model) wantDetail() tea.Cmd {
	if m.cursor >= len(m.rows) || m.rows[m.cursor].kind != rowIssue {
		return nil
	}
	id := m.rows[m.cursor].issue.ID
	if m.detailWanted[id] {
		return nil
	}
	m.detailWanted[id] = true
	repo := m.repo
	return func() tea.Msg { return detailMsg{id, loadDetail(repo, id)} }
}

// layout sizes the form's inputs to the window.
func (m *model) layout() {
	w := max(10, min(m.width, formMaxWidth)-formLabelWidth-22) // room for the badge column
	m.inTitle.Width, m.inBranch.Width, m.inHint.Width = w, w, w
	m.inPrompt.SetWidth(w)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case issuesMsg:
		m.issuesLoading = false
		m.issues = msg.issues
		if msg.err != nil {
			m.issuesErr = msg.err.Error()
		}
		m.refilter()
		return m, m.wantDetail()
	case detailMsg:
		m.details[msg.id] = msg.detail
		return m, nil
	case namesMsg:
		return m.gotNames(msg)
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		m.flash = ""
		switch m.screen {
		case scMenu:
			return m.updateMenu(msg)
		case scForm:
			return m.updateForm(msg)
		case scPick:
			return m.updatePick(msg)
		}
	}
	return m, nil
}

func (m model) updateMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m, tea.Quit
	case "up", "ctrl+p", "shift+tab":
		m.move(-1)
		return m, m.wantDetail()
	case "down", "ctrl+n", "tab":
		m.move(1)
		return m, m.wantDetail()
	case "alt+c":
		return m.chooseClean()
	case "enter":
		if m.cursor >= len(m.rows) {
			return m, nil
		}
		switch r := m.rows[m.cursor]; r.kind {
		case rowNew:
			return m.openForm(strings.TrimSpace(m.input.Value()), "")
		case rowIssue:
			return m.openForm(r.issue.ID, r.issue.ID)
		case rowClean:
			return m.chooseClean()
		}
		return m, nil
	}
	var cmd tea.Cmd
	before := m.input.Value()
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m.cursor = 0
		m.refilter()
		return m, tea.Batch(cmd, m.wantDetail())
	}
	return m, cmd
}

func (m model) chooseClean() (tea.Model, tea.Cmd) {
	if m.repo == "" {
		m.flash = "not in a git repository: nothing to clean here"
		return m, nil
	}
	m.action = &Action{Kind: "clean", Dir: m.repo, Args: []string{m.repo}}
	return m, tea.Quit
}

func (m model) issue(id string) (Issue, bool) {
	for _, is := range m.issues {
		if is.ID == id {
			return is, true
		}
	}
	return Issue{}, false
}

func (m model) openForm(input, issueID string) (tea.Model, tea.Cmd) {
	m.form = form{input: input, issueID: issueID, project: m.repo, agent: m.defaultAgent(m.repo)}
	prompt := input
	if is, ok := m.issue(issueID); ok {
		prompt = "Work on Linear " + issueID + ": " + is.Title + "."
	}
	m.inTitle.SetValue("")
	m.inBranch.SetValue("")
	m.inHint.SetValue("")
	m.inPrompt.SetValue(prompt)
	if m.form.project == "" {
		m.form.cursor = fProject
	}
	m.screen = scForm
	return m, m.focusField()
}

// focusField gives the keyboard to the text field under the cursor, if any.
func (m *model) focusField() tea.Cmd {
	m.inTitle.Blur()
	m.inBranch.Blur()
	m.inHint.Blur()
	m.inPrompt.Blur()
	switch m.form.cursor {
	case fTitle:
		m.inTitle.CursorEnd()
		return m.inTitle.Focus()
	case fBranch:
		m.inBranch.CursorEnd()
		return m.inBranch.Focus()
	case fHint:
		m.inHint.CursorEnd()
		return m.inHint.Focus()
	case fPrompt:
		m.inPrompt.CursorEnd()
		return m.inPrompt.Focus()
	}
	return nil
}

// defaultAgent is the agent last used in repo, else the first one found.
func (m model) defaultAgent(repo string) int {
	if last, ok := m.prefs[repo]; ok {
		for i, a := range m.agents {
			if a.Label == last {
				return i
			}
		}
	}
	return 0
}

// highlighted is what the highlighted menu row would name: the typed text, an issue ID, or "".
func (m model) highlighted() string {
	if m.cursor >= len(m.rows) {
		return ""
	}
	switch r := m.rows[m.cursor]; r.kind {
	case rowNew:
		return strings.TrimSpace(m.input.Value())
	case rowIssue:
		return r.issue.ID
	}
	return ""
}

// ensureNames starts a naming job unless one for the same key is running or has succeeded.
func (m *model) ensureNames(dir, input, hint string, agent Agent) tea.Cmd {
	k := nameKey(dir, input, hint, agent)
	if j, ok := m.jobs[k]; ok && (!j.done || j.err == nil) {
		return nil
	}
	m.jobs[k] = &nameJob{start: time.Now()}
	return func() tea.Msg {
		n, err := resolveNames(dir, input, hint, agent)
		return namesMsg{k, n, err}
	}
}

// forgetNames drops names Claude gave for another project or naming hint, keeping your edits.
func (m *model) forgetNames() {
	f := &m.form
	f.names, f.naming, f.queued, f.key, f.nameErr = Names{}, false, false, "", ""
	if !f.edited[fTitle] {
		m.inTitle.SetValue("")
	}
	if !f.edited[fBranch] {
		m.inBranch.SetValue("")
	}
}

// startNaming attaches the form to its naming job, starting one if the menu has not already.
func (m *model) startNaming() tea.Cmd {
	f := &m.form
	if f.project == "" || len(m.agents) == 0 {
		return nil
	}
	f.nameErr, f.queued = "", false
	agent := m.agents[f.agent]
	hint := strings.TrimSpace(m.inHint.Value())
	cmd := m.ensureNames(f.project, f.input, hint, agent)
	f.key = nameKey(f.project, f.input, hint, agent)
	if j := m.jobs[f.key]; j.done {
		f.naming = false
		m.applyNames(j.names)
	} else {
		f.naming = true
	}
	return cmd
}

// nameWait is how long a naming job has run, for the progress text.
func (m model) nameWait(key string) time.Duration {
	if j, ok := m.jobs[key]; ok && !j.done {
		return time.Since(j.start)
	}
	return 0
}

func (m model) gotNames(msg namesMsg) (tea.Model, tea.Cmd) {
	if j, ok := m.jobs[msg.key]; ok {
		j.done, j.names, j.err = true, msg.names, msg.err
	}
	f := &m.form
	if m.screen == scMenu || msg.key != f.key || !f.naming {
		return m, nil
	}
	f.naming = false
	if msg.err != nil {
		f.nameErr = msg.err.Error()
		f.queued = false
		return m, nil
	}
	m.applyNames(msg.names)
	if f.queued {
		return m.create()
	}
	return m, nil
}

// applyNames fills the title and branch you have not changed; edits always win.
func (m *model) applyNames(n Names) {
	f := &m.form
	f.names = n
	if !f.edited[fTitle] {
		m.inTitle.SetValue(n.Title)
		m.inTitle.CursorEnd()
	}
	if !f.edited[fBranch] {
		m.inBranch.SetValue(n.Branch)
		m.inBranch.CursorEnd()
	}
	// The prompt stays as shown: names arrive only on Create, too late to change what you saw.
}

func (m model) fieldText(f int) string {
	switch f {
	case fTitle:
		return m.inTitle.Value()
	case fBranch:
		return m.inBranch.Value()
	case fHint:
		return m.inHint.Value()
	case fPrompt:
		return m.inPrompt.Value()
	}
	return ""
}

func (m model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := &m.form
	switch msg.String() {
	case "ctrl+s":
		return m.create()
	case "esc":
		if f.queued { // first esc only cancels the pending Create
			f.queued = false
			return m, nil
		}
		m.screen = scMenu
		m.inTitle.Blur()
		m.inBranch.Blur()
		m.inHint.Blur()
		m.inPrompt.Blur()
		return m, nil
	case "up", "shift+tab":
		if f.cursor > 0 {
			f.cursor--
		}
		return m, m.focusField()
	case "down", "tab":
		if f.cursor < fieldCount-1 {
			f.cursor++
		}
		return m, m.focusField()
	case "enter":
		switch f.cursor {
		case fCreate:
			return m.create()
		case fBase, fProject, fAgent:
			m.openPick(f.cursor)
			return m, nil
		}
		f.cursor++ // a text field: on to the next
		return m, m.focusField()
	case "left", "right":
		if c := f.cursor; c == fBase || c == fProject || c == fAgent {
			d := 1
			if msg.String() == "left" {
				d = -1
			}
			return m, m.cycle(c, d)
		}
	}
	if !isText(f.cursor) {
		return m, nil
	}
	before := m.fieldText(f.cursor)
	var cmd tea.Cmd
	switch f.cursor {
	case fTitle:
		m.inTitle, cmd = m.inTitle.Update(msg)
	case fBranch:
		m.inBranch, cmd = m.inBranch.Update(msg)
	case fHint:
		m.inHint, cmd = m.inHint.Update(msg)
	case fPrompt:
		m.inPrompt, cmd = m.inPrompt.Update(msg)
	}
	if m.fieldText(f.cursor) != before {
		f.edited[f.cursor] = true
		if f.cursor == fHint { // names given under the old hint no longer hold: Create asks again
			m.forgetNames()
		}
	}
	return m, cmd
}

// choices lists a choice field's options and the index of its current value.
func (m *model) choices(field int) ([]string, int) {
	f := &m.form
	var opts []string
	cur := 0
	switch field {
	case fBase:
		if _, ok := m.bases[f.project]; !ok {
			m.bases[f.project] = recentBranches(f.project)
		}
		opts = append([]string{""}, m.bases[f.project]...)
		for i, b := range opts {
			if b == f.base {
				cur = i
			}
		}
	case fProject:
		if m.projects == nil {
			m.projects = listProjects(m.repo)
		}
		opts = m.projects
		for i, p := range opts {
			if p == f.project {
				cur = i
			}
		}
	case fAgent:
		for _, a := range m.agents {
			opts = append(opts, a.Label)
		}
		cur = f.agent
	}
	return opts, cur
}

// choose sets a choice field to option i; a new project means new names.
func (m *model) choose(field, i int) tea.Cmd {
	opts, _ := m.choices(field)
	if i < 0 || i >= len(opts) {
		return nil
	}
	f := &m.form
	switch field {
	case fBase:
		f.base = opts[i]
	case fAgent:
		f.agent = i
	case fProject:
		if f.project != opts[i] {
			// Names depend on the repo (its branches, what exists): Create asks again.
			m.forgetNames()
		}
		f.project, f.base = opts[i], ""
	}
	return nil
}

func (m *model) cycle(field, d int) tea.Cmd {
	opts, cur := m.choices(field)
	if len(opts) == 0 {
		return nil
	}
	return m.choose(field, (cur+d+len(opts))%len(opts))
}

func (m *model) openPick(field int) {
	opts, cur := m.choices(field)
	m.pick = pick{field: field, options: opts, cursor: cur}
	m.screen = scPick
}

func (m model) updatePick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.pick
	switch msg.String() {
	case "esc":
		m.screen = scForm
	case "up", "ctrl+p", "shift+tab":
		if p.cursor > 0 {
			p.cursor--
		}
	case "down", "ctrl+n", "tab":
		if p.cursor < len(p.options)-1 {
			p.cursor++
		}
	case "enter":
		m.screen = scForm
		return m, m.choose(p.field, p.cursor)
	}
	return m, nil
}

func (m model) create() (tea.Model, tea.Cmd) {
	f := m.form
	title, branch := strings.TrimSpace(m.inTitle.Value()), strings.TrimSpace(m.inBranch.Value())
	switch {
	case len(m.agents) == 0:
		m.flash = "no agent CLI found on PATH (claude, codex, ...)"
		return m, nil
	case f.project == "":
		m.flash = "pick a project first"
		return m, nil
	case (title == "" || branch == "") && (f.naming || f.names.Branch == ""):
		// Claude names only on Create (naming every highlighted row costs too much): ask,
		// then launch from gotNames when the names arrive.
		cmd := m.startNaming()
		m.form.queued = true
		return m, cmd
	case !validBranch(branch):
		m.flash = "branch name is not valid: " + branch
		return m, nil
	case title == "":
		m.flash = "title is empty"
		return m, nil
	}
	agent := m.agents[f.agent]
	args := []string{"--detach", "-B", branch, "--title", title, "--prompt", strings.TrimSpace(m.inPrompt.Value())}
	args = append(args, agent.Args...)
	if f.base != "" {
		args = append(args, "-b", f.base)
	}
	ticket := f.issueID
	if ticket == "" && f.names.Kind == "ticket" {
		ticket = f.names.ID // typed ENG-123 or a linear.app link into the New row
	}
	if ticket != "" {
		args = append(args, "--", ticket)
	}
	m.prefs[f.project] = agent.Label // saved by main, and only on a real run
	m.action = &Action{Kind: "create", Dir: f.project, Args: args}
	return m, tea.Quit
}
