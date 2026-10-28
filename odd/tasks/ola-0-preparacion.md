# OLA-0: preparación (licencias, workflow, feeds reales)

Autorizado el 2026-10-28. Alcance: H-01, H-02 y SPIKE-01 del backlog de `docs/ARRANQUE.md`.
Sin código de CLI en esta ola: solo hechos de la fuente, licencias y forma de trabajo.

## Specs

- **S1** — Flujo de git: `main` y `dev` como ramas largas; de `dev` salen todas las features y
  fixes. El agente commitea, pushea, abre el PR y lo mergea contra `dev`. `main` no se toca.
  Cita: "Tenemos la rama main y de ahi sacas dev y de dev salen todas las features, fix etc. Tu
  vas a commitear pushear y crear la pr y mergearla contra dev."
- **S2** — Higiene en git: sin conflictos, sin ramas out of date. Cita: "Mantién siempre buena
  higiene en git para que no hayan conflictos, ramas out of date y demás."
- **S3** — Lenguaje del proyecto: Go. Cita: "el lenguaje en el que tenemos que hacer este
  proyecto será en go".
- **S4** — D-01 resuelto: comandos y flags en inglés; la salida `pretty` va en español.
- **S5** — D-02 y D-03 resueltos: binario `trafico`; sin geocodificación en la v1 (`--near` solo
  con lugares guardados o `--lat/--lon`).
- **S6** — Alcance autorizado de esta ola: Ola 0 completa. Cita: "Solo Ola 0 (licencias +
  SPIKE-01)". Entregables: `docs/adr/0001-licencias.md`, `docs/adr/0002-workflow.md`,
  `docs/feeds.md`, `testdata/feeds/` con al menos 3 instantáneas consecutivas por feed y
  `internal/datex2/mapping.yaml` borrador.
- **S7** — Criterio de aceptación de SPIKE-01 (ARRANQUE §14): URLs vigentes, tamaño, frecuencia,
  cabeceras, estructura de cada registro, tipos DATEX2 presentes, campos de provincia y PK.

## Tasks

| ID | Tarea | Specs | Ruta | Commit |
|---|---|---|---|---|
| T1 | H-01: licencia de cada dataset de la DGT | S6 | inline (research) | _(pendiente)_ |
| T2 | H-02: flujo de trabajo y comprobación del harness | S1, S2, S6 | inline | _(pendiente)_ |
| T3 | SPIKE-01: feeds reales, instantáneas y `mapping.yaml` | S6, S7 | inline | _(pendiente)_ |

## Evidencia de verificación

_(pendiente por tarea)_

## Progreso

- Bootstrap del repositorio: `main` creada con README, LICENSE, AGENTS.md, `docs/ARRANQUE.md`.

## Siguiente paso

T1 — H-01.

## Log

- **L1** (usuario, verbatim): "Lee bien el md de arranque para que sepas que hay que desarrollar.
  Antes de nada te voy a indicar como vamos a funcionar en git. Tenemos la rama main y de ahi
  sacas dev y de dev salen todas las features, fix etc. Tu vas a commitear pushear y crear la pr y
  mergearla contra dev. Mantién siempre buena higiene en git para que no hayan conflictos, ramas
  out of date y demás. Supoongo que el lenguaje en el que tenemos que hacer este proyecto será en
  go, no sé si lo indica en el documento"
- **L2**: decisiones del usuario en la ronda de autorización — alcance = "Solo Ola 0 (licencias +
  SPIKE-01)"; D-01 = "Inglés (recomendado por el doc)"; D-02/D-03 = "Sí: binario `trafico`, sin
  geocodificación". Se descarta la Ola 1 por ahora.
