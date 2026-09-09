<p align="center">
  <a href="https://github.com/SuppieRK/cmdshape">
    <picture>
      <source srcset="assets/readme-banner.svg">
      <img src="assets/readme-banner.svg" alt="cmdshape — Shape command output. Preserve command truth.">
    </picture>
  </a>
</p>

<p align="center">
  <a href="./LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/License-MIT-blue.svg?style=flat-square" /></a>
  <a href="https://github.com/SuppieRK/cmdshape/releases"><img alt="Release" src="https://img.shields.io/github/v/release/SuppieRK/cmdshape?style=flat-square" /></a>
  <a href="https://github.com/SuppieRK/cmdshape/actions/workflows/main-validation.yml"><img alt="CI" src="https://github.com/SuppieRK/cmdshape/actions/workflows/main-validation.yml/badge.svg" /></a>
  <a href="https://sonarcloud.io/summary/new_code?id=SuppieRK_cmdshape"><img alt="Maintainability Rating" src="https://sonarcloud.io/api/project_badges/measure?project=SuppieRK_cmdshape&metric=sqale_rating" /></a>
</p>

## Keep known command noise out of your coding agent's context

`cmdshape` is command-output control for coding agents. Deterministic filters
keep known noise out of output routed through it while preserving native
execution, exit status, actionable diagnostics, and a raw-output escape hatch.
It is a small proxy around the commands you already use:

```text
$ git status
On branch main
Your branch is up to date ...
Changes not staged ...
  modified: README.md
  ?? internal/lifecycle/report_text.go

$ cmdshape git status
## main...origin/main
 M README.md
?? internal/lifecycle/report_text.go
```

Shape command output. Preserve command truth. There is no second command
language to learn, and `--raw` is always available when exact native output
matters.

## Quick start

Install the latest release:

```bash
(
  set -eu
  installer="$(mktemp)"
  trap 'rm -f "$installer"' EXIT
  curl --disable --proto '=https' --proto-redir '=https' --tlsv1.2 --max-redirs 10 -sSfL https://raw.githubusercontent.com/SuppieRK/cmdshape/main/scripts/install.sh -o "$installer" || { echo 'installer bootstrap: curl download failed' >&2; exit 1; }
  sh "$installer"
)
```

Or use wget:

```bash
(
  set -eu
  installer="$(mktemp)"
  trap 'rm -f "$installer"' EXIT
  wget --no-config --secure-protocol=TLSv1_2 --max-redirect=0 https://raw.githubusercontent.com/SuppieRK/cmdshape/main/scripts/install.sh -O "$installer" || { echo 'installer bootstrap: wget download failed' >&2; exit 1; }
  sh "$installer"
)
```

Both commands download the complete installer before executing it. The installer
uses curl, with wget as a fallback when available, and verifies the release
archive's SHA-256 checksum before replacing the executable. Set `VERSION` to an
exact `X.Y.Z` release and `CMDSHAPE_INSTALL_DIR` to an absolute directory to
override the defaults. For example, replace `sh "$installer"` above with
`VERSION=0.9.4 CMDSHAPE_INSTALL_DIR="$HOME/.local/bin" sh "$installer"`.

