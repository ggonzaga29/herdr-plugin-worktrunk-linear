# herdr-plugin-worktrunk-linear

One [herdr](https://herdr.dev) popup to start work or clean up, built on
[worktrunk](https://worktrunk.dev) and Linear:

- **New work.** Pick one of your Linear issues, or type what you want to do. Claude (Haiku, ~4 s)
  names the branch and a short workspace title; a form shows every field filled in, editable.
  Create runs worktrunk (its hooks included), opens the worktree as a herdr workspace, starts your
  agent, and types the prompt for you to review. The popup closes as soon as the agent launches.
- **Clean.** Shows what would go — idle worktrees, stale records, empty leftover folders, Xcode
  caches for deleted worktrees — with sizes and the reason for every skip, and asks before deleting.

## Requirements

- herdr 0.9.5+, macOS
- `wt` (worktrunk), `git`, `jq`, Go (to build the menu)
- Optional: the `linear` CLI (for issues; a repo needs a `.linear.toml`, made with `linear config`),
  `claude` (for naming; without it, branches are plain slugs)

## Install

```sh
git clone https://github.com/ggonzaga29/herdr-plugin-worktrunk-linear.git
cd herdr-plugin-worktrunk-linear
go build -o build/herdr-wtl .
herdr plugin link "$PWD"
```

Bind a key in `~/.config/herdr/config.toml`, then `herdr server reload-config`:

```toml
[[keys.command]]
key = "prefix+t"
type = "plugin_action"
command = "ggonzaga29.worktrunk-linear.menu"
description = "worktrees: new or clean"
```

Optional: put the engines on your PATH for terminal use:
`ln -s "$PWD/bin/herdr-start" "$PWD/bin/herdr-clean" ~/.local/bin/`.

## The menu

| Key | Does |
| --- | --- |
| type | filters issues; a `+ New worktree: "…"` row starts work from what you typed |
| enter | open the form (issue or new) or Clean |
| alt-c | Clean, from anywhere |
| esc | close (or back, in the form) |

In the form, Enter on a row edits it (Title, Branch, Prompt) or opens a list (Base, Project,
Agent); Enter on Create launches. Agents: `claude` per account (any `claude-<name>` wrapper on
PATH, e.g. `claude-team`), plus codex, gemini, opencode and others found on PATH. The last agent
per repo is remembered.

## The engines

The menu is a thin UI over two bash scripts in `bin/`, usable on their own (`-h` for details):

- `herdr-start` — ticket, task or branch → worktrunk worktree → herdr workspace → agent with the
  prompt typed. `--names INPUT` prints how it would name something, as JSON; `--detach` finishes
  in the background (log: `~/.cache/herdr-start.log`, problems as herdr notifications).
- `herdr-clean` — plan, confirm, clean. `-n` to only look.

## Naming

Claude (the latest Sonnet, ~3 s) reads the task or ticket and answers with a **type** (`feat`,
`fix`, ...), a **slug** (`empty-wallet-list-crash`) and a sidebar **title**. The branch is built
from your format, so it never picks up someone's name from Linear or other branches.

- A ticket that already has a branch carrying its ID (local or remote) reuses it, so earlier work
  is picked up. Otherwise its branch is built from the format; the ID is always kept in the name,
  since Linear links branches and PRs by it.
- A new task branch that already exists gets `-2`, `-3`, ...
- An exact branch name (no spaces) is used as typed.
- No answer from Claude within 30 s: a plain slug, still in your format.

Configure with git config, per repo or `--global` (an env var of the same name wins):

| Key | Default | |
| --- | --- | --- |
| `herdr-start.branchFormat` | `{type}/{id}-{slug}` | `{id}` is empty for tasks and drops out with its separator: `fix/empty-wallet-list-crash`, `fix/eng-123-empty-wallet-list-crash` |
| `herdr-start.branchTypes` | `feat fix chore refactor docs test perf ci` | the types Claude may choose |
| `herdr-start.ticketBranch` | `format` | `linear`: use Linear's own branch name for tickets, as is |
| `herdr-start.model` | `sonnet` | any `claude --model` value, e.g. `haiku` for speed |

```sh
git config --global herdr-start.branchFormat '{id}-{slug}'      # eng-123-empty-wallet-list-crash
git config herdr-start.branchFormat '{type}/{slug}'             # this repo only; tickets still get their ID
```
