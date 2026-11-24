// herdr-wtl: the herdr popup menu for worktrunk + Linear. One menu to start new work in a
// worktree (a Linear issue or a typed task, named by Claude) or to clean up idle worktrees.
// The bash engines in bin/ do the work: herdr-start and herdr-clean.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	dry := flag.Bool("dry-run", false, "print the command the chosen row would run, instead of running it")
	dir := flag.String("dir", "", "start in this folder instead of the focused pane's")
	flag.Parse()

	start := *dir
	if start == "" {
		start = startDir()
	}
	m := newModel(repoRoot(start))

	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "herdr-wtl:", err)
		os.Exit(1)
	}
	fm := final.(model)
	a := fm.action
	if a == nil {
		return
	}
	if !*dry && a.Kind == "create" {
		fm.prefs.save() // remember the agent for this repo
	}

	name := "herdr-start"
	if a.Kind == "clean" {
		name = "herdr-clean"
	}
	bin := engine(name)
	if *dry {
		fmt.Printf("would run in %s:\n  %s %s\n", a.Dir, name, quoteArgs(a.Args))
		return
	}

	// Hand the terminal over: worktrunk's hook approvals and herdr-clean's confirmation are interactive.
	cmd := exec.Command(bin, a.Args...)
	cmd.Dir = a.Dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	runErr := cmd.Run()

	if a.Kind == "clean" {
		// Keep the popup open so the result can be read.
		fmt.Print("\n[Enter to close] ")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	// herdr-start --detach holds the popup open on its own errors.
	if runErr != nil {
		os.Exit(1)
	}
}

func quoteArgs(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t'\"`$\\") {
			q[i] = strconv.Quote(a)
		} else {
			q[i] = a
		}
	}
	return strings.Join(q, " ")
}
