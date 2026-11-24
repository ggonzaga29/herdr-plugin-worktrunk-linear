package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func testModel() model {
	m := newModel("")
	m.repo = "/repo"
	m.agents = []Agent{{"claude · personal", []string{"-a", "personal"}}}
	m.prefs = prefs{}
	return m
}

func TestQueuedCreateLaunchesWhenNamesArrive(t *testing.T) {
	m := testModel()
	tm, _ := m.openForm("fix the login loop", "")
	m = tm.(model)
	if !m.form.naming {
		t.Fatal("form should be naming")
	}
	tm, _ = m.create() // Enter on Create while Claude is still naming
	m = tm.(model)
	if !m.form.queued || m.action != nil {
		t.Fatalf("Create while naming should queue, got queued=%v action=%v", m.form.queued, m.action)
	}

	// An answer for some other input (a prefetch of another row) changes nothing.
	other := namesMsg{key: nameKey("/repo", "something else", m.agents[0]), names: Names{Branch: "wrong", Title: "Wrong"}}
	tm, _ = m.gotNames(other)
	m = tm.(model)
	if m.action != nil || m.inBranch.Value() == "wrong" {
		t.Fatal("a stale answer must not fill the form or launch")
	}

	tm, cmd := m.gotNames(namesMsg{key: m.form.key, names: Names{Branch: "fix/login-loop", Title: "Fix login loop", Prompt: "fix the login loop"}})
	m = tm.(model)
	if m.action == nil || cmd == nil {
		t.Fatal("the answer should launch the queued Create")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("launch should quit the TUI")
	}
	want := []string{"--detach", "-B", "fix/login-loop", "--title", "Fix login loop", "--prompt", "fix the login loop", "-a", "personal"}
	if len(m.action.Args) != len(want) {
		t.Fatalf("args = %q, want %q", m.action.Args, want)
	}
	for i := range want {
		if m.action.Args[i] != want[i] {
			t.Fatalf("args = %q, want %q", m.action.Args, want)
		}
	}
}

func TestPrefetchedNamesFillFormAtOnce(t *testing.T) {
	m := testModel()
	k := nameKey("/repo", "add dark mode", m.agents[0])
	m.jobs[k] = &nameJob{done: true, names: Names{Branch: "feat/dark-mode", Title: "Add dark mode"}}
	tm, _ := m.openForm("add dark mode", "")
	m = tm.(model)
	if m.form.naming || len(m.jobs) != 1 || m.inBranch.Value() != "feat/dark-mode" {
		t.Fatalf("a finished prefetch should fill the form with no wait: naming=%v branch=%q", m.form.naming, m.inBranch.Value())
	}
}

func TestTypingMarksEditedAndBeatsLateNames(t *testing.T) {
	m := testModel()
	tm, _ := m.openForm("fix the login loop", "")
	m = tm.(model)
	m.form.cursor = fBranch
	m.focusField()
	for _, r := range "fix/mine" {
		tm, _ = m.updateForm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = tm.(model)
	}
	if !m.form.edited[fBranch] || m.inBranch.Value() != "fix/mine" {
		t.Fatalf("typing should edit the branch: edited=%v value=%q", m.form.edited[fBranch], m.inBranch.Value())
	}
	tm, _ = m.gotNames(namesMsg{key: m.form.key, names: Names{Branch: "fix/claude", Title: "Claude title"}})
	m = tm.(model)
	if m.inBranch.Value() != "fix/mine" || m.inTitle.Value() != "Claude title" {
		t.Fatalf("names must fill only untouched fields: branch=%q title=%q", m.inBranch.Value(), m.inTitle.Value())
	}
}

func TestCtrlSQueuesWhileNaming(t *testing.T) {
	m := testModel()
	tm, _ := m.openForm("fix the login loop", "")
	m = tm.(model)
	tm, _ = m.updateForm(tea.KeyMsg{Type: tea.KeyCtrlS})
	if m = tm.(model); !m.form.queued {
		t.Fatal("ctrl+s while naming should queue the create")
	}
}

func TestCursorSkipsGroupHeaders(t *testing.T) {
	m := testModel()
	m.issues = sampleIssues()
	m.refilter()
	if m.rows[m.cursor].kind != rowIssue {
		t.Fatalf("cursor should start on an issue, is on kind %d", m.rows[m.cursor].kind)
	}
	for i := 0; i < len(m.rows)*2; i++ {
		m.move(1)
		if !m.rows[m.cursor].selectable() {
			t.Fatal("cursor landed on a group header")
		}
	}
	if m.rows[0].kind != rowGroup || m.rows[0].issue.StateType != "started" {
		t.Fatal("started work should be the first group")
	}
}

func sampleIssues() []Issue {
	return []Issue{
		{ID: "ENG-1", Title: "Todo thing with a rather long title that will need truncating somewhere", State: "Todo", StateType: "unstarted", StateColor: "#e2e2e2", Priority: 2, PriorityLabel: "High", Labels: []Label{{"Feature", "#BB87FC"}, {"Mobile", "#0EA5E9"}, {"Parity", "#4EA7FC"}}},
		{ID: "ENG-2", Title: "In progress thing", State: "In Progress", StateType: "started", StateColor: "#f2c94c", Priority: 1, PriorityLabel: "Urgent"},
		{ID: "ENG-3", Title: "Another todo", State: "Todo", StateType: "unstarted"},
	}
}
