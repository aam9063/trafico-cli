# Ola 5 · agentes y publicación

## Specs

- **S1** — `trafico skills` lista las recetas disponibles, con su identificador y una línea de para qué sirven.
- **S2** — `trafico skills get <receta>` devuelve la receta completa: el comando exacto, qué responde y cómo leer la salida. Debe bastar para que un agente sin contexto resuelva el caso sin leer la documentación del repositorio.
- **S3** — Las cuatro recetas de `docs/ARRANQUE.md` §6.4 existen y funcionan contra el binario:
  - `estado-carretera` → `trafico incidents --road A-7 --pk 100-140`
  - `mi-ruta` → `trafico route check trabajo`
  - `obras-fin-de-semana` → `trafico incidents --road N-332 --type roadworks --status planned --from <sáb> --to <dom>`
  - `paneles` → `trafico signs --road AP-7 --direction positive`
- **S4** — `trafico schema` lista los esquemas publicados y `trafico schema <nombre>` devuelve el JSON Schema embebido en el binario, no leído de `schemas/` en disco: el binario instalado por el release no lleva el repositorio al lado.
- **S5** — Toda salida respeta FORMAT-01: texto legible por defecto, `--json` para el valor JSON. `schema` y `skills` no son la excepción.
- **S6** — Un nombre de receta o de esquema desconocido es un error de uso (exit 2), nunca una lista vacía con exit 0.

## Medición previa

- `schemas/` contiene solo `incident.schema.json` e `incidents-response.schema.json`; el resto del contrato vive en `internal/model` como structs con tags `json`.
- `internal/agentdocs/` contiene solo `doc.go`: el paquete existe como marcador pero no publica nada.
- Las cuatro recetas están redactadas en `docs/ARRANQUE.md` §6.4 y ninguna está expuesta por el binario.

## Tasks

| ID | Spec | Descripción |
| --- | --- | --- |
| T1 | S1, S2, S3 | `internal/agentdocs`: las recetas como datos embebidos, con un test que verifique que cada comando citado existe en el árbol de cobra |
| T2 | S4 | `go:embed` de `schemas/` en el binario y comando `schema` para listar y servir |
| T3 | S5, S6 | Cableado en `internal/cli` con los flags de FORMAT-01 y exit 2 en nombre desconocido |
| T4 | S3 | Test que ejecuta cada receta contra los fixtures y comprueba que el comando resuelve |

## Decisiones

- Los esquemas se embeben con `go:embed` y son la copia de `schemas/`: una sola fuente, sin duplicar el contrato a mano.
- Un test compara cada receta contra el árbol de comandos de cobra, para que una receta no quede apuntando a un flag que ya no existe.

## Log

- **L1** — «si pero primero realinea dev» — `dev` realineado con `main` por merge; `main` es ancestro y el contenido difiere solo en R3-001, que va por delante.
