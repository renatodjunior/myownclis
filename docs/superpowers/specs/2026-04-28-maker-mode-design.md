# Maker Mode — Design Spec
Date: 2026-04-28

## Overview

New `maker` module for `moc` CLI. Serves as a personal command repository where the user saves, executes, schedules, and chains shell or moc commands. Commands are grouped by `cmdlet` (namespace). Chains sequence multiple commands. Both support cron-based scheduling (in-session goroutine + OS-level registration).

---

## Architecture

### New Go files

```
cmd/maker.go              # cobra commands, TUI shell, init
cmd/maker_store.go        # read/write commands and chains from disk
cmd/maker_scheduler.go    # in-session goroutine scheduler + OS scheduler
cmd/maker_log.go          # log writer with rotation and cleanup
```

Follows existing patterns from `sf.go` / `sf_tui.go`. No new dependencies — `bubbletea` and `bubbles` already present as indirect deps.

### Disk layout

```
~/.moc/
  commands/
    git/
      pull.yaml
      push.yaml
      status.yaml
    go/
      run.yaml
      build.yaml
    aws/
      s3-ls.yaml
  chains/
    deploy.yaml
  logs/
    git-pull.log
    git-pull.log.1       # rotated
    deploy.log
  backup/
    2026-04-28.yaml      # exports commands + chains together
```

Config file `~/.moc.yaml` is unchanged. Maker uses its own directory.

---

## Data Models

### Command (`~/.moc/commands/<cmdlet>/<slug>.yaml`)

```yaml
cmdlet: git
command: "git pull"
description: "sync main"     # optional
type: shell                  # shell | moc
workdir: ""                  # optional, defaults to pwd at save time
created_at: 2026-04-28T10:00:00Z
last_run: ~
last_status: never           # never | success | failed
schedule:
  cron: ""
  in_session: false
  os_registered: false
```

Slug is derived from command string: `git pull` → `pull.yaml`, `git reset --soft HEAD~3` → `reset-soft-head3.yaml`.

### Chain (`~/.moc/chains/<name>.yaml`)

```yaml
name: deploy
description: "sync, build, run"
stop_on_error: true          # default true; false = nonstop
steps:
  - command: git/pull        # references <cmdlet>/<slug> — matches storage path
  - command: go/build
  - command: go/run
created_at: 2026-04-28T10:00:00Z
last_run: ~
last_status: never
schedule:
  cron: ""
  in_session: false
  os_registered: false
```

---

## Save + Execute Rules

| Situation | Behavior |
|---|---|
| New cmdlet+command | save + execute |
| Same cmdlet + same command | execute only (no re-save) |
| Same cmdlet + different command string | update + execute |
| `--add` flag present | save only, do not execute |

---

## Scheduling

### In-session (goroutine)

On `moc` start, `maker_scheduler.go` reads all commands/chains with `in_session: true`. Spawns one goroutine per schedule with a ticker derived from cron expression. On tick, runs the command and sends result to a global `notifyCh chan Notification`.

`runMainShell()` gets a companion goroutine that drains `notifyCh` and prints to stdout with a mutex, appearing between prompts without interrupting input:

```
moc ❯
  ── [maker] git pull ─────────────────
  Already up to date.
  Status: success  (08:30:01)
  ─────────────────────────────────────

moc ❯ _
```

### OS-level

`--os` flag on `moc maker schedule` registers with the OS scheduler and sets `os_registered: true`.

**Windows:**
```
schtasks /Create /TN "moc-git-pull" /TR "moc maker run git pull" /SC MINUTE /MO 60
```
Note: cron expression is parsed and translated to `schtasks` flags. Complex cron expressions (e.g. weekday-only, nth-of-month) fall back to `/SC MINUTE /MO <interval>` approximation. User is warned when approximation occurs.

**Linux/Mac:**
```
(crontab -l; echo "0 8 * * * moc maker run git pull") | crontab -
```

`moc maker unschedule git pull --os` removes from OS + resets flag.

---

## Logging + Rotation

Each execution appends to `~/.moc/logs/<cmdlet>-<slug>.log`:

```
[2026-04-28 08:30:01] git pull — START
Already up to date.
[2026-04-28 08:30:02] git pull — SUCCESS (1.2s)
```

Chain logs to `~/.moc/logs/chain-<name>.log`, including per-step status.

**Rotation:** file > 1MB → rename to `.log.1`, create new `.log`. Max 3 rotations (`.log.1`, `.log.2`, `.log.3`). Oldest deleted automatically. Cleanup runs silently at `moc` startup (<50ms).

---

## CLI Interface (quick mode)

```bash
# Save + execute (or just execute if unchanged)
moc maker git pull
moc maker git "git reset --soft HEAD~3"
moc maker go "go run ." --workdir ~/projetos/myowncli

# Save only (dry-run)
moc maker git pull --add

# Explicit run
moc maker run git pull

# Chains
moc maker chain add deploy git/pull go/build go/run
moc maker chain run deploy
moc maker chain deploy --export > deploy.sh    # outputs shell script

# Schedule
moc maker schedule git pull --cron "0 8 * * *"
moc maker schedule git pull --cron "0 8 * * *" --os
moc maker unschedule git pull
moc maker unschedule git pull --os

# Logs
moc maker log git pull           # tail (last 20 lines)
moc maker log git pull --all     # full log

# Backup / restore
moc maker backup
moc maker restore ~/.moc/backup/2026-04-28.yaml

# Enter TUI shell
moc maker
```

---

## TUI Shell (bubbletea)

```
╭─ maker ──────────────────────────────────────────────╮
│  cmdlets: git·3  go·2  aws·1     chains: deploy      │
│  último: git pull ✓ 08:30                            │
╰──────────────────────────────────────────────────────╯

  /ls   /add   /run   /schedule   /log   /chain   /backup

maker ❯ /git

  [1]  git pull        sync main      ✓ 08:30
  [2]  git push                       — never
  [3]  git status                     ✓ 08:29

maker [git] ❯ /run 1
maker [git] ❯ /schedule 2 --cron "0 9 * * *"
maker [git] ❯ /log 1
```

- Fixed header: total cmdlets, chains, last execution status
- `/` prefix for all shell actions
- Arrow-key navigation in lists
- Typing filters by cmdlet name
- Live notifications from scheduler appear between prompts (same as main shell)

### Shell commands

| Command | Action |
|---|---|
| `/ls` | List all cmdlets and chain count |
| `/git`, `/aws`, etc. | Show commands under that cmdlet |
| `/run <n>` | Execute command by list number |
| `/add <cmdlet> <command>` | Save new command |
| `/schedule <n> --cron "..."` | Schedule selected command |
| `/log <n>` | Tail log for selected command |
| `/del <n>` | Delete command |
| `/chain` | Enter chain management |
| `/backup` | Export backup |
| `exit` / `q` | Exit maker shell |

---

## Chain Export (shell script)

`moc maker chain deploy --export` outputs:

```bash
#!/bin/bash
set -e        # stop_on_error: true maps to set -e
git pull
go build ./...
go run .
```

`stop_on_error: false` omits `set -e`.

---

## Error Handling

- Missing `workdir`: run from current directory, warn once
- OS scheduler registration failure: log error, keep `os_registered: false`, continue
- Command execution failure in chain with `stop_on_error: true`: log failure, abort remaining steps, set `last_status: failed`
- Cron parse error: reject at save time with clear message

---

## Out of Scope (this iteration)

- Migrating `sf` module to maker commands (user-planned future work)
- Parallel chain execution
- Command versioning / history of past inputs
- Remote sync of command repository
