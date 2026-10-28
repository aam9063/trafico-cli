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
| T1 | H-01: licencia de cada dataset de la DGT | S6 | inline (research) | `3627851` |
| T2 | H-02: flujo de trabajo y comprobación del harness | S1, S2, S6 | inline | `67e17b9` |
| T3 | SPIKE-01: feeds reales, instantáneas y `mapping.yaml` | S6, S7 | inline | _(este PR)_ |

## Evidencia de verificación

**T1 · H-01**

- Ficha de cada dataset leída en `nap.dgt.es` el 2026-10-28; campo `dc:rights` y enlace a
  `opendefinition.org/licenses/cc-by`.
- Confirmado que **SCT declara literalmente `No se especificó la licencia`** → excluido.
- Los 7 recursos de la DGT responden `HTTP 200` en SPIKE-01, sin token ni registro.

**T2 · H-02**

- `gentle-ai --version` → 4.3.1; `pi --version` → 1.4.0; extensiones `gentle-pi`, `gentle-engram`,
  `pi-btw`, `pi-web-access` actualizadas.
- `gentle-ai doctor` → 6 ok, 0 fallos, 5 avisos, estado `degraded`. Los avisos son de entorno y
  están descritos con su remedio en el ADR 0002.
- `gentle-ai review mode status` → RDD `on` (global).
- `main` y `dev` creadas y publicadas; `dev` es la rama por defecto del repositorio.

**T3 · SPIKE-01**

- 18 descargas (`6 feeds × 3 instantáneas`), todas `HTTP 200`; 7 feeds catalogados incluyendo
  `tefiva` (SPFRIA) descubierto por búsqueda en el portal.
- Frecuencia del feed de incidencias medida: `publicationTime` avanza exactamente 60,0 s entre
  instantáneas consecutivas.
- **Sin `ETag` ni `Last-Modified`** en ninguno de los 6 feeds → no hay cabeceras condicionales.
  Gzip soportado en todos (7,17 MB → 224 KB en incidencias).
- 18/18 fixtures en `testdata/feeds/` validadas como XML bien formado.
- `internal/datex2/mapping.yaml` parsea correctamente como YAML: 12 claves de primer nivel,
  14 `cause_types`, 13 grupos y 39 entradas de `detailed_cause_types`.
- Recorte reproducible con `scripts/trim-fixtures.sh`; 628 KB versionados frente a ~19 MB por
  instantánea de los originales.

## Progreso

- Bootstrap del repositorio: `main` creada con README, LICENSE, AGENTS.md, `docs/ARRANQUE.md`.
- T1 (H-01) cerrada y mergeada en `dev`.
- T2 (H-02) cerrada y mergeada en `dev`.
- T3 (SPIKE-01) cerrada y mergeada en `dev`.

## Siguiente paso

Ola 0 completa. Falta la autorización del usuario para la Ola 1 (OPS-01 en adelante).

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
