package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Every screen, at small, medium and large sizes: no panic, no line wider than the terminal.
func TestViewsFit(t *testing.T) {
	for _, sz := range [][2]int{{60, 15}, {80, 24}, {120, 40}, {200, 60}} {
		m := testModel()
		m.issues = sampleIssues()
		m.issuesLoading = false
		tm, _ := m.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		m = tm.(model)
		m.refilter()
		check := func(name string, mm model) {
			out := mm.View()
			lines := strings.Split(out, "\n")
			if len(lines) > sz[1] {
				t.Errorf("%dx%d %s: %d lines, more than the height", sz[0], sz[1], name, len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w > sz[0] {
					t.Errorf("%dx%d %s: line %d is %d wide: %q", sz[0], sz[1], name, i, w, ansi.Strip(l))
				}
			}
		}
		check("menu", m)
		mt := m
		mt.missing = []string{"wt not found: brew install worktrunk", "linear not found: brew install schpet/tap/linear, then linear auth login"}
		mt.cursor = len(mt.rows) - 1 // scrolled to the bottom with the warnings above
		check("menu missing tools", mt)
		m.input.SetValue("make settings remember the tab")
		m.refilter()
		check("menu typing", m)
		tm, _ = m.openForm("ENG-1", "ENG-1")
		fm := tm.(model)
		check("form issue", fm)
		fm.form.cursor = fPrompt
		fm.focusField()
		check("form prompt", fm)
		tm, _ = m.openForm("make settings remember the tab", "")
		check("form task", tm.(model))
		fm.openPick(fAgent)
		check("pick", fm)
	}
}
