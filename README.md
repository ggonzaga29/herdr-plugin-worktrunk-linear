# herdr-plugin-worktrunk-linear

One [herdr](https://herdr.dev) popup to start work or clean up, built on
[worktrunk](https://worktrunk.dev) and Linear:

- **New work.** Pick one of your Linear issues, or type what you want to do. Claude names the
  branch and a short workspace title while you look (see [Naming](#naming)); the form shows every
  field filled in, and you can type over any of them. Create runs worktrunk (its hooks included),
  opens the worktree as a herdr workspace, starts your agent, and types the prompt for you to
  review. The popup closes as soon as the agent launches.
- **Clean.** Shows what would go — idle worktrees, stale records, empty leftover folders, Xcode
  caches for deleted worktrees — with sizes and the reason for every skip, and asks before deleting.

## Requirements

- herdr 0.9.5+ on macOS or Linux
- `git`, `jq`
- [worktrunk](https://worktrunk.dev) (`wt`): `brew install worktrunk`
- [linear-cli](https://github.com/schpet/linear-cli) (`linear`, tested with 2.5.0):
  `brew install schpet/tap/linear`, then `linear auth login`. A repo shows issues once it has a
  `.linear.toml` (run `linear config` in it); without one you can still start work from a typed task.
- Optional: [Claude Code](https://claude.com/claude-code) (`claude`) to name branches and titles;
  without it, branches are plain slugs. It runs on your Claude login, not an API key.

The menu lists any required tool it can't find, with the command to install it.

## Install

```sh
herdr plugin install ggonzaga29/herdr-plugin-worktrunk-linear
```

The install downloads the menu's prebuilt binary for your platform from the matching GitHub
release and checks it against `checksums.txt`. If there is none, it builds from source, which
needs [Go](https://go.dev/dl) 1.24+.

Then bind a key in `~/.config/herdr/config.toml` (herdr has no way for a plugin to bind one
itself) and run `herdr server reload-config`:

```toml
[[keys.command]]
key = "prefix+t"
type = "plugin_action"
command = "ggonzaga29.worktrunk-linear.menu"
description = "worktrees: new or clean"
```

Optional: to use the engines from a terminal too, clone the repo and
`ln -s "$PWD/bin/herdr-start" "$PWD/bin/herdr-clean" ~/.local/bin/`.

## The menu

| Key | Does |
| --- | --- |
| type | filters issues; a `+ New worktree "…"` row starts work from what you typed |
| ↑ ↓ | move |
| enter | open the form (issue or new) or Clean |
| alt-c | Clean, from anywhere |
| esc | close |

In the form:

| Key | Does |
| --- | --- |
| ↑ ↓, tab | move between fields |
| type | edit Title, Branch or Prompt in place; an edited field keeps your text when Claude answers |
| ← → | change Base, Project or Agent |
| enter | next field; opens the list on Base or Project; launches on Create |
| ctrl+s | create, from any field (waits for Claude's names if they are still coming) |
| esc | back to the menu |

### Agents and Claude accounts

The Agent field offers `claude`, one entry per extra Claude account, then codex, gemini,
opencode, cursor-agent, amp and pi when they are on PATH. The last agent per repo is remembered.

An extra Claude account is a config dir `~/.claude-<name>` that holds a `.claude.json`, which
is what running `CLAUDE_CONFIG_DIR=~/.claude-<name> claude` and logging in leaves behind. Each one
shows up as `claude · <name>` and starts with that `CLAUDE_CONFIG_DIR`. If a wrapper script
`claude-<name>` is on PATH, it is used instead, so a wrapper that does more (syncs memory,
checks something first) keeps working.

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
- The form's **Naming** field tells Claude how to name that one branch, in plain words
  ("start the slug with web"). On the command line: `herdr-start --naming "..."`.

Configure with git config, per repo or `--global` (an env var of the same name wins):

| Key | Default | |
| --- | --- | --- |
| `herdr-start.branchFormat` | `{type}/{id}-{slug}` | `{id}` is empty for tasks and drops out with its separator: `fix/empty-wallet-list-crash`, `fix/eng-123-empty-wallet-list-crash` |
| `herdr-start.branchTypes` | `feat fix chore refactor docs test perf ci` | the types Claude may choose |
| `herdr-start.ticketBranch` | `format` | `linear`: use Linear's own branch name for tickets, as is |
| `herdr-start.model` | `sonnet` | any `claude --model` value, e.g. `haiku` for speed |
| `herdr-start.namingPrompt` | none | your own naming instructions, in plain words; added to Claude's rules, and they win where the two disagree |

```sh
git config --global herdr-start.branchFormat '{id}-{slug}'      # eng-123-empty-wallet-list-crash
git config herdr-start.branchFormat '{type}/{slug}'             # this repo only; tickets still get their ID
git config herdr-start.namingPrompt 'Start the slug with the area it touches: web, api or ios.'
```

## Releasing

`scripts/release.sh 0.3.0` (from a clean `main`) sets the manifest version, builds the menu for
macOS and Linux on arm64 and amd64, commits `checksums.txt`, tags `v0.3.0`, pushes, and uploads
the binaries as a GitHub release. Bump the version whenever the Go code changes, so installs
get a binary that matches the scripts beside it.

For development, clone the repo, run `sh scripts/build.sh` (or `go build -o build/herdr-wtl .`),
and `herdr plugin link "$PWD"`; linking does not run the build.

## License

MIT
