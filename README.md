# yaz2health

Syncs nutrition data (calories, macros) and water intake from [Yazio](https://www.yazio.com) to **Google Health**, duplicate-free, idempotent, running hourly via launchd.

## How it works

```
yazio CLI ──summary JSON──▶ mapper ──▶ planner (diff) ──▶ Google Health API v4
                                          ▲
                                          └── dataPoints.list (existing state)
```

- Up to 5 data points are created per Yazio day: one `nutrition-log` per meal (breakfast/lunch/snack/dinner with kcal, carbs, protein, fat) and one `hydration-log` for water.
- **Duplicate Prevention:** Every point receives a deterministic client ID (`yazio-2026-06-11-lunch`). Before any write operation, the current state is read from Google and diffed: `create`, `patch`, `delete`, `skip`. Re-running the sync is always idempotent. Points without the `yazio-` prefix (e.g., logged manually in the Health app) are never modified.
- Yazio days are UTC-based; the day assignment is transferred 1:1. Meals are assigned representative times (08:00 / 13:00 / 16:00 / 19:00 local time, water at 12:00) because Yazio only tracks the day.

## Prerequisites

1. **[yazio CLI](https://github.com/itzptk/yazio-go-cli)** installed and logged in (`yazio auth status` → `valid`).
2. **Google Cloud Project** with the [Google Health API](https://developers.google.com/health) enabled:
   - APIs & Services → Library → Search for "Google Health API" → Enable
   - OAuth Consent Screen: Create an app, add scopes `googlehealth.nutrition.readonly` and `googlehealth.nutrition.writeonly`
   - **Important:** Set the publishing status to **"In production"** (not "Testing"). In testing mode, refresh tokens expire after 7 days, which breaks the cronjob. The "unverified app" warning during login can be safely ignored for personal use (confirm via "Advanced").
   - Credentials → Create Credentials → OAuth client ID → Application type **"Desktop app"** → Download the JSON file

## Setup

```bash
make install                 # builds to $GOBIN (~/go/bin/yaz2health)

# One-time step: Google login (opens browser)
yaz2health auth login --client-secret ~/Downloads/client_secret_*.json
yaz2health auth status
```

Token and client secret are stored in `~/Library/Application Support/yaz2health/` (Mode 0600).

## Usage

```bash
yaz2health sync --dry-run        # shows planned actions, writes nothing
yaz2health sync                  # today + 3 days lookback (default)
yaz2health sync --days 30        # backfill: the last 30 days
yaz2health sync --from 2026-05-12 --to 2026-06-11
```

Example output:

```
Syncing 2026-06-08..2026-06-11
create yazio-2026-06-10-breakfast (327 kcal)
create yazio-2026-06-10-water (1650 ml)
patch yazio-2026-06-09-dinner (650 kcal -> 783 kcal)
created=2 patched=1 deleted=0 skipped=12
```

## Automation (launchd)

Two jobs cover different cadences:

| Job | Schedule | Command |
|-----|----------|---------|
| `com.stefanhoth.yaz2health.backfill` | daily at 07:00 | `sync` (today + 3-day lookback) |
| `com.stefanhoth.yaz2health.hourly` | hourly 09:00–22:00 | `sync --days 1` (today only) |

```bash
cp launchd/com.stefanhoth.yaz2health.backfill.plist ~/Library/LaunchAgents/
cp launchd/com.stefanhoth.yaz2health.hourly.plist ~/Library/LaunchAgents/
launchctl load ~/Library/LaunchAgents/com.stefanhoth.yaz2health.backfill.plist
launchctl load ~/Library/LaunchAgents/com.stefanhoth.yaz2health.hourly.plist

# Status & logs
launchctl list | grep yaz2health
tail -f ~/Library/Logs/yaz2health.log

# Trigger a manual test run immediately
launchctl start com.stefanhoth.yaz2health.backfill
launchctl start com.stefanhoth.yaz2health.hourly
```

To adjust the schedule, edit the plist files, then `launchctl unload` + `load`.

## Development

```bash
make test    # unit tests (parser, mapper, diff planner, API sink against httptest)
make vet
make build   # ./bin/yaz2health
```

Structure: `internal/yazio` (source, subprocess) → `internal/domain` (data model) → `internal/mapper` (day → points) → `internal/planner` (pure diff) → `internal/health` (Google Health API v4 + OAuth) → `internal/syncer` (orchestration).

## Troubleshooting

| Symptom | Cause / Fix |
|---|---|
| `not logged in (run yaz2health auth login)` | OAuth flow has not been run yet or token was deleted |
| `token invalid or revoked` after ~7 days | Consent screen is set to "Testing" → change to "In production", log in again |
| `yazio summary ...: exit status 1` | yazio CLI is not logged in: run `yazio auth login` |
| `patch failed (...), retrying as delete+create` | Google Health's Patch endpoint returned a 500; the tool automatically falls back to delete + create. Normal behaviour on this new API. |
| Yesterday's 23:30 entry is missing from the expected day | Yazio logs entries based on UTC; late entries might end up on the following day. The lookback syncs both days correctly. |
