# AGENTS.md · trafico-cli

Lee `docs/ARRANQUE.md` y `docs/feeds.md` antes de tu primera tarea.

## Qué es
CLI en Go que lee los datos abiertos de tráfico de la DGT (CC BY), los normaliza a JSON y los sirve a agentes de IA. Solo lectura.

## Cómo trabajar
- ODD (gentle-ai / gentle-shell): Authorize, Explore, Resolve uncertainty, Classify, Track, Implement, Close.
- Una tarea por vez por su ID. Si es grande, `odd/tasks/<id>-<slug>.md` antes de escribir código.
- Test-first.
- Solo `internal/datex2` conoce el XML. Solo `internal/feeds` hace HTTP.
- `internal/model` y `schemas/` son el contrato: cambios en PR aparte con `schema_version`.
- Antes de cerrar: `make check` en verde y review gate con informe en `odd/reviews/<id>.md`. Una ronda de correcciones.
- Rama `tr/<id>-descripcion`. Commit y PR.

## Git
- Ramas largas: `main` (protegida, solo releases) y `dev` (integración).
- Cada tarea nace de `dev` como `tr/<id>-descripcion` o `fix/<descripcion>`.
- El agente commitea, pushea, abre el PR contra `dev` y lo mergea a `dev` una vez que el CI está verde.
- Nunca se pushea directamente a `main` ni se mergea a `main`: `main` solo avanza por decisión humana.
- Antes de abrir el PR, rebase sobre `dev` para evitar ramas out of date y conflictos.

## Reglas duras
- stdout solo JSON. Códigos de salida de la sección 6.3.
- Toda salida lleva `attribution` y `meta.published_at`.
- No inventes campos: si el feed no lo trae, `null`. Conserva el tipo y el texto originales.
- Nunca envíes ubicaciones, nombres de rutas ni lugares guardados a ningún sitio, ni en avisos.
- Ninguna dependencia de red aparte de nap.dgt.es (y los sinks que configure el usuario).
- Ningún test de PR toca la red.

## Cuándo parar y preguntar
- El feed real no coincide con `docs/feeds.md`.
- Una URL de la DGT cambia o desaparece.
- Quieres añadir un dataset sin licencia confirmada (SCT, DT-GV u otro).
- Necesitas cambiar el esquema o un código de salida.
- Aparece un cambio de alcance.
- La review propone un refactor grande (va a REF).

## Comandos
- `make build` · `make check` · `make test` · `make golden` · `make bench` · `TRAFICO_LIVE=1 go test -tags live ./...`