If installation fails, keep the complete error output: `installer bootstrap`
identifies the script download; `version lookup`, `archive`, and `checksums`
identify requests made by the installer. Use the canonical links on the
[release page](https://github.com/SuppieRK/cmdshape/releases), since redirected
asset URLs can expire.

### macOS browser downloads

The macOS binaries are not Developer ID-signed or notarized by Apple. Browser
downloads may therefore be blocked by Gatekeeper. A checksum verifies that the
archive matches the published release; it is not Apple notarization.

For a manual download, obtain the matching `darwin_arm64` (Apple Silicon) or
`darwin_amd64` (Intel) ZIP and `cmdshape_checksums.txt` from the same release.
Compare `shasum -a 256 <archive.zip>` with that archive's entry in the manifest
before extracting and running it. If you trust the download and macOS blocks
it as unverified, follow Apple's per-app **System Settings → Privacy & Security
→ Open Anyway** instructions, then retry `cmdshape --version` in Terminal.
See [Apple's instructions](https://support.apple.com/en-gb/102445). Managed Macs
may require administrator approval. The installer does not remove quarantine
attributes or disable Gatekeeper.

### Initialize integrations

After installing, initialize the integrations you want:

```bash
cmdshape init
# or: cmdshape init --tools claude,codex,opencode
```

`cmdshape init` detects supported coding-agent integrations and installs or
refreshes their normal hook, plugin, or instruction files. The target can be
home-scoped or repository-scoped depending on the integration, so run it from
the repository where you want repo-local setup. Use `--tools` for an explicit
selection.

You can also try a command directly, without installing an integration:

```bash
cmdshape git status
cmdshape gain
cmdshape --raw git status
```

## The output contract

For ordinary commands, cmdshape:

- executes the native program and preserves its exit status;
- applies deterministic, command-aware YAML rules when a safe filter matches;
- keeps actionable diagnostics, warnings, and failure context;
- passes structured, interactive, ambiguous, or precision-sensitive output through;
- supports `--raw` for an explicit native-output escape hatch.

Filters may define documented argument normalization for a specific command
shape. Otherwise the command is executed as supplied. Either way, cmdshape
does not invent a replacement implementation of the underlying tool.

The goal is not maximum reduction. It is to remove noise the filter knows
while keeping output an agent can still use in its next shell command. When
`cmdshape` cannot identify a safe shape, it passes the output through.

## Measure the boundary `cmdshape` controls

`cmdshape gain` reports exact bytes for command output routed through
`cmdshape`. For example, this seven-day local snapshot was recorded while
working on the project:

```text
9,429 cmds · 73.3 MiB source → 72.1 MiB emitted (1.6% net reduction) [since=7d]
Most net reduction : find (793.3 KiB / 62%)
Low reduction      : go (302.1 KiB / 9%) · git (92.7 KiB / 1%)
```

This measures only the source and emitted bytes at the `cmdshape` command-output
boundary. It does not measure total agent context, model tokens, billing,
turns, task cost, or result quality. Output obtained through native file reads,
search tools, IDE features, or any other route that does not invoke `cmdshape`
is unchanged and is not included. Inspect your own workload with:

```bash
cmdshape gain --table
cmdshape history
```

## We ship useful defaults. We do not pretend to know your domain.

The built-in filters cover common tools and common noise. Your repository may
have custom scripts, generated logs, or failure output that deserves different
treatment. Keep those rules with the project instead of waiting for an
upstream release or forcing one generic policy onto every codebase.

Ask your coding agent:

> Help me create or improve a `cmdshape` filter for `<tool>`. Start with
> `cmdshape filter prompt <tool>`, then follow its workflow. Capture success,
> warning, failure, and structured output where applicable. Show me the
> proposed YAML, replay output, stream destinations, decisions, and dispatch.
> Do not trust the project filter until I have reviewed its complete source.

The agent workflow is deliberately reviewable:

`cmdshape` treats the nearest enclosing Git worktree root as the project root;
outside Git repositories, it uses the current directory. Project-local
`.cmdshape` paths below refer to that resolved root even when the command runs
from a subdirectory.

1. Inspect the active filter and copy it into `./.cmdshape/filters`, or scaffold
   a new one with `cmdshape filter new <tool>`.
2. Capture representative native success, warning, failure, and structured
   output to use as fixtures.
3. Edit the project-local YAML.
4. Review the complete source, then approve the exact bytes with
   `cmdshape filter trust`.
5. Replay the fixtures with `cmdshape verify` and inspect output, stream
   destinations, decisions, and dispatch. If verification leads to another
   edit, review and trust the changed source again before replaying it.

Any addition, removal, rename, mapping change, or content edit invalidates the
approval. Re-review and trust again after changing a project filter. Project
filters override home-scoped filters once trusted; absent, untrusted, changed,
or unsafe project filters fall back safely.

Read the focused authoring guide in [FILTERS.md](FILTERS.md).

## Integrations and operations

Automatic hooks and plugins are available for selected agents; other adapters
use managed instructions or context files. See the generated, release-owned
inventory in [CLI facts](docs/generated/CLI_FACTS.md) for the current list and
target paths.

Useful local commands:

```bash
cmdshape filter status
cmdshape filter performance --limit 30
cmdshape gain --global
cmdshape history --global
cmdshape history purge --before 90d --yes
cmdshape repair
cmdshape recovery enable
cmdshape recovery list
cmdshape recovery purge
cmdshape upgrade
cmdshape uninstall --tools codex
cmdshape uninstall
```

Command-output metrics stay on your machine and are retained for 90 days.
Recovery is disabled by default; when enabled, it stores only bounded
failed-and-compacted output and excludes raw, passthrough, confidential,
zero-byte, and oversized runs.
Captures and recovery artifacts can contain sensitive data, so review them
before sharing or committing. `cmdshape repair` rewrites cmdshape-managed home
state without replacing project-owned filters. See
[SECURITY.md](SECURITY.md) for the precise boundaries and
[ARCHITECTURE.md](ARCHITECTURE.md) for the runtime model.

## Supported command families

The shipped inventory remains explicit rather than hiding behind a generic
"many tools" claim:

| Area | Tools |
|---|---|
| Files and search | `find`, `grep`, `ls` |
| Source control | `git` |
| JVM build systems | `bazel`, `gradle`, `maven`, `sbt` |
| JavaScript and TypeScript | `biome`, `bun`, `deno`, `eslint`, `jest`, `next`, `node`, `npm`, `npx`, `nx`, `oxlint`, `playwright`, `pnpm`, `prettier`, `prisma`, `tsc`, `turbo`, `vitest`, `yarn` |
| Python | `basedpyright`, `mypy`, `pip`, `poetry`, `pytest`, `ruff`, `ty`, `uv` |
| Go and Rust | `go`, `golangci-lint`, `cargo`, `trunk` |
| Infrastructure and containers | `docker`, `helm`, `terraform`, `tflint`, `tofu` |
| Other ecosystems | `composer`, `dart`, `flutter`, `hadolint`, `mix`, `pio`, `shellcheck`, `yamllint` |

Structured, interactive, precision-sensitive, and already compact modes stay
native when filtering would make the result harder to trust.

Automatic hooks or plugins are available for Claude Code, CodeBuddy, KiloCode,
and OpenCode. Other supported agents use managed instruction, rule, or context
files: Aider, Amazon Q, Antigravity, Auggie, Cline, Codex, Crush, Cursor,
Factory, Gemini, GitHub Copilot, Kiro, Pi, Qoder, Qwen, Roo Code, Trae, and
Windsurf. `costrict` is accepted as an alias for Roo Code.

The authoritative generated inventory, lifecycle commands, and adapter list are
in [docs/generated/CLI_FACTS.md](docs/generated/CLI_FACTS.md).

## License

See [LICENSE](LICENSE).
