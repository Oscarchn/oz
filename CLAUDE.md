# CLAUDE.md

Guidance for AI assistants (Claude Code and others) working in this
repository. Keep this file accurate: when real source code lands, update the
sections below to match reality rather than leaving stale instructions.

## ⚠️ Current state: empty repository

As of this writing the working tree is **empty** — there is no application
source code on `main` or any other branch. `git ls-tree -r HEAD` returns
nothing.

The only artifact that has ever lived here is a now-deleted GitHub Actions
workflow (`.github/workflows/build-cc-connect-statusline.yml`). Its full
history is:

| Commit    | Description                            |
| --------- | -------------------------------------- |
| `cd7f4c9` | Add cc-connect statusline build workflow (263 lines) |
| `fdf4df5` | Trigger cc-connect statusline build    |
| `6f550bb` | Remove invalid temporary workflow (deletes the file) |

Because the repo is empty, there is currently **nothing to build, test, run,
or lint locally**. Do not invent build/test commands — there is no
`package.json`, `go.mod`, `Makefile`, or equivalent. If a tool reports "no
files found," that is expected, not a bug.

When you add the first real code, replace this warning and fill in the
"Codebase structure", "Development workflow", and "Conventions" sections with
concrete details.

## What this repository was for (historical context)

The deleted workflow describes the original intent of `oz`: it is a
**patch-and-build harness** for an external upstream project rather than a
project with its own source tree. The workflow did the following on push to
`main` (or via `workflow_dispatch`):

1. **Clone upstream** — `git clone --depth 1
   https://github.com/chenhg5/cc-connect.git` (a Go-based Claude Code
   notification/bridge tool with Feishu/Lark card support).
2. **Patch in a feature** — an inline `python3` script applied exact-string
   replacements across `config/config.go`, `core/engine.go`, and
   `cmd/cc-connect/main.go` to add a `statusline_footer` capability. The
   feature fetches quota/model info from an external HTTP endpoint
   (`x-api-key` / `Bearer` auth, configurable timeout and cache TTL) and
   appends a Claude-style quota footer to assistant replies.
3. **Build** — `gofmt` the touched files, run the two relevant Go tests
   (`go test ./config ./core -run '...ReplyFooter...'`), then cross-compile a
   Windows binary: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build
   -tags no_web -ldflags "-s -w ..." -o dist/cc-connect.exe ./cmd/cc-connect`.
4. **Publish** — `sha256sum` the binary and upload `cc-connect.exe` +
   `checksums.txt` as a GitHub Actions artifact.

If you are asked to restore or extend this functionality, recover the full
workflow from history with:

```bash
git show cd7f4c9:.github/workflows/build-cc-connect-statusline.yml
```

### Notes if reviving the patch-and-build pattern

- Patches were applied as **exact-string replacements** and would fail loudly
  (`raise SystemExit`) if the upstream source drifted. Pin the upstream commit
  (replace `--depth 1` cloning of the default branch with a specific ref) so
  patches stay reproducible.
- Prefer maintaining patches as real `.patch`/`.diff` files (applied with
  `git apply`) over inline Python string surgery — they are far easier to
  review, rebase, and debug.
- Go version is taken from the upstream `go.mod` via
  `actions/setup-go@v5` (`go-version-file`); the build uses the `no_web` build
  tag.

## Development workflow

### Branching

- The default branch is `main`.
- Do **not** commit directly to `main`. Create a feature branch, push it, and
  open a PR only when explicitly requested.
- Active development branch for AI-assisted work: `claude/claude-md-docs-lgRiV`
  (and similarly namespaced `claude/...` branches for other tasks).

### Pushing

```bash
git push -u origin <branch-name>
```

Retry transient network failures with exponential backoff (2s, 4s, 8s, 16s).
Do not open pull requests unless the user explicitly asks.

### Commits

Write clear, descriptive commit messages in the imperative mood (e.g. "Add
build workflow", "Remove invalid temporary workflow"), matching the existing
history style. Keep one logical change per commit.

## Conventions

- **CI lives in `.github/workflows/`.** GitHub Actions is the established
  automation surface for this repo.
- **This repo orchestrates external builds**; it does not (yet) vendor or host
  the code it builds. Treat upstream sources as read-only inputs fetched at CI
  time.
- If the project's nature changes (e.g. it becomes a standalone codebase),
  rewrite this file accordingly — these conventions are inferred from a single
  historical workflow and should not be treated as immutable.

## For AI assistants

- **Be honest about the empty state.** When asked to "find", "fix", or
  "explain the code", confirm there is no code rather than hallucinating files.
- **Check history before assuming nothing exists** — meaningful context lives
  in deleted commits (`git log --all`, `git show <commit>:<path>`).
- **Don't fabricate build/test/run commands.** Add them here only once the
  corresponding tooling actually exists in the tree.
- Keep this file in sync with the repository's real state on every
  substantive change.
