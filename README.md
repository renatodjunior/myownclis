<div align="center">

<img src="docs/screenshots/logo.png" alt="MOC" width="360" />

**MyOwnCLI** — personal CLI hub for AWS and other services

[![Go](https://img.shields.io/badge/go-1.24-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Bubble Tea](https://img.shields.io/badge/built%20with-bubbletea-FF7CCB)](https://github.com/charmbracelet/bubbletea)

</div>

---

## What is `moc`?

`moc` is a single-binary, terminal-first companion for the day-to-day chores of an
engineer who lives between AWS, scripts, and shell history. Two modules ship today:

- **`sf`** — Step Functions browser, watcher, tail and rerun in a TUI.
- **`maker`** — personal command repository: save, schedule, chain CLIs you
  reach for over and over.

It runs as a one-shot command (`moc sf list`) **or** as an interactive shell
(`moc`) where each module has its own sub-shell.

## Screenshots

| Main shell | Main shell — modules |
|---|---|
| ![moc main](docs/screenshots/MOC%201.png) | ![moc help](docs/screenshots/MOC%201.1.png) |

| Maker mode | Step Functions — state machines |
|---|---|
| ![maker mode](docs/screenshots/MOC%202.png) | ![sf list](docs/screenshots/MOC%20SF%20LS.png) |

| Step Functions — loading |
|---|
| ![sf loading](docs/screenshots/MOC%20SF%20LOADING.png) |

## Install

Requires Go 1.24+.

```bash
git clone https://github.com/renatodjunior/myownclis.git
cd myownclis
go build -o moc .
# optional — drop the binary on your PATH
mv moc ~/bin/   # or C:\Users\<you>\bin on Windows
```

## Quick start

```bash
moc                    # interactive shell
moc sf                 # Step Functions sub-shell
moc maker              # command repository TUI

moc sf list            # one-shot: list state machines
moc maker git status   # save and run "git status" under the "git" cmdlet
```

### Interactive shell

```
moc ❯ help
moc ❯ sf
moc ❯ maker
moc ❯ exit
```

### `sf` — Step Functions module

```bash
moc sf list                # list state machines
moc sf watch <name>        # follow latest executions
moc sf tail <execution>    # stream history events
moc sf rerun <execution>   # restart with same input
```

### `maker` — command repository

`maker` groups commands under **cmdlets** (e.g. `git`, `aws`, `docker`). Each
command keeps its workdir, last-run status and a tail of its log. Commands can
be run on demand, scheduled with cron, or composed into chains.

```bash
moc maker                       # open interactive TUI
moc maker git "git status"      # save under cmdlet "git"
moc maker run git status        # run by slug
moc maker schedule git status "*/15 * * * *"
```

Inside the TUI:

| Key / command | Action |
|---|---|
| `↑ ↓` | Navigate list |
| `Enter` | Open cmdlet / run command |
| `Esc` | Back / clear input |
| `/add <cmd>` | Save command in active cmdlet |
| `/del`, `/log` | Delete / show log of selected command |
| `/help` | All commands and shortcuts |
| `exit`, `q` | Leave maker |

## Configuration

`moc` reads a YAML config from the standard location for your OS
(`~/.config/moc/config.yaml` on Linux/macOS, `%APPDATA%\moc\config.yaml` on
Windows). All keys are optional.

```yaml
region: us-east-1
profile: default
maker:
  store: ~/.moc/maker     # where commands and logs live
```

`region` and `profile` can also be overridden per-invocation through
environment variables (`AWS_REGION`, `AWS_PROFILE`).

## Architecture

```
cmd/
├── root.go              cobra root + interactive shell
├── logo.go              shared MOC ASCII logo
├── sf.go / sf_tui.go    Step Functions module
└── maker.go             cobra subcommands for maker
    maker_tui.go         bubbletea TUI
    maker_store.go       YAML-backed command repository
    maker_exec.go        shell execution + log capture
    maker_log.go         per-command append-only log
    maker_scheduler.go   in-session cron driver
```

Built on top of:

- [`spf13/cobra`](https://github.com/spf13/cobra) + [`viper`](https://github.com/spf13/viper) — commands and config
- [`charmbracelet/bubbletea`](https://github.com/charmbracelet/bubbletea), [`bubbles`](https://github.com/charmbracelet/bubbles), [`lipgloss`](https://github.com/charmbracelet/lipgloss) — TUI
- [`aws-sdk-go-v2`](https://github.com/aws/aws-sdk-go-v2) — Step Functions
- [`robfig/cron`](https://github.com/robfig/cron) — scheduler

## Roadmap

- [ ] More AWS modules (Lambda, CloudWatch Logs, ECS)
- [ ] Maker chains UI (multi-step pipelines with conditional steps)
- [ ] Cross-machine sync of the maker store
- [ ] Plugin system for third-party cmdlets

## License

MIT — see [LICENSE](LICENSE).
