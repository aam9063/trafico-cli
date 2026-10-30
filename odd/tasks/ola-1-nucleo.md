# OLA-1: núcleo (esqueleto, contrato de salida, config, feeds, modelo)

Autorizado el 2026-10-28. Alcance: todas las tareas de la Ola 1 del backlog de `docs/ARRANQUE.md`.
**Estado: cerrada.** Las siete tareas están en `dev`, con CI verde en los tres SO.

## Specs

- **S1** — Flujo de git: `main` solo releases (humano), `dev` integración, ramas `tr/<id>-<slug>`
  desde `dev`. El agente commitea, pushea, abre PR contra `dev` y lo mergea con rebase.
  Cita: "Tenemos la rama main y de ahi sacas dev y de dev salen todas las features, fix etc. Tu
  vas a commitear pushear y crear la pr y mergearla contra dev."
- **S2** — Higiene: sin conflictos ni ramas out of date. Cita: "Mantién siempre buena higiene en
  git para que no hayan conflictos, ramas out of date y demás."
- **S3** — Lenguaje: Go. Cita: "el lenguaje en el que tenemos que hacer este proyecto será en go".
- **S4** — Alcance autorizado: Ola 1 completa. Cita: "1. Si Ola 1."
- **S5** — Decisiones cerradas: comandos y flags en inglés, binario `trafico`, sin geocodificación
  (ADR 0002).
- **S6** — La fuente real manda: `docs/feeds.md` y `internal/datex2/mapping.yaml`.
- **S7** — Tres dialectos DATEX II. FEED-02 implementa solo v3.7 de situaciones.
- **S8** — `make check` delega en `scripts/check.sh`, que corre también sin `make`.
- **S9** — Ningún test de PR toca la red; las fixtures son `testdata/feeds/`.

## Tasks

| ID | Tarea | Specs | Commit | PR |
|---|---|---|---|---|
| T1 | OPS-01: estructura §8.3, `go.mod`, Makefile, `check.sh`, CI | S3, S4, S8 | `754a4f2` | #5 |
| T2 | CORE-01: writer JSON, sobre de error, códigos de salida | S4 | `87a728a` | #9 |
| T3 | CORE-02: config TOML, XDG, escrituras atómicas | S4 | `b5435c3`, `1c3badd`, `8336335` | #11 |
| T4 | FEED-02: parser DATEX II v3.7 en streaming + mapping | S4, S6, S7, S9 | `4717a72`, `ab08412` | #8 |
| T5 | FEED-01: descarga, caché de 60 s, frescura, respaldo | S4, S6, S9 | `308b879`, `d49523b` | #13 |
| T6 | MODEL-01: modelo `Incident`, vías, `status`, JSON Schema | S4, S6 | `5c3e4a7` | #10 |
| T7 | OPS-02: review gate en CI y plantilla | S4 | `610d3fa` | #12 |

## Evidencia de verificación

**Por tarea**

- **OPS-01**: `check.sh --strict` verde; `trafico version` → `0.0.0-dev`; CI verde en ubuntu,
  macos y windows. Defecto corregido: el job de Windows llamaba `make`, que no existe en esos
  runners.
- **CORE-01**: tabla de constructores con JSON exacto y código de salida; `--bogus` y comando
  desconocido ahora salen con **2** (§6.3), no con 1. Atribución y `Fuente: DGT (CC BY)`
  verificados.
- **CORE-02**: rutas XDG con entorno inyectado, merge sobre defaults, TOML estricto, escritura
  atómica y permisos `0600`/`0700`. Los tests de permisos solo corren en Linux/macOS; verificados
  en Docker. Defecto corregido: el feed de motos estaba en los defaults sin que ningún comando lo
  consuma.
- **FEED-02**: parser streaming a **68,24 MB/s** sobre fixture real; 18 situaciones / 24 registros
  por instantánea; namespaces por URI; `-race` verde en Linux.
- **FEED-01**: caché de 60 s con una sola petición por ventana; respaldo a caché con `stale: true`
  y aviso; códigos 5/6/7 verificados; **24 tests con `-race`** en `golang:1.26.6` sobre Linux,
  incluidos cuatro de regresión de los dos blockers.
- **MODEL-01**: golden byte a byte desde el registro real `18811074`; tabla de `status` con reloj
  inyectado; esquemas JSON validados; tabla de normalización de vías aprobada por el usuario.
- **OPS-02**: el gate bloquea sin informe y pasa con él; probado contra títulos con dos IDs, IDs en
  minúscula, `XOPS-99`, headings en `###`, informes vacíos y exactamente 200 bytes.

**Transversal**

- CI verde en **ubuntu, macos y windows** en los seis PRs con código.
- `check.sh --strict` verde en local: gofmt, build, vet, staticcheck, govulncheck, tests.
- Suite completa en Linux real (`golang:1.26.6`): `cli`, `config`, `datex2`, `errs`, `feeds`,
  `model`, `output`, `road` en `ok`.
- `govulncheck`: **0 vulnerabilities** con el toolchain en 1.26.10.

**Seguridad**

- **GO-2026-6088** (`encoding/xml` sin guard de recursión): toolchain subido a 1.26.6 en `ab08412`.
- **10 vulnerabilidades de `net/http` y `crypto/x509`** que se volvieron alcanzables al introducir
  HTTP: toolchain subido a **1.26.10** en `d49523b`.
- **Inyección de shell** en el job del review gate vía título del PR: corregida en `610d3fa`,
  y corregida también en `docs/ARRANQUE.md` §12, que tenía la misma forma.

**Review gate**

Dos tareas pasaron por un revisor independiente de contexto limpio, con informe en
`odd/reviews/`:

- `OPS-02.md` → REQUEST CHANGES (inyección de shell) → corregido.
- `FEED-01.md` → REQUEST CHANGES (corte a mitad de cuerpo; caché envenenada con 0 bytes) → corregido.

## Progreso

Ola 1 cerrada. Siete tareas, ocho PRs mergeados a `dev` (los seis de código más el de evidencia de
Ola 0 y este). `main` sigue en el commit de bootstrap: solo la mueve una persona y no hay release
todavía.

## Siguiente paso

Ola 2 (consultas): `CMD-01`, `ROUTE-01`, `CMD-02`. Pendiente de autorización del usuario.

Decisiones que la Ola 2 va a tener que tomar, ya identificadas:

- `coverage_note` impreciso: la DGT sí publica incidencias en Barcelona, Girona y Tarragona
  (`docs/feeds.md` §4.6).
- Provincia por código INE en radares: hace falta tabla o `province: null` (FEED-04).
- `id` de incidencia: registro (implementado) o situación.
- Los tres dialectos DATEX II: v3 y v1.0 quedan para FEED-03/04/05.

## Log

- **L1** (usuario, verbatim): "1. Si Ola 1. 2.como que hay duplicados y el doctor no esta?"
- **L2**: la Ola 1 se autorizó completa, no tarea por tarea.
- **L3**: el entorno local no tenía `make`, `staticcheck` ni `govulncheck`. Los dos linters se
  instalaron vía `go install`; `make` no se instala en Windows y por eso `Makefile` y
  `scripts/check.sh` comparten la definición de "check".
- **L4** (usuario): "ya está docker corriendo" → se incorporó Docker/Linux a la verificación
  normal. Encontró tres defectos que solo aparecían fuera de Windows.
- **L5**: el usuario aprobó la tabla de normalización de vías (prefijo de 1-3 letras + dígito)
  antes de fijarla como contrato.
