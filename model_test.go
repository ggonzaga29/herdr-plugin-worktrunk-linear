package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func testModel() model {
	m := newModel("")
	m.repo = "/repo"
	m.agents = []Agent{{"claude", nil}, {"claude · team", []string{"-a", "team"}}}
	m.prefs = prefs{}
	return m
}

func TestOpeningTheFormNames(t *testing.T) {
	m := testModel()
	m.issues = sampleIssues()
	m.refilter()
	for range m.rows {
		m.move(1)
	}
	if len(m.jobs) != 0 {
		t.Fatal("moving through the menu must not ask Claude")
	}
	tm, cmd := m.openForm("fix the login loop", "")
	m = tm.(model)
	if !m.form.naming || len(m.jobs) != 1 || cmd == nil {
		t.Fatalf("opening the form should ask Claude once: naming=%v jobs=%d", m.form.naming, len(m.jobs))
	}
	tm, _ = m.create()
	m = tm.(model)
	if !m.form.queued || m.action != nil || len(m.jobs) != 1 {
		t.Fatalf("Create should wait for the same answer: queued=%v action=%v jobs=%d", m.form.queued, m.action, len(m.jobs))
	}

	// An answer for some other input changes nothing.
	other := namesMsg{key: nameKey("/repo", "something else", "", m.agents[0]), names: Names{Branch: "wrong", Title: "Wrong"}}
	tm, _ = m.gotNames(other)
	m = tm.(model)
	if m.action != nil || m.inBranch.Value() == "wrong" {
		t.Fatal("a stale answer must not fill the form or launch")
	}

	tm, cmd = m.gotNames(namesMsg{key: m.form.key, names: Names{Branch: "fix/login-loop", Title: "Fix login loop"}})
	m = tm.(model)
	if m.action == nil || cmd == nil {
		t.Fatal("the answer should launch the waiting Create")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("launch should quit the TUI")
	}
	want := []string{"--detach", "-B", "fix/login-loop", "--title", "Fix login loop", "--prompt", "fix the login loop"}
	if !reflect.DeepEqual(m.action.Args, want) {
		t.Fatalf("args = %q, want %q", m.action.Args, want)
	}
}

func TestTypedNamesSkipClaude(t *testing.T) {
	m := testModel()
	tm, _ := m.openForm("fix the login loop", "")
	m = tm.(model)
	m.inTitle.SetValue("Login loop")
	m.inBranch.SetValue("fix/login-loop")
	tm, _ = m.create()
	m = tm.(model)
	if m.action == nil {
		t.Fatal("with title and branch filled in, Create should launch without waiting for Claude")
	}
}

func TestTypingBeatsLateNames(t *testing.T) {
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
	tm, _ = m.updateForm(tea.KeyMsg{Type: tea.KeyCtrlS}) // title is empty: Claude names it
	m = tm.(model)
	if !m.form.queued {
		t.Fatal("ctrl+s with an empty title should name, then create")
	}
	tm, _ = m.gotNames(namesMsg{key: m.form.key, names: Names{Branch: "fix/claude", Title: "Claude title"}})
	m = tm.(model)
	if m.action == nil || m.inBranch.Value() != "fix/mine" || m.inTitle.Value() != "Claude title" {
		t.Fatalf("names must fill only untouched fields: branch=%q title=%q", m.inBranch.Value(), m.inTitle.Value())
	}
}

// The Naming field is sent to Claude, and changing it asks again.
func TestNamingHintRenames(t *testing.T) {
	m := testModel()
	tm, _ := m.openForm("fix the login loop", "")
	m = tm.(model)
	typeHint := func(s string) {
		m.form.cursor = fHint
		m.focusField()
		for _, r := range s {
			tm, _ = m.updateForm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			m = tm.(model)
		}
	}
	typeHint("start with web")
	tm, _ = m.create()
	m = tm.(model)
	if want := nameKey("/repo", "fix the login loop", "start with web", m.agents[0]); m.form.key != want || m.jobs[want] == nil {
		t.Fatalf("Create should name with the hint: key=%q", m.form.key)
	}
	m.form.queued = false // names arrive, but you keep editing
	tm, _ = m.gotNames(namesMsg{key: m.form.key, names: Names{Branch: "fix/web-login-loop", Title: "Fix web login loop"}})
	m = tm.(model)
	if m.inBranch.Value() != "fix/web-login-loop" {
		t.Fatalf("names should fill the form: branch=%q", m.inBranch.Value())
	}
	typeHint("-app")
	if m.inBranch.Value() != "" || m.form.names.Branch != "" {
		t.Fatal("a changed hint should drop the old names")
	}
	m.form.cursor = fBase // leaving the Naming field asks again, under the new hint
	m.focusField()
	if want := nameKey("/repo", "fix the login loop", "start with web-app", m.agents[0]); !m.form.naming || m.form.key != want {
		t.Fatalf("leaving the Naming field should ask Claude again: naming=%v key=%q", m.form.naming, m.form.key)
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

func TestClaudeAccountsAreConfigDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, d := range []string{".claude", ".claude-team", ".claude-work", ".claude-worktrees"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{".claude-work", ".claude-team"} { // worktrees holds no .claude.json
		if err := os.WriteFile(filepath.Join(home, d, ".claude.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := claudeAccounts(), []string{"team", "work"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("claudeAccounts() = %q, want %q", got, want)
	}
}
