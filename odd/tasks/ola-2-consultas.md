# OLA-2: consultas (incidents, rutas y estado de feeds)

Autorizado el 2026-10-28. Alcance: las tareas de la Ola 2 del backlog de `docs/ARRANQUE.md`.
**Estado: cerrada.** Las cuatro tareas están en `dev`, con CI verde en los tres SO.

## Specs

- **S1** — Flujo de git: `main` solo releases (humano), `dev` integración, ramas `tr/<id>-<slug>`;
  el agente mergea **siempre contra `dev`**. Cita: "Mientras mergea siempre hacia dev".
- **S2** — Alcance autorizado: Ola 2 completa. Cita: "seguimos con Ola 2 y ya despues mergearé a
  main."
- **S3** — `incidents` y `incidents show` con todos los filtros de §5.2, y `coverage_note` que
  **nombra al gestor, no a la provincia**: "Cobertura de la DGT: no incluye las incidencias
  gestionadas por la SCT (Cataluña) ni por el Gobierno Vasco". Decidido por el usuario (L3).
- **S4** — `--pk` sin `--road` es un error de uso: **código 2**.
- **S5** — Golden por filtro; ningún test de PR toca la red.
- **S6** — `route check` agrupa por tramo **en el orden de la ruta**.
- **S7** — `feeds list/status` y `doctor` muestran URL, edad y estado de cada feed.
- **S8** — Salida JSON con `schema_version`, `meta` y `attribution`; `pretty` en español.

## Tasks

| ID | Tarea | Specs | Commits | PR |
|---|---|---|---|---|
| T0 | Decisión y documentación de `coverage_note` | S3 | `8007422` | #15 |
| T1 | CMD-02: `feeds list/status` y `doctor` | S7, S8 | `545a174`, `2e19804` | #16 |
| T2a | CMD-01: `internal/filter` e `internal/geo` | S4, S5 | `bc35dc1` | #17 |
| T2b | CMD-01: `incidents`, `incidents show`, goldens | S3, S4, S5, S8 | `4f2acb2`, `50af55f` | #18 |
| T3 | ROUTE-01: `place` y `route` | S6, S8 | `761077b` | #19 |

CMD-02 fue primero porque no dependía de ninguna decisión abierta; CMD-01 se partió en dos por
superficie (lógica pura y comandos) para que cada mitad se revisara sola.

## Evidencia de verificación

**Por tarea**

- **CMD-02**: `feeds list` hace **cero** peticiones (contadas en el servidor de prueba) y reporta
  `enabled`/`disabled`, no `ok`; `feeds status` registra un feed caído como `error` y sigue;
  `doctor` informa config corrupta con `ok: false` sin fallar. Review independiente → **REQUEST
  CHANGES** → siete correcciones.
- **CMD-01a**: los doce filtros de §5.2 y haversine, 51 tests. Review independiente → **APPROVE
  WITH FIXES** → siete correcciones.
- **CMD-01b**: `incidents` e `incidents show`, siete goldens validados contra el schema con
  resolución de `$ref`, `time/tzdata` embebido. Review independiente → **REQUEST CHANGES** → seis
  correcciones.
- **ROUTE-01**: `place` y `route` con rutas en `config.toml`; una sola descarga por `route check`;
  `--yes` obligatorio para borrar. Review independiente → **APPROVE** con un minor documentado.

**Transversal**

- CI verde en **ubuntu, macos y windows** en los cuatro PRs de código.
- `check.sh --strict` verde en cada PR: gofmt, build, vet, staticcheck, govulncheck, tests.
- `go test ./... -race` en `golang:1.26.10` sobre Linux en cada tarea: once paquetes `ok`.
- `govulncheck`: **0 vulnerabilidades** durante toda la ola.

**Defectos que la revisión independiente encontró y que los tests propios no**

Veinte en total. Los tres que más importan, por lo que habrían significado en producción:

1. **`direction: "both"` excluida al filtrar por `positive`** (CMD-01a). El feed marca 78
   incidencias así: un corte que bloquea ambos sentidos no aparecía al preguntar por el sentido
   propio.
2. **`NaN` aceptado en `--radius`/`--lat`/`--lon`** (CMD-01b). Toda comparación con NaN es falsa,
   así que pasaba todas las validaciones, descartaba todas las filas y devolvía `exit 0` con
   "no hay incidencias".
3. **Aviso de caché calculado y descartado** (CMD-02). El usuario veía `stale: true` en el JSON y
   ningún aviso en stderr, contra §5.6.

Y uno encontrado por el padre: **un registro sin `<loc:pointCoordinates>` recibía coordenadas
`(0,0)`** — el Golfo de Guinea. Con `--near` desaparecía sin contarse como no ubicada.

**Patrón que dejó la ola**: los defectos que sobreviven a los tests propios son casi siempre
**valores límite aceptados en silencio** — `NaN`, `-5`, `""`, `{null,null}`, `both`. El camino feliz
siempre está cubierto; lo que falta es rechazar lo que el usuario pasa raro en vez de decidir por él.

## Progreso

Ola 2 cerrada. `dev` acumula **26 commits** sobre `main`, que sigue en el bootstrap: solo la mueve
una persona y no hay release todavía.

## Siguiente paso

Ola 3 (vigilancias): `WATCH-01` y `WATCH-02`. Pendiente de autorización del usuario.

Lo que la Ola 3 va a necesitar y hoy no existe:

- `internal/store` con SQLite (`modernc.org/sqlite`, sin CGO) para estado previo y eventos.
- `internal/window` para el parser de `--window` ("lun-vie 07:00-09:00").
- `internal/watch` con la línea base, los eventos (`incident.new`, `incident.updated`,
  `incident.cleared`) y los sinks (ntfy, webhook, exec).
- Un `PrettyFunc` para `--pretty`, que hoy cae al JSON indentado en todos los comandos.
- El merge `dev` → `main`, que es decisión del usuario.

## Log

- **L1** (usuario, verbatim): "seguimos con Ola 2 y ya despues mergearé a main. Mientras mergea
  siempre hacia dev"
- **L2**: `dev` → `main` lo hará el usuario cuando corresponda; el agente no lo toca.
- **L3** (decisión del usuario): `coverage_note` nombra a los gestores, no a la provincia.
- **L4**: `internal/config` se abrió como superficie en ROUTE-01 solo para añadir `[routes]`, porque
  §7 dice que las rutas viven en la config y no en SQLite.
