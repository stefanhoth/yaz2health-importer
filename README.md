# yaz2health

Synct Ernährungsdaten (Kalorien, Makros) und Wasserzufuhr aus [Yazio](https://www.yazio.com) in **Google Health** — duplikatfrei, idempotent, als launchd-Job 3x täglich.

## Wie es funktioniert

```
yazio CLI ──summary JSON──▶ mapper ──▶ planner (diff) ──▶ Google Health API v4
                                          ▲
                                          └── dataPoints.list (existing state)
```

- Pro Yazio-Tag entstehen bis zu 5 Datenpunkte: je ein `nutrition-log` pro Mahlzeit (Frühstück/Mittag/Snack/Abend mit kcal, Kohlenhydraten, Protein, Fett) und ein `hydration-log` für Wasser.
- **Duplikatfreiheit:** Jeder Punkt bekommt eine deterministische Client-ID (`yazio-2026-06-11-lunch`). Vor jedem Schreiben wird der Ist-Zustand aus Google gelesen und gedifft: `create` / `patch` / `delete` / `skip`. Erneutes Ausführen ist immer idempotent. Punkte ohne `yazio-`-Präfix (z.B. manuell in der Health-App geloggt) werden nie angefasst.
- Yazio-Tage sind UTC-basiert; die Tageszuordnung wird 1:1 übernommen. Mahlzeiten bekommen repräsentative Uhrzeiten (08/13/16/19 Uhr lokal, Wasser 12 Uhr), da Yazio nur den Tag kennt.

## Voraussetzungen

1. **yazio CLI** installiert und eingeloggt (`yazio auth status` → `valid`).
2. **Google-Cloud-Projekt** mit aktivierter [Google Health API](https://developers.google.com/health):
   - APIs & Services → Library → "Google Health API" → Enable
   - OAuth Consent Screen: App anlegen, Scopes `googlehealth.nutrition.readonly` + `googlehealth.nutrition.writeonly` hinzufügen
   - **Wichtig:** Publishing-Status auf **"In production"** stellen (nicht "Testing"). Im Testing-Modus laufen Refresh-Tokens nach 7 Tagen ab und der Cronjob bricht. Die "unverified app"-Warnung beim Login ist für die persönliche Nutzung unbedenklich (über "Advanced" bestätigen).
   - Credentials → Create Credentials → OAuth client ID → Typ **"Desktop app"** → JSON herunterladen

## Setup

```bash
make install                     # baut nach $GOBIN (~/go/bin/yaz2health)

# Einmalig: Google-Login (öffnet Browser)
yaz2health auth login --client-secret ~/Downloads/client_secret_*.json
yaz2health auth status
```

Token und Client-Secret liegen danach in `~/Library/Application Support/yaz2health/` (Mode 0600).

## Benutzung

```bash
yaz2health sync --dry-run        # zeigt geplante Aktionen, schreibt nichts
yaz2health sync                  # heute + 3 Tage Lookback (Default)
yaz2health sync --days 30        # Backfill: die letzten 30 Tage
yaz2health sync --from 2026-05-12 --to 2026-06-11
```

Beispielausgabe:

```
Syncing 2026-06-08..2026-06-11
create yazio-2026-06-10-breakfast (327 kcal)
create yazio-2026-06-10-water (1650 ml)
patch yazio-2026-06-09-dinner (650 kcal -> 783 kcal)
created=2 patched=1 deleted=0 skipped=12
```

## Automatisierung (launchd, 3x täglich)

```bash
cp launchd/com.stefanhoth.yaz2health.plist ~/Library/LaunchAgents/
launchctl load ~/Library/LaunchAgents/com.stefanhoth.yaz2health.plist

# Status & Log
launchctl list | grep yaz2health
tail -f ~/Library/Logs/yaz2health.log

# Sofort testweise auslösen
launchctl start com.stefanhoth.yaz2health
```

Zeiten (10/15/21 Uhr) bei Bedarf in der Plist anpassen, danach `launchctl unload` + `load`.

## Entwicklung

```bash
make test    # Unit-Tests (Parser, Mapper, Diff-Planner, API-Sink gegen httptest)
make vet
make build   # ./bin/yaz2health
```

Struktur: `internal/yazio` (Quelle, Subprocess) → `internal/domain` (Datenmodell) → `internal/mapper` (Tag → Punkte) → `internal/planner` (purer Diff) → `internal/health` (Google Health API v4 + OAuth) → `internal/syncer` (Orchestrierung).

## Troubleshooting

| Symptom | Ursache / Fix |
|---|---|
| `not logged in (run yaz2health auth login)` | OAuth-Flow noch nicht gelaufen oder Token gelöscht |
| `token invalid or revoked` nach ~7 Tagen | Consent Screen steht auf "Testing" → auf "In production" stellen, neu einloggen |
| `yazio summary ...: exit status 1` | yazio CLI nicht eingeloggt: `yazio auth login` |
| `did not honor the client-provided data point ID` | Die API hat die Client-ID verworfen; Abbruch verhindert Duplikate. Bitte Issue aufmachen — das Verhalten der (neuen) Write-API hat sich geändert. |
| Eintrag von gestern 23:30 fehlt am erwarteten Tag | Yazio bucht UTC-basiert; späte Einträge landen ggf. auf dem Folgetag. Der Lookback synct beide Tage korrekt. |
