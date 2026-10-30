# trafico-cli

**Spanish traffic data for agents: one JSON document, stable exit codes, no guessing.**

A single-binary Go CLI that reads the DGT open data feeds, normalises them into a documented JSON
contract, and lets an agent query incidents by road, kilometre point or area — and watch a route,
notifying a phone when something changes on it.

[![CI](https://github.com/aam9063/trafico-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/aam9063/trafico-cli/actions/workflows/ci.yml)
[![Go 1.26+](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)
[![Data: CC BY](https://img.shields.io/badge/data-CC%20BY-lightgrey)](https://nap.dgt.es/)

---

## Why this exists

The DGT publishes its traffic data as DATEX II XML, refreshed every minute, under a free licence.
There are web apps built on it and there are scripts that scrape it. None of them are built for an
agent: nothing offers a stable JSON contract, kilometre-point filters, saved routes, or exit codes
that say *why* a call failed instead of just *that* it failed.

That gap is the whole point of this tool.

## What it does

- **Queries incidents** — active or planned — by road, kilometre range, direction, province, type,
  severity or distance from a saved place.
- **Checks a saved route** in one call, grouped by segment in the order the route declares them.
- **Watches anything queryable** and notifies you when it changes, inside the time windows you set.
- **Reports feed health** so a silent feed is visible before it costs you a decision.
- **Speaks JSON first**: one value on stdout, diagnostics on stderr, an `attribution` block in every
  response, and exit codes that encode the failure class.

## What it does not do

- **No routing.** "My route" is a list of road segments you save, not a computed path.
- **No travel times and no predictions.** It shows what the DGT publishes, when it publishes it.
- **No Catalonia or Basque Country coverage in v1.** The DGT feed excludes what those regions
  manage; `meta.coverage_note` says so in the response instead of letting an agent conclude there is
  no traffic there.
- **No LLM inside.** The CLI is deterministic; the agent reasons.
- **Not an official source.** See [Attribution](#attribution-and-scope).

## Install

No release is published yet. Build from source:

```bash
git clone https://github.com/aam9063/trafico-cli.git
cd trafico-cli
make build          # produces ./bin/trafico
```

Requires Go 1.26 or newer. The binary is pure Go — `CGO_ENABLED=0` builds and tests the whole tree,
including the SQLite driver used by watches.

## Quick start

```bash
# What is happening on the A-7 between kilometre 100 and 140?
trafico incidents --road A-7 --pk 100-140

# Anything planned on the N-332 this weekend?
trafico incidents --road N-332 --type roadworks --status planned --from 2026-11-01 --to 2026-11-02

# Save your commute as segments, then check it in one call
trafico route add trabajo --segment A-7:102-118 --segment CV-86:0-6
trafico route check trabajo

# Save a place, then ask what is near it
trafico place add casa --lat 38.3452 --lon -0.4810
trafico incidents --near casa --radius 15km

# Get told when something changes on that route, weekday mornings
trafico watch add trabajo route trabajo \
  --window "lun-vie 07:00-09:00" --min-severity medium --notify phone
trafico watch service install      # systemd on Linux, launchd on macOS
```

Every command answers with one JSON value. `--pretty` renders the same response as human text in
Spanish, ending with the attribution line.

## Commands

| Command | What it answers |
|---|---|
| `incidents` | Current or planned incidents, with every filter below |
| `incidents show <id>` | One incident by its `dgt:<record id>` |
| `route add \| list \| show \| remove \| check` | Saved routes of road segments, and their current state |
| `place add \| list \| remove` | Saved coordinates for `--near` |
| `watch add \| list \| show \| remove \| check \| run` | Saved watches, and the vigilance cycle |
| `watch service install \| uninstall \| status` | The scheduler unit (systemd / launchd) |
| `feeds list \| status` | Configured feeds, and their freshness and errors |
| `doctor` | Configuration, feeds and cache health in one report |
| `version`, `completion` | Version and shell completion |

### Incident filters

```
--road A-7            repeatable; a7, A 7 and A-7 are the same road
--pk 100-140          kilometre range; requires exactly one --road
--direction           positive | negative | both
--province alicante   repeatable; either half of a bilingual name works
--near casa          a saved place, or --lat/--lon with --radius 15km
--type roadworks,accident,closure
--severity            low | medium | high
--status              active | planned | all        (default: active)
--from / --to         date or RFC3339; a bare --to date includes the whole day
--limit 50 --sort     severity | pk | start
```

An incident marked `both` in the feed matches `--direction positive` and `--direction negative`,
because a closure that blocks both carriageways affects the one you are driving on.

## The JSON contract

`schema_version`, `meta` and `attribution` appear in every response; the payload sits next to them,
never nested. The JSON Schema files under `schemas/` are what the contract tests validate against in
CI.

```json
{
  "schema_version": 1,
  "meta": {
    "feed": "dgt-situations",
    "published_at": "2026-10-28T09:29:00Z",
    "fetched_at": "2026-10-28T09:30:12Z",
    "age_seconds": 72,
    "stale": false,
    "from_cache": true,
    "total": 2,
    "unlocated": 0,
    "coverage_note": "Cobertura de la DGT: no incluye las incidencias gestionadas por la SCT (Cataluña) ni por el Gobierno Vasco"
  },
  "attribution": {
    "source": "DGT, Punto de Acceso Nacional",
    "license": "CC BY",
    "url": "https://nap.dgt.es/",
    "fetched_at": "2026-10-28T09:30:12Z"
  },
  "incidents": []
}
```

Read `meta.published_at` and `meta.stale` before telling anyone what is happening *now*: when the
network is down, the tool serves the last cached data, marks it `stale: true` and warns on stderr.

Fields the feed does not publish are `null`. Nothing is inferred: the DGT omits severity on most
records, so `severity: "unknown"` there means *not published*, not *low*.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success — including data served from a stale cache, which `meta.stale` reports |
| `1` | Unexpected internal error |
| `2` | Invalid arguments, cancelled, or a destructive command missing `--yes` |
| `4` | Not found: unknown incident, route, place or watch |
| `5` | Upstream problem: the DGT rate-limited or failed |
| `6` | No network and no usable cache |
| `7` | The feed changed shape or the URL disappeared — the message includes a prefilled issue link |
| `130` | Interrupted |

There is no code 3: nothing here authenticates.

Failures also carry a JSON envelope on stderr:

```json
{"error":{"type":"usage","message":"--pk solo es válido con exactamente una --road","exit_code":2}}
```

## Configuration

Files follow the XDG base directory spec:

| What | Where |
|---|---|
| Config — feeds, places, routes, sinks | `$XDG_CONFIG_HOME/trafico-cli/config.toml` |
| Database — watches, events, previous state | `$XDG_DATA_HOME/trafico-cli/trafico.db` |
| Feed cache | `$XDG_CACHE_HOME/trafico-cli/` |

Both the config file and the database are created `0600` inside a `0700` directory, and writes are
atomic: a crash leaves the old file or the new one, never half of either. A config file with wider
permissions is rejected rather than silently accepted.

```toml
[feeds.dgt-situations]
url = "https://nap.dgt.es/datex2/v3/dgt/SituationPublication/datex2_v37.xml"
enabled = true

[places]
casa = "38.3452,-0.4810"

[stale]
max_age = "10m"

[sinks.phone]
type = "ntfy"
url = "https://ntfy.sh"
topic = "pick-a-long-random-topic"

[sinks.alerts]
type = "webhook"
url = "https://discord.com/api/webhooks/..."
template = "discord"
```

Feed URLs are compiled in and can be overridden here, so a DGT URL change is a config edit rather
than a release. Sinks are `ntfy`, `webhook` (templates `discord`, `slack`, `telegram`) and `exec`.

## Privacy

Your locations stay on your machine. Home, work and saved routes live only in the local `0600`
config, and **a notification never contains a route name, a place name or coordinates** — only the
road, the kilometre point, the incident type, the severity and the attribution. The payload has no
field where personal data could travel, which is stronger than filtering it out.

The only network destination is `nap.dgt.es`, plus whatever sinks you configure yourself.

## Watches

A watch is a saved query plus a time window. The first check records what is already happening and
stays **silent**, so creating a watch never fires a burst of alerts; `--emit-initial` opts into
announcing the baseline instead.

After that it reports three events: `incident.new`, `incident.updated` (severity, lanes or expected
end changed) and `incident.cleared` — which requires the incident to be absent from **two
consecutive** checks, so a blip in the feed cannot announce that the road opened.

Outside its window a watch does not check and does not notify. The interval has a two-minute floor
and a five-minute default, enforced before the request, so the tool stays polite to a feed that
updates every minute. A cross-process lock keeps a scheduled `watch run` and a manual
`watch check` from duplicating notifications, and `watch check` shows what *would* be notified
without consuming it.

## Attribution and scope

Traffic data is published by the **DGT (Dirección General de Tráfico)** through the Spanish National
Access Point (<https://nap.dgt.es/>) under the **Creative Commons Attribution (CC BY)** licence.
Every response carries the `attribution` block, and `--pretty` output ends with `Fuente: DGT (CC BY)`.

This data is normalised and filtered by this tool; the output is not the DGT's original document.
See [`docs/feeds.md`](docs/feeds.md) for how each feed was measured and what it does and does not
contain.

**This is not an official tool.** It is an unaffiliated client. Do not use it as a substitute for
official DGT channels or for the real road signs. The DGT does not endorse, sponsor or maintain this
project.

## Development

```bash
make build          # build ./bin/trafico
make test           # go test ./...
make check          # the full quality gate: gofmt, vet, staticcheck, govulncheck, tests
make bench          # parser benchmarks
make fixtures-trim  # regenerate the trimmed feed snapshots under testdata/feeds/
```

`make check` delegates to [`scripts/check.sh`](scripts/check.sh), which is the single definition of
"green" for both local runs and CI. No test in this repository touches the network: the parser and
the whole CLI run against real captured snapshots in [`testdata/feeds/`](testdata/feeds/), and a
nightly job is the only thing that talks to the DGT.

- [`docs/ARRANQUE.md`](docs/ARRANQUE.md) — the design and backlog this project is built from.
- [`docs/feeds.md`](docs/feeds.md) — every feed measured: URLs, dialects, headers, record structure.
- [`AGENTS.md`](AGENTS.md) — the working rules for agents in this repository.

Every task runs against a review gate: a pull request naming a task must carry its review report
under [`odd/reviews/`](odd/reviews/), and CI fails without it.

## License

Code is [MIT](LICENSE). The DGT data is CC BY and keeps its own licence — see `LICENSE` for the
separation.
