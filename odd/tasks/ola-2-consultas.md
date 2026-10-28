# OLA-2: consultas (incidents, rutas y estado de feeds)

Autorizado el 2026-10-28. Alcance: las tareas de la Ola 2 del backlog de `docs/ARRANQUE.md`.

## Specs

- **S1** — Flujo de git: `main` solo releases (humano), `dev` integración, ramas `tr/<id>-<slug>` desde
  `dev`; el agente mergea **siempre contra `dev`**. Cita: "Mientras mergea siempre hacia dev".
- **S2** — Alcance autorizado: Ola 2 completa. Cita: "seguimos con Ola 2 y ya despues mergearé a
  main."
- **S3** — `incidents` y `incidents show` con todos los filtros de §5.2, y `coverage_note` que **nombra al gestor, no a la provincia**: "Cobertura de la DGT: no incluye las incidencias gestionadas por la SCT (Cataluña) ni por el Gobierno Vasco". Decidido por el usuario el 2026-10-28 (ver L3). La nota aparece salvo que el filtro restrinja a provincias sin gestor autonómico distinto. Motivo: el feed sí trae incidencias en Barcelona, Girona y Tarragona (carreteras estatales), así que "sin datos de Cataluña" sería falso y un agente lo repetiría.
- **S4** — `--pk` sin `--road` es un error de uso: **código 2** (§6.3).
- **S5** — Golden por filtro; ningún test de PR toca la red (S9 de la Ola 1).
- **S6** — `route check` agrupa por tramo **en el orden de la ruta**.
- **S7** — `feeds list/status` y `doctor` muestran URL, edad y estado de cada feed.
- **S8** — Salida JSON con `schema_version`, `meta` y `attribution`; `pretty` en español.

## Tasks

| ID | Tarea | Specs | Depende | Commit |
|---|---|---|---|---|
| T1 | CMD-02: `feeds list/status` y `doctor` | S7, S8 | Ola 1 | _(pendiente)_ |
| T2 | CMD-01: `incidents`, `incidents show`, filtros y `coverage_note` | S3, S4, S5, S8 | T1, Ola 1 | _(pendiente)_ |
| T3 | ROUTE-01: `place` y `route add/list/show/remove/check` | S6, S8 | T2 | _(pendiente)_ |

CMD-02 va primero porque no depende de ninguna decisión abierta y comparte `internal/cli/root.go`
con CMD-01: serializarlos evita que dos escritores editen el mismo archivo.

## Evidencia de verificación

_(pendiente por tarea)_

## Progreso

- Ola 0 y Ola 1 cerradas (ver `odd/tasks/ola-0-preparacion.md` y `odd/tasks/ola-1-nucleo.md`).

## Siguiente paso

T1 — CMD-02.

## Log

- **L1** (usuario, verbatim): "seguimos con Ola 2 y ya despues mergearé a main. Mientras mergea
  siempre hacia dev"
- **L2**: `dev` → `main` lo hará el usuario cuando corresponda; el agente no lo toca.
- **L3** (decisión del usuario, 2026-10-28): `coverage_note` nombra a los gestores, no a la provincia. Se eligió sobre mantener el literal del borrador y sobre añadir un campo estructurado `excluded_managers`. Aplicado a `docs/ARRANQUE.md` §5.3 (bloque JSON, regla de aparición y receta de la skill) y a `docs/feeds.md` §4.6.
