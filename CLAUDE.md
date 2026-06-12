# yaz2health — Claude Code instructions

## Workflow

- **Always work on a branch.** Never commit directly to `main`.
- **Open a PR for every change**, no matter how small. Merge via GitHub (or `gh pr merge`).
- Follow **Conventional Commits** for branch names, commit messages, and PR titles:

| Type | When to use |
|------|-------------|
| `feat` | new user-facing functionality |
| `fix` | bug fix |
| `chore` | maintenance, dependencies, config |
| `docs` | documentation only |
| `refactor` | code change with no behaviour change |
| `test` | adding or fixing tests |
| `ci` | CI/CD pipeline changes |

Branch naming: `<type>/<short-slug>` — e.g. `fix/patch-500-fallback`, `feat/delete-command`.

Commit messages: `<type>(<optional-scope>): <description>` — e.g. `fix(sink): retry on HTTP 5xx`.

PR titles follow the same format as commit messages.

```bash
git checkout -b fix/some-bug
# ... make changes ...
git commit -m "fix(syncer): fall back to delete+create on patch 500"
gh pr create --title "fix(syncer): fall back to delete+create on patch 500" --fill
```

## Build & test

```bash
make build   # ./bin/yaz2health
make test    # go test ./...
make vet     # go vet ./...
make install # go install → ~/go/bin/yaz2health
```

All tests must pass before opening a PR.

## Project structure

```
internal/yazio    — source: subprocess `yazio --output json summary <date>`
internal/domain   — shared data types (Point, DaySummary, Macros)
internal/mapper   — DaySummary → []Point (IDs, meal times)
internal/planner  — pure diff: (desired, existing) → []Action
internal/health   — Google Health API v4 wrapper + OAuth
internal/syncer   — orchestration: fetch → map → plan → apply
cmd/yaz2health    — cobra CLI (auth, sync, delete)
launchd/          — two plist jobs (backfill daily, hourly today-sync)
```

## Key behaviours to preserve

- **Idempotency**: re-running sync must never create duplicates. The planner matches by semantic key `(date, type, meal)` — Google Health does not preserve client-provided IDs, so this is the sole dedup mechanism.
- **API-enforced ownership**: the planner emits `OpDelete` for all unmatched existing points; the API itself rejects deletes for foreign points (`DATA_POINT_NOT_OWNED_BY_CLIENT`, `USER_DEFINED_CONTENT`) and those are silently skipped.
- **Patch fallback**: if Google Health returns 500 on Patch, the syncer falls back to delete + create automatically.
- **Rate limiting**: the syncer sleeps `Throttle` (default 300 ms) between Yazio fetches on multi-day ranges.
