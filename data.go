package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Issue is one Linear issue in the menu.
type Issue struct {
	ID            string
	Title         string
	State         string
	StateType     string // triage, backlog, unstarted, started, completed, canceled
	StateColor    string // Linear's hex colour for the state
	Priority      int    // 0 none, 1 urgent, 2 high, 3 medium, 4 low
	PriorityLabel string
	Labels        []Label
}

// Label is a Linear label with its hex colour.
type Label struct{ Name, Color string }

// IssueDetail is fetched when an issue is highlighted (the list query has no branch or description).
type IssueDetail struct {
	Branch      string
	Description string
	Err         string
}

// Names is herdr-start --names output: how the engine resolved a ticket, task or branch.
type Names struct {
	Kind         string `json:"kind"`
	ID           string `json:"id"`
	Branch       string `json:"branch"`
	Title        string `json:"title"`
	Prompt       string `json:"prompt"`
	BranchSource string `json:"branch_source"`
	TitleSource  string `json:"title_source"`
	Where        string `json:"where"`
}

// Agent is one choice in the form's Agent row: a label and the herdr-start flags it maps to.
type Agent struct {
	Label string
	Args  []string
}

// run executes a command in dir and returns stdout; on failure the error carries stderr's first line.
func run(dir, name string, args ...string) (string, error) {
	return runFor(0, dir, name, args...)
}

// runFor is run with a time limit (0 = none), so a hung network call cannot block the menu.
func runFor(limit time.Duration, dir, name string, args ...string) (string, error) {
	ctx := context.Background()
	if limit > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, limit)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 2 * time.Second // children holding the pipes open must not outlive the limit
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return out.String(), fmt.Errorf("%s took longer than %s", filepath.Base(name), limit)
		}
		msg := firstLine(stripANSI(errb.String()))
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), errors.New(msg)
	}
	return out.String(), nil
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// clean makes text written by other people (Linear titles, descriptions) safe to draw: it drops
// every control character (C0 including ESC, DEL, C1), so no escape sequence reaches the terminal.
// Tabs become spaces; newlines are kept only when keepNewlines is set.
func clean(s string, keepNewlines bool) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' && keepNewlines:
			return r
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || r == utf8.RuneError:
			return -1 // controls, and invalid bytes (a raw 0x9b is CSI to an 8-bit terminal)
		}
		return r
	}, s)
}

var hexRE = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// hexColor keeps a colour only if it is a plain #rrggbb (it is drawn as a terminal colour).
func hexColor(c string) string {
	if hexRE.MatchString(c) {
		return c
	}
	return ""
}

// palette is the popup's colours: herdr's [theme.custom] when set, else these defaults.
type palette struct {
	text, muted, faint, dim, accent, sel, surface, red, yellow, green string
}

var themeLineRE = regexp.MustCompile(`^\s*([a-z0-9_]+)\s*=\s*"(#[0-9A-Fa-f]{6})"`)

// loadTheme reads the colour keys of herdr's [theme.custom] table, so the popup matches herdr.
// ponytail: a line regex, not a TOML parser; only flat `key = "#rrggbb"` lines are read.
func loadTheme() palette {
	p := palette{text: "#e5e5e5", muted: "#a3a3a3", faint: "#737373", dim: "#525252", accent: "#60a5fa",
		sel: "#303030", surface: "#222222", red: "#f87171", yellow: "#facc15", green: "#4ade80"}
	home, _ := os.UserHomeDir()
	b, err := os.ReadFile(filepath.Join(home, ".config", "herdr", "config.toml"))
	if err != nil {
		return p
	}
	slots := map[string]*string{"text": &p.text, "subtext0": &p.muted, "overlay1": &p.faint, "overlay0": &p.dim,
		"accent": &p.accent, "selection_bg": &p.sel, "surface0": &p.surface, "red": &p.red, "yellow": &p.yellow, "green": &p.green}
	in := false
	for _, l := range strings.Split(string(b), "\n") {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "[") {
			in = t == "[theme.custom]"
			continue
		}
		if m := themeLineRE.FindStringSubmatch(l); in && m != nil {
			if dst, ok := slots[m[1]]; ok {
				*dst = m[2]
			}
		}
	}
	return p
}

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

