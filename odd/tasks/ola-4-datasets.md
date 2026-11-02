# OLA-4: más datasets (paneles, radares y tramos)

Autorizado el 2026-10-30. Alcance: las tareas de la Ola 4 del backlog de `docs/ARRANQUE.md`.

## Specs

- **S1** — Git: `main` solo releases y solo la mueve el usuario; el agente mergea siempre contra
  `dev`, con rebase y borrado de rama.
- **S2** — Alcance autorizado: Ola 4 completa salvo **FEED-06**. Cita: "Ola 4".
- **S3** — **FEED-06 queda fuera**: su criterio es "H-01 positivo para ambos" y SCT no declara
  licencia (ADR 0001). DT-GV es CC BY pero no se integra en esta ola.
- **S4** — Solo `internal/datex2` conoce el XML. Ahora con **dos dialectos nuevos**:
  - **v3** (`datex2.eu/schema/3`, `loc:PredefinedLocationsPublication`): paneles-localizaciones,
    tefiva, motos.
  - **v1.0** (`d2LogicalModel`, `datex2.eu/schema/1_0/1_0`): radares, INVIVE.
  - El v3.7 de incidencias (`levelC/schema/3`) no cambia.
- **S5** — Cada parser se valida contra las instantáneas reales de `testdata/feeds/`; ningún test
  de PR toca la red.
- **S6** — No se inventa ningún campo: si el feed no lo trae, `null` (§4.3 y §6 de `docs/feeds.md`).
- **S7** — Toda salida conserva `schema_version`, `meta.published_at` y `attribution`.

## Tasks

| ID | Tarea | Feed | Dialecto | Commit |
|---|---|---|---|---|
| T1 | FEED-03: parser de paneles y comando `signs` | vms-locations + vms-messages | v3 + v3.7 | _(pendiente)_ |
| T2 | FEED-04: parser de radares y comando `radars` | radars | v1.0 | _(pendiente)_ |
| T3 | FEED-05: parser de tramos y comando `sections` | invive + tefiva | v1.0 + v3 | _(pendiente)_ |
| T4 | ROUTE-02: `route check` incluye paneles y radares | — | — | _(pendiente)_ |

## Lo que la medición ya sabe (no hay que redescubrirlo)

- **Paneles son dos feeds cruzados por id**: `DevicePublication` da la ubicación y
  `VmsPublication` el mensaje, con `vmsControllerReference/@id` ↔ `ns2:device/@id`.
- **Radares**: 737 tramos, sin `speed_limit`, PK en **metros** (`referencePointDistance`), provincia
  como **código INE** — hace falta una tabla INE→nombre o `province: null`.
- **INVIVE**: 1331 tramos v1.0 con PK en metros.
- **TEFIVA (SPFRIA)**: 150 tramos v3 con el PK **codificado en el id** (`HUELVA_N-433_71400.0`).
- Ninguno de estos feeds publica `publicationTime` utilizable en algunos casos: `published_at`
  puede ser `null` (P-08 y §9 de `docs/feeds.md`).

## Evidencia de verificación

_(pendiente por tarea)_

## Progreso

- Olas 0 a 3 cerradas y publicadas en `main` (`b8683f9`, 34 PRs).

## Siguiente paso

T1 — FEED-03.

## Log

- **L1** (usuario, verbatim): "Ola 4"
