# trafico-cli · MCP

`trafico mcp` convierte el binario en un servidor **Model Context Protocol (MCP)** que habla por
**stdio**: el transporte que consumen Claude Desktop, Cursor, Codex y Pi. El resultado es que un agente
pregunta en lenguaje natural («¿qué pasa en la A-7 en el kilómetro 100?») y recibe el mismo JSON
congelado que `trafico incidents --json` imprime: el agente aprende un formato, el mismo de `schemas/`.

La v1 es **solo lectura**, por diseño.

## Conectar el agente

El binario ya instalado es todo lo que hace falta: el servidor no abre puerto ni pid, escribe el
protocolo en stdout y lee en stdin (diagnósticos solo en stderr, P-01).

### Claude Desktop

`claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "trafico": {
      "command": "trafico",
      "args": ["mcp"]
    }
  }
}
```

### Cursor

`~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "trafico": {
      "command": "trafico",
      "args": ["mcp"]
    }
  }
}
```

### Pi

`~/.pi/mcp.json` (o el equivalente del agente en uso):

```json
{
  "mcpServers": {
    "trafico": {
      "command": "trafico",
      "args": ["mcp"]
    }
  }
}
```

En cualquiera de los tres la config usa `trafico mcp` sin flags: no hay flag de transporte en la v1
(stdio es el de los cuatro agentes) y no hay ningún token. El servidor hereda la configuración del
usuario (`$XDG_CONFIG_HOME/trafico-cli/config.toml`): feeds, lugares guardados y rutas son los mismos
que usa la CLI. Las ubicaciones, rutas y lugares guardados nunca salen de la máquina: el servidor
responde al cliente MCP que ya controla el usuario, no abre sinks.

## Herramientas (v1, solo lectura)

| Herramienta | Equivale a | Qué puede pedir |
|---|---|---|
| `incidencias` | `trafico incidents --json` | `road`, `pk_from`, `pk_to`, `direction`, `province`, `type`, `severity`, `status`, `from`/`to` (instantes RFC3339), `limit`, `sort`, `near` + `radius_km` |
| `paneles` | `trafico signs --json` | `road`, `direction`, `province`, `limit` |
| `radares` | `trafico radars --json` | `road`, `direction`, `province`, `limit` |
| `tramos` | `trafico sections --kind <kind> --json` | `kind` (obligatorio: `invive` o `animals`), `road`, `direction`, `province`, `limit` |
| `rutas` | `trafico route list --json` (sin `name`) / `trafico route check --json` (con `name`) | `name` (opcional) |
| `estado_feeds` | `trafico feeds list --json` (sin `check`) / `trafico feeds status --json` (con `check`) | `check` (booleano, opcional) |

Detalles que el agente debe saber:

- Toda respuesta lleva `attribution` y `meta.published_at`; el `published_at` es `null` en los feeds
  que no publican fecha (radares, tramos): `null`, nunca una fecha inventada.
- Los filtros son los mismos que en la CLI, con dos formas de llamar: el rango de punto kilométrico
  va como `pk_from`/`pk_to` numéricos (no como el texto `100-140` del flag) y la ventana de
  fechas son instantes RFC3339 (`2026-01-17T00:00:00Z`), porque el agente es quien calcula «el
  fin de semana».
- Los errores de uso llegan como error de herramienta (`isError`) con el mismo mensaje en español
  que imprimiría la CLI.
- Hay las cuatro recetas de la guía del agente (§6.4 de `docs/ARRANQUE.md`) como **prompts**
  (`estado-carretera`, `mi-ruta`, `obras-fin-de-semana`, `paneles`), y los JSON Schemas publicados
  como **resources** (`trafico://schemas/<nombre>`), para que el agente los descubra por el protocolo.

## Solo lectura, con la puerta abierta

Viene de decisión del usuario (ODD ola-7-mcp L2: «lecturas pero deja la puerta abierta a rutas y
vigilancia»):

- El **registro de herramientas** lleva una marca `Mutating` por herramienta; el servidor no expone
  ninguna herramienta marcada como de escritura, y un test falla si la v1 expone una. La puerta está
  abierta en código y en test, no en una nota al pie.
- **Escribir rutas y vigilancias es el punto de extensión documentado** (y aún sin implementar vía
  MCP): las funciones que correspondería exponer (`app.AddRoute`, `app.RemoveRoute`, el ciclo de
  vigilancias) existen en la aplicación y siguen siendo territorio de la CLI. Una futura ola las
  declararía en el registro con `Mutating: true` y con su criterio de confirmación, y sólo entonces
  se expondrían.
- La regla de oro no se mueve: el servidor MCP **no envía ubicaciones, nombres de rutas ni lugares
  guardados a ningún sitio** — lee feeds de la DGT y responde al cliente local.
