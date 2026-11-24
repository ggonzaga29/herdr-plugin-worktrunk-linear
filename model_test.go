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
	if m.action != nil || m.form.branch == "wrong" {
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
	tm, cmd := m.openForm("add dark mode", "")
	m = tm.(model)
	if m.form.naming || cmd != nil || m.form.branch != "feat/dark-mode" {
		t.Fatalf("a finished prefetch should fill the form with no wait: naming=%v branch=%q", m.form.naming, m.form.branch)
	}
}
