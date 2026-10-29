# OLA-3: vigilancias (watch, eventos y avisos)

Autorizado el 2026-10-29. Alcance: las tareas de la Ola 3 del backlog de `docs/ARRANQUE.md`.

## Specs

- **S1** — Git: `main` solo releases y solo la mueve el usuario; el agente mergea siempre contra
  `dev`. Cita: "Mientras mergea siempre hacia dev".
- **S2** — Alcance autorizado: Ola 3 completa (`WATCH-01`, `WATCH-02`).
- **S3** — **Línea base silenciosa** al crear una vigilancia (salvo `--emit-initial`): lo que ya
  pasaba no genera avisos (§5.5).
- **S4** — **Ventanas horarias**: fuera de la ventana no se comprueba ni se avisa. Hora de
  Europe/Madrid.
- **S5** — **Eventos**: `incident.new`, `incident.updated` (cambia severidad, carriles o fin
  previsto), `incident.cleared` (desaparece en **dos** comprobaciones seguidas).
- **S6** — Intervalo mínimo 2 minutos, por defecto 5.
- **S7** — **El aviso nunca incluye el nombre ni las coordenadas de la ruta**: solo carretera, PK,
  tipo, severidad y atribución (P-07).
- **S8** — Destinos: `ntfy`, `webhook` (plantillas `discord`, `slack`, `telegram`) y `exec`.
- **S9** — `watch service install/uninstall/status` sin restos al desinstalar (S2).
- **S10** — Criterio de aceptación de `WATCH-01`: dos instantáneas consecutivas producen
  exactamente un `incident.new`; fuera de ventana no hay aviso; el aviso no contiene coordenadas ni
  nombre de ruta.

## Tasks

| ID | Tarea | Specs | Depende | Commit |
|---|---|---|---|---|
| T1 | WATCH-01a: `internal/window`, parser de `--window` | S4 | — | _(pendiente)_ |
| T2 | WATCH-01b: `internal/store`, SQLite sin CGO | S3, S5 | — | _(pendiente)_ |
| T3 | WATCH-01c: `internal/watch`, línea base, eventos y sinks | S3, S5, S6, S7, S8 | T1, T2 | _(pendiente)_ |
| T4 | WATCH-01d: `watch add/list/remove/check/run` | S3-S8 | T3 | _(pendiente)_ |
| T5 | WATCH-02: `watch service install/uninstall/status` | S9 | T4 | _(pendiente)_ |

T1 primero: es lógica pura, sin dependencias nuevas, y es donde un error se propaga a todos los
avisos (una ventana mal parseada silencia o despierta al usuario a destiempo).

## Evidencia de verificación

_(pendiente por tarea)_

## Progreso

- Olas 0, 1 y 2 cerradas. `dev` en `f129d1c`, 27 commits sobre `main`.

## Siguiente paso

T1 — WATCH-01a.

## Log

- **L1** (usuario, verbatim): "Vas a empezar la Ola 3 no? lo digo por mergear de mientras dev a main yo"
