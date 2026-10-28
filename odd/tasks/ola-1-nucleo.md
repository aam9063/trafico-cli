# OLA-1: núcleo (esqueleto, contrato de salida, config, feeds, modelo)

Autorizado el 2026-10-28. Alcance: todas las tareas de la Ola 1 del backlog de `docs/ARRANQUE.md`.

## Specs

- **S1** — Flujo de git: `main` solo releases (humano), `dev` integración, ramas `tr/<id>-<slug>`
  desde `dev`. El agente commitea, pushea, abre PR contra `dev` y lo mergea con rebase.
  Cita: "Tenemos la rama main y de ahi sacas dev y de dev salen todas las features, fix etc. Tu
  vas a commitear pushear y crear la pr y mergearla contra dev."
- **S2** — Higiene: sin conflictos ni ramas out of date. `git pull --ff-only` en `dev` antes de
  ramificar, rebase antes del PR, rama borrada al mergear. Cita: "Mantién siempre buena higiene en
  git para que no hayan conflictos, ramas out of date y demás."
- **S3** — Lenguaje: Go. Cita: "el lenguaje en el que tenemos que hacer este proyecto será en go".
- **S4** — Alcance autorizado de esta ola (cita: "1. Si Ola 1."): OPS-01, CORE-01, CORE-02,
  FEED-02, FEED-01, MODEL-01 y OPS-02, en el orden de §14 del ARRANQUE.
- **S5** — Decisiones cerradas: comandos y flags en inglés, binario `trafico`, sin geocodificación
  (ADR 0002).
- **S6** — La fuente real manda: `docs/feeds.md` y `internal/datex2/mapping.yaml` son la verdad
  sobre los feeds. Donde contradigan al ARRANQUE, manda el documento medido.
- **S7** — Tres dialectos DATEX II (v3.7, v3, v1.0). FEED-02 implementa **solo v3.7 de
  situaciones**; v3 y v1.0 son de FEED-03/04/05.
- **S8** — Criterio de OPS-01 (§14): `make check` verde. En esta máquina no hay `make` ni linters
  instalados, así que `make check` delega en `scripts/check.sh`, que es la fuente única y corre en
  Git Bash y en los tres SO del CI.
- **S9** — Ningún test de PR toca la red; las fixtures son `testdata/feeds/`.

## Tasks

| ID | Tarea | Specs | Ruta | Commit |
|---|---|---|---|---|
| T1 | OPS-01: estructura §8.3, `go.mod`, Makefile, CI | S3, S4, S8 | worker | _(pendiente)_ |
| T2 | CORE-01: salida JSON, sobre de error, códigos, `attribution` | S4 | inline | _(pendiente)_ |
| T3 | CORE-02: config, XDG, escrituras atómicas | S4 | inline | _(pendiente)_ |
| T4 | FEED-02: parser DATEX II v3.7 en streaming + mapping | S4, S6, S7, S9 | inline | _(pendiente)_ |
| T5 | FEED-01: descarga, caché 60 s, frescura, respaldo | S4, S6, S9 | inline | _(pendiente)_ |
| T6 | MODEL-01: modelo `Incident`, normalización de vías, `status`, JSON Schema | S4, S6 | inline | _(pendiente)_ |
| T7 | OPS-02: review gate en CI y plantilla | S4 | inline | _(pendiente)_ |

Dependencias: T2, T3 y T4 en paralelo tras T1 (superficies disjuntas). T5, T6 y T7 después.

## Evidencia de verificación

_(pendiente por tarea)_

## Progreso

- Ola 0 cerrada (ver `odd/tasks/ola-0-preparacion.md`).

## Siguiente paso

T1 — OPS-01.

## Log

- **L1** (usuario, verbatim): "1. Si Ola 1. 2.como que hay duplicados y el doctor no esta?"
- **L2**: la Ola 1 se autoriza completa, no tarea por tarea.
- **L3**: el entorno local no tiene `make`, `staticcheck` ni `govulncheck`. Se instalaron los dos
  linters vía `go install` (sin admin); `make` no se instala en Windows y por eso `Makefile` y
  `scripts/check.sh` comparten la misma definición de "check".
