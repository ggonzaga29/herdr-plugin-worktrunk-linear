package main

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
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
	rowIssue
	rowClean
)

type row struct {
	kind  rowKind
	issue Issue
}

// Form fields, in display order.
const (
	fCreate = iota
	fTitle
	fBranch
	fBase
	fProject
	fAgent
	fPrompt
	fieldCount
)

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
	title, branch  string
	prompt         string
	base           string // "" = worktrunk's default
	project        string
	agent          int
	edited         [fieldCount]bool // fields you changed: a late naming answer leaves them alone
	cursor         int
	editing        bool
	key            string // the naming job this form waits on (repo, input, agent)
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

func nameKey(dir, input string, a Agent) string { return dir + "\x00" + input + "\x00" + a.Label }

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
	prefs    prefs

	form        form
	jobs        map[string]*nameJob
	prefetchSeq int // debounce: only the latest highlight starts a naming job
	edit        textinput.Model
	editOrig    string // the field's value when its edit began
	pick        pick
	spin        spinner.Model
	flash       string

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
type prefetchMsg struct{ seq int }

func newModel(repo string) model {
	in := textinput.New()
	in.Placeholder = "type a task or a branch, or pick an issue"
	in.Prompt = ""
	in.PlaceholderStyle = stDim
	in.Focus()

	ed := textinput.New()
	ed.Prompt = ""

	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(stAccent))

	m := model{
		repo:          repo,
		input:         in,
		edit:          ed,
		spin:          sp,
		details:       map[string]IssueDetail{},
		detailWanted:  map[string]bool{},
		jobs:          map[string]*nameJob{},
		agents:        listAgents(),
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

// refilter rebuilds the menu rows from the query: New (when typing), matching issues, Clean.
func (m *model) refilter() {
	q := strings.TrimSpace(m.input.Value())
	lq := strings.ToLower(q)
	m.rows = m.rows[:0]
	if q != "" {
		m.rows = append(m.rows, row{kind: rowNew})
	}
	words := strings.Fields(lq)
	for _, is := range m.issues {
		hay := strings.ToLower(is.ID + " " + is.Title)
		ok := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			m.rows = append(m.rows, row{kind: rowIssue, issue: is})
		}
	}
	if m.repo != "" && (q == "" || strings.Contains("clean worktrees", lq)) {
		m.rows = append(m.rows, row{kind: rowClean})
	}
	if m.cursor >= len(m.rows) {
		m.cursor = max(0, len(m.rows)-1)
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

// wantDetail starts fetching the highlighted issue's branch and description, once.
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

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
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
		return m, tea.Batch(m.wantDetail(), m.schedulePrefetch())
	case prefetchMsg:
		return m, m.prefetch(msg.seq)
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
		if m.cursor > 0 {
			m.cursor--
		}
		return m, tea.Batch(m.wantDetail(), m.schedulePrefetch())
	case "down", "ctrl+n", "tab":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
		return m, tea.Batch(m.wantDetail(), m.schedulePrefetch())
	case "alt+c":
		return m.chooseClean()
	case "enter":
		if len(m.rows) == 0 {
			return m, nil
		}
		r := m.rows[m.cursor]
		switch r.kind {
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
		return m, tea.Batch(cmd, m.wantDetail(), m.schedulePrefetch())
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

func (m model) openForm(input, issueID string) (tea.Model, tea.Cmd) {
	f := form{input: input, issueID: issueID, project: m.repo, agent: m.defaultAgent(m.repo)}
	if issueID != "" {
		for _, is := range m.issues {
			if is.ID == issueID {
				f.title = issueID + " " + is.Title
				f.prompt = "Work on Linear " + issueID + ": " + is.Title + "."
			}
		}
	} else {
		f.prompt = input
	}
	if f.project == "" {
		f.cursor = fProject
	}
	m.form = f
	m.screen = scForm
	return m, m.startNaming()
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

// schedulePrefetch starts naming the highlighted row once you pause on it: longer while typing,
// so a half-typed task is not sent to Claude on every key.
func (m *model) schedulePrefetch() tea.Cmd {
	m.prefetchSeq++
	seq, wait := m.prefetchSeq, 350*time.Millisecond
	if m.cursor < len(m.rows) && m.rows[m.cursor].kind == rowNew {
		wait = 700 * time.Millisecond
	}
	return tea.Tick(wait, func(time.Time) tea.Msg { return prefetchMsg{seq} })
}

func (m *model) prefetch(seq int) tea.Cmd {
	in := m.highlighted()
	if seq != m.prefetchSeq || m.screen != scMenu || m.repo == "" || len(m.agents) == 0 || in == "" {
		return nil
	}
	return m.ensureNames(m.repo, in, m.agents[m.defaultAgent(m.repo)])
}

// ensureNames starts a naming job unless one for the same key is running or has succeeded.
func (m *model) ensureNames(dir, input string, agent Agent) tea.Cmd {
	k := nameKey(dir, input, agent)
	if j, ok := m.jobs[k]; ok && (!j.done || j.err == nil) {
		return nil
	}
	m.jobs[k] = &nameJob{start: time.Now()}
	return func() tea.Msg {
		n, err := resolveNames(dir, input, agent)
		return namesMsg{k, n, err}
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
	cmd := m.ensureNames(f.project, f.input, agent)
	f.key = nameKey(f.project, f.input, agent)
	if j := m.jobs[f.key]; j.done {
		f.naming = false
		m.applyNames(j.names)
	} else {
		f.naming = true
	}
	return cmd
}

// nameWait is how long the form's naming job has run, for the progress line.
func (m model) nameWait() time.Duration {
	if j, ok := m.jobs[m.form.key]; ok && !j.done {
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
	if f.queued && !f.editing {
		return m.create()
	}
	return m, nil
}

// applyNames fills the fields you have not changed; edits always win.
func (m *model) applyNames(n Names) {
	f := &m.form
	f.names = n
	fresh := map[int]string{fTitle: n.Title, fBranch: n.Branch, fPrompt: n.Prompt}
	for field, v := range fresh {
		if f.edited[field] {
			continue
		}
		// The field being edited: follow the answer only if nothing was typed yet.
		if f.editing && f.cursor == field && m.edit.Value() != m.editOrig {
			continue
		}
		switch field {
		case fTitle:
			f.title = v
		case fBranch:
			f.branch = v
		case fPrompt:
			f.prompt = v
		}
		if f.editing && f.cursor == field {
			m.edit.SetValue(v)
			m.edit.CursorEnd()
			m.editOrig = v
		}
	}
}

func (m model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := &m.form
	if f.editing {
		switch msg.String() {
		case "enter":
			if m.edit.Value() == m.editOrig { // nothing typed: keep the field as it is, still Claude's
				f.editing = false
				m.edit.Blur()
				return m, nil
			}
			v := strings.TrimSpace(m.edit.Value())
			switch f.cursor {
			case fTitle:
				f.title = v
			case fBranch:
				f.branch = v
			case fPrompt:
				f.prompt = v
			}
			f.edited[f.cursor] = true
			f.editing = false
			m.edit.Blur()
			return m, nil
		case "esc":
			f.editing = false
			m.edit.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.edit, cmd = m.edit.Update(msg)
		return m, cmd
	}
	switch msg.String() {
	case "esc":
		if f.queued { // first esc only cancels the pending Create
			f.queued = false
			return m, nil
		}
		m.screen = scMenu
		return m, nil
	case "up", "ctrl+p", "shift+tab":
		if f.cursor > 0 {
			f.cursor--
		}
	case "down", "ctrl+n", "tab":
		if f.cursor < fieldCount-1 {
			f.cursor++
		}
	case "enter":
		switch f.cursor {
		case fCreate:
			return m.create()
		case fTitle, fBranch, fPrompt:
			v := map[int]string{fTitle: f.title, fBranch: f.branch, fPrompt: f.prompt}[f.cursor]
			m.edit.SetValue(v)
			m.edit.CursorEnd()
			m.editOrig = v
			m.edit.Width = max(10, m.width/2-20)
			f.editing = true
			return m, m.edit.Focus()
		case fBase:
			opts := append([]string{"default (worktrunk)"}, recentBranches(f.project)...)
			m.pick = pick{field: fBase, options: opts}
			m.screen = scPick
		case fProject:
			if m.projects == nil {
				m.projects = listProjects(m.repo)
			}
			m.pick = pick{field: fProject, options: m.projects}
			for i, p := range m.projects {
				if p == f.project {
					m.pick.cursor = i
				}
			}
			m.screen = scPick
		case fAgent:
			var opts []string
			for _, a := range m.agents {
				opts = append(opts, a.Label)
			}
			m.pick = pick{field: fAgent, options: opts, cursor: f.agent}
			m.screen = scPick
		}
	}
	return m, nil
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
		if len(p.options) == 0 {
			m.screen = scForm
			return m, nil
		}
		f := &m.form
		m.screen = scForm
		switch p.field {
		case fBase:
			if p.cursor == 0 {
				f.base = ""
			} else {
				f.base = p.options[p.cursor]
			}
		case fAgent:
			f.agent = p.cursor
		case fProject:
			changed := f.project != p.options[p.cursor]
			f.project = p.options[p.cursor]
			f.base = ""
			// Names depend on the repo (its branch style, what exists): ask again for a new project.
			if changed || (f.names.Branch == "" && !f.naming) {
				return m, m.startNaming()
			}
		}
	}
	return m, nil
}

func (m model) create() (tea.Model, tea.Cmd) {
	f := m.form
	switch {
	case len(m.agents) == 0:
		m.flash = "no agent CLI found on PATH (claude, codex, ...)"
		return m, nil
	case f.project == "":
		m.flash = "pick a project first"
		return m, nil
	case f.naming && (!f.edited[fBranch] || !f.edited[fTitle]):
		m.form.queued = true // launches from gotNames when the names arrive
		return m, nil
	case !validBranch(f.branch):
		m.flash = "branch name is not valid: " + f.branch
		return m, nil
	case strings.TrimSpace(f.title) == "":
		m.flash = "title is empty"
		return m, nil
	}
	agent := m.agents[f.agent]
	args := []string{"--detach", "-B", f.branch, "--title", f.title, "--prompt", f.prompt}
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
