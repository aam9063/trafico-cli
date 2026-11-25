# Ola 7 · MCP

## Specs

- **S1** — «Y Podemos hacer un mcp a partir de esta cli? para no tener que estar con comandos y asi hablar en lenguaje natural con el agente?» El binario gana un subcomando `trafico mcp` que habla Model Context Protocol por **stdio**, que es el transporte que consumen Claude Desktop, Cursor, Codex y Pi. Sin flags nuevas para elegir transporte en la v1.
- **S2** — «lecturas pero deja la puerta abierta a rutas y vigilancia». La v1 expone **solo herramientas de lectura**. El registro de herramientas está construido para admitir mutaciones después (cada herramienta declara si escribe), y un test falla si la v1 expone una herramienta que escribe. La puerta abierta no es una nota al pie: es código y es un test.
- **S3** — Toda herramienta lee por la misma función de `internal/app` que usan los comandos y la TUI. Ninguna consulta nueva, ningún parser nuevo, ninguna lógica de filtrado propia del MCP.
- **S4** — Cada resultado de herramienta es **el mismo contrato JSON** que produce `--json`, con su `attribution` y su `meta.published_at`. Un agente no aprende dos formatos.
- **S5** — Las cuatro recetas de §6.4 de `docs/ARRANQUE.md` se publican como **prompts** MCP, y los JSON Schemas de `schemas/` como **resources**. El agente las descubre desde el protocolo, no leyendo el repositorio.
- **S6** — `CGO_ENABLED=0` sigue compilando y los tres targets del release se construyen. La dependencia nueva es pure Go.
- **S7** — Ningún test de PR toca la red. Las sesiones MCP se prueban sobre el transporte en memoria del SDK, con las fixtures y el fetcher falsos que ya usa la suite.

## Alcance medido

- La costura existe: `internal/app` es la única puerta de consultas (`Incidents`, `Signs`, `Radars`, `Sections`, `Routes`, `Feeds`). La CLI y la TUI son presentaciones encima. El MCP es la cuarta, no un producto nuevo.
- Dependencia: `github.com/modelcontextprotocol/go-sdk` (SDK oficial, Tier 1 de la especificación, Apache-2.0/MIT, mantenido con Google). Pure Go, sin cgo. Es una dependencia de entrega: entra en el `go.mod` con versión fijada y se revisa como tal.
- No hay tokens que inventar: el contrato JSON ya está congelado en `schemas/` y las recetas ya están escritas.

## Decisiones

- **stdio, no HTTP.** El caso de uso es un agente local en la misma máquina que ya tiene el binario. HTTP añade puerto, autenticación y superficie de red a un proyecto cuyo contrato es «ninguna dependencia de red aparte de nap.dgt.es».
- **Paquete `internal/mcp` propio, aislado de `internal/cli`.** Igual que la TUI: mezclar el servidor con cobra haría que un cambio de herramienta pudiera romper un contrato de salida.
- **Escrituras fuera de la v1, dentro del diseño.** Designar `Rutas` y `Vigilancias` como `Mutating: true` y exigir confirmación explícita por llamada es trabajo de otra ola; lo que la v1 garantiza es que el registro sabe distinguirlas y que ninguna está expuesta.
- **La regla de oro del proyecto no se mueve:** nunca se envían ubicaciones, nombres de rutas ni lugares guardados a ningún sitio. El MCP lee y responde al cliente MCP que el usuario ya controla; no abre sinks.

## Tasks

| ID | Spec | Descripción |
| --- | --- | --- |
| T1 | S1, S6 | Dependencia del SDK oficial y esqueleto `trafico mcp` por stdio con handshake |
| T2 | S3, S4 | Herramientas de lectura: incidencias, paneles, radares, tramos |
| T3 | S3, S4 | Herramientas de lectura: rutas guardadas y estado de feeds |
| T4 | S5 | Las cuatro recetas como prompts MCP y los schemas como resources |
| T5 | S2 | Registro con marca de escritura y test que prueba que v1 no expone ninguna |
| T6 | S8 | Tests de sesión sobre transporte en memoria, sin red |
| T7 | S6, S7 | `CGO_ENABLED=0`, tres targets, y la documentación de cómo conectar el agente |

## Log

- **L1** — «Arreglalo y subelo directo a main. Y Podemos hacer un mcp a partir de esta cli? para no tener que estar con comandos y asi hablar en lenguaje natural con el agente?»
- **L2** — Decisión de alcance del usuario: «lecturas pero deja la puerta abierta a rutas y vigilancia».