// engine finds a bundled script: the plugin's bin/ first, then next to this binary, then PATH.
func engine(name string) string {
	var dirs []string
	if root := os.Getenv("HERDR_PLUGIN_ROOT"); root != "" {
		dirs = append(dirs, filepath.Join(root, "bin"))
	}
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "..", "bin"), filepath.Dir(exe))
	}
	for _, d := range dirs {
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return name
}

// startDir is the folder of the pane you were in when the menu opened.
func startDir() string {
	var ctx struct {
		FocusedPaneCwd string `json:"focused_pane_cwd"`
	}
	if raw := os.Getenv("HERDR_PLUGIN_CONTEXT_JSON"); raw != "" && json.Unmarshal([]byte(raw), &ctx) == nil && ctx.FocusedPaneCwd != "" {
		return ctx.FocusedPaneCwd
	}
	if d := os.Getenv("HERDR_ACTIVE_PANE_CWD"); d != "" {
		return d
	}
	d, _ := os.Getwd()
	return d
}

// repoRoot is the main checkout of the repo that holds dir (any worktree or subfolder), or "".
func repoRoot(dir string) string {
	out, err := run(dir, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(firstLine(out), "worktree ")
}

func loadIssues(repo string) ([]Issue, error) {
	if _, err := exec.LookPath("linear"); err != nil {
		return nil, errors.New("linear CLI not installed")
	}
	out, err := runFor(30*time.Second, repo, "linear", "issue", "query", "--json", "--assignee", "self",
		"--state", "started", "--state", "unstarted", "--limit", "100")
	if err != nil {
		return nil, err
	}
	var res struct {
		Nodes []struct {
			Identifier    string `json:"identifier"`
			Title         string `json:"title"`
			Priority      int    `json:"priority"`
			PriorityLabel string `json:"priorityLabel"`
			State         struct {
				Name  string `json:"name"`
				Type  string `json:"type"`
				Color string `json:"color"`
			} `json:"state"`
			Labels struct {
				Nodes []struct {
					Name  string `json:"name"`
					Color string `json:"color"`
				} `json:"nodes"`
			} `json:"labels"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, errors.New("could not read linear's output")
	}
	issues := make([]Issue, 0, len(res.Nodes))
	for _, n := range res.Nodes {
		is := Issue{
			ID: clean(n.Identifier, false), Title: clean(n.Title, false),
			State: clean(n.State.Name, false), StateType: n.State.Type, StateColor: hexColor(n.State.Color),
			Priority: n.Priority, PriorityLabel: clean(n.PriorityLabel, false),
		}
		for _, l := range n.Labels.Nodes {
			is.Labels = append(is.Labels, Label{clean(l.Name, false), hexColor(l.Color)})
		}
		issues = append(issues, is)
	}
	return issues, nil
}

func loadDetail(repo, id string) IssueDetail {
	out, err := runFor(30*time.Second, repo, "linear", "issue", "view", id, "--json", "--no-comments", "--no-download")
	if err != nil {
		return IssueDetail{Err: err.Error()}
	}
	var res struct {
		BranchName  string `json:"branchName"`
		Description string `json:"description"`
	}
	if json.Unmarshal([]byte(out), &res) != nil {
		return IssueDetail{Err: "could not read linear's output"}
	}
	return IssueDetail{Branch: clean(res.BranchName, false), Description: clean(res.Description, true)}
}

// resolveNames asks the engine how it would name this input, without creating anything.
func resolveNames(repo, input string, agent Agent) (Names, error) {
	args := append([]string{"--names"}, agent.Args...)
	args = append(args, "--", input)
	// Claude is capped at 15 s inside; this also covers a hung Linear lookup for tickets.
	out, err := runFor(45*time.Second, repo, engine("herdr-start"), args...)
	if err != nil {
		return Names{}, errors.New(strings.TrimPrefix(err.Error(), "herdr-start: "))
	}
	var n Names
	if err := json.Unmarshal([]byte(out), &n); err != nil {
		return Names{}, errors.New("could not read herdr-start's answer")
	}
	// herdr-start already strips these; belt and braces, since they are drawn and passed back.
	n.Title, n.Prompt, n.Branch = clean(n.Title, false), clean(n.Prompt, false), clean(n.Branch, false)
	return n, nil
}

func recentBranches(repo string) []string {
	out, _ := run(repo, "git", "for-each-ref", "--count=15", "--sort=-committerdate", "--format=%(refname:short)", "refs/heads")
	var bs []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			bs = append(bs, l)
		}
	}
	return bs
}

func validBranch(b string) bool {
	return b != "" && exec.Command("git", "check-ref-format", "--branch", b).Run() == nil
}

// listProjects: repos herdr has open, plus git repos in ~/workspace, current first.
func listProjects(current string) []string {
	seen := map[string]bool{}
	var ps []string
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		ps = append(ps, p)
	}
	add(current)
	var rest []string
	if out, err := run("", herdrBin(), "workspace", "list"); err == nil {
		var res struct {
			Result struct {
				Workspaces []struct {
					Worktree *struct {
						RepoRoot string `json:"repo_root"`
					} `json:"worktree"`
				} `json:"workspaces"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(out), &res) == nil {
			for _, w := range res.Result.Workspaces {
				if w.Worktree != nil && w.Worktree.RepoRoot != "" {
					rest = append(rest, w.Worktree.RepoRoot)
				}
			}
		}
	}
	home, _ := os.UserHomeDir()
	if entries, err := os.ReadDir(filepath.Join(home, "workspace")); err == nil {
		for _, e := range entries {
			p := filepath.Join(home, "workspace", e.Name())
			if st, err := os.Stat(filepath.Join(p, ".git")); err == nil && st.IsDir() {
				rest = append(rest, p)
			}
		}
	}
	sort.Strings(rest)
	for _, p := range rest {
		add(p)
	}
	return ps
}

func herdrBin() string {
	if b := os.Getenv("HERDR_BIN_PATH"); b != "" {
		return b
	}
	return "herdr"
}

var accountRE = regexp.MustCompile(`^claude-([a-z0-9]+)$`)

// listAgents: claude on each account (claude-<name> wrappers on PATH), then other agent CLIs.
func listAgents() []Agent {
	var agents []Agent
	if _, err := exec.LookPath("claude"); err == nil {
		agents = append(agents, Agent{"claude · personal", []string{"-a", "personal"}})
	}
	seen := map[string]bool{}
	var accounts []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			m := accountRE.FindStringSubmatch(e.Name())
			if m == nil || seen[m[1]] {
				continue
			}
			if st, err := os.Stat(filepath.Join(dir, e.Name())); err == nil && st.Mode()&0o111 != 0 {
				seen[m[1]] = true
				accounts = append(accounts, m[1])
			}
		}
	}
	sort.Strings(accounts)
	for _, a := range accounts {
		agents = append(agents, Agent{"claude · " + a, []string{"-a", a}})
	}
	for _, k := range []string{"codex", "gemini", "opencode", "cursor-agent", "amp", "pi"} {
		if _, err := exec.LookPath(k); err == nil {
			agents = append(agents, Agent{k, []string{"-k", k}})
		}
	}
	return agents
}

// prefs remembers the last agent per repo.
type prefs map[string]string

func prefsPath() string {
	dir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state", "herdr-plugin-worktrunk-linear")
	}
	return filepath.Join(dir, "prefs.json")
}

func loadPrefs() prefs {
	p := prefs{}
	if b, err := os.ReadFile(prefsPath()); err == nil {
		_ = json.Unmarshal(b, &p)
	}
	return p
}

func (p prefs) save() {
	path := prefsPath()
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	if b, err := json.MarshalIndent(p, "", "  "); err == nil {
		_ = os.WriteFile(path, b, 0o644)
	}
}
