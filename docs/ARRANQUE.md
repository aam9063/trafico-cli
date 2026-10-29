# trafico-cli: documento de arranque

Versión 0.1 · 28 de octubre de 2026 · Estado: diseño. Ningún código de este documento se ha ejecutado todavía.

> Este documento es la fuente de verdad para crear el repositorio. Lo leen tus agentes en gentle-shell (Pi) con ODD. Si algo de aquí contradice a `AGENTS.md`, manda `AGENTS.md` y se corrige este documento en un PR.

---

## 0. Cómo usar este documento

1. Haz la Ola 0 (sección 14): leer el aviso legal de la DGT, instalar gentle-shell y hacer el SPIKE-01 sobre los feeds reales.
2. Crea el repo `aam9063/trafico-cli` y copia este archivo como `docs/ARRANQUE.md`.
3. Abre gentle-shell en la carpeta del repo y pega el prompt de la sección 16.
4. Autoriza ola por ola. Los agentes abren PR; el merge lo haces tú.

---

## 1. Qué es y para qué sirve

**trafico-cli** es una herramienta de línea de comandos open source (MIT) que lee los datos abiertos de tráfico de la DGT (incidencias, obras, paneles de mensaje variable y radares fijos), los normaliza a JSON y permite a un agente de IA consultarlos, filtrarlos por carretera, punto kilométrico o zona, y vigilar tus rutas con avisos al móvil.

Ejemplos de uso real con un agente:

- "¿Cómo está ahora la A-7 entre Alicante y Elche?"
- "Avísame al móvil si aparece una incidencia en mi ruta al trabajo, de lunes a viernes entre las 7:30 y las 9:00."
- "¿Hay obras o cortes programados en la N-332 este fin de semana?"
- "¿Qué dicen ahora los paneles de la AP-7 en dirección Valencia?"
- "¿Qué radares fijos hay en la A-31 entre Alicante y Albacete?"

### 1.1 Lo que NO hace (no objetivos)

- **No calcula rutas** ni depende de servicios de mapas. "Mi ruta" es una lista de carreteras con tramos de punto kilométrico (y opcionalmente coordenadas que tú guardas).
- **No predice tráfico** ni estima tiempos de viaje. Solo muestra lo que publica la DGT.
- **No cubre Cataluña ni País Vasco en la v1.** El feed de la DGT los excluye. Sus datasets propios se añaden solo si H-01 confirma su licencia.
- **No lleva un LLM dentro.** La CLI es determinista; el agente razona.
- **No es una fuente oficial.** Muestra datos de la DGT con su atribución, pero no sustituye a los canales oficiales.

### 1.2 Por qué es distinta de lo que ya existe

Con estos mismos datos hay apps web (HayTrafico, trafico.live), capas JSON para una app (copiloto-alerts) y scripts que recopilan de la web de la DGT (ICEbot-dgt). No hay ninguna herramienta pensada para agentes: contrato JSON estable, filtros por carretera y punto kilométrico, rutas guardadas, vigilancias con ventana horaria y códigos de salida. Ese es el hueco.

---

## 2. La fuente de datos (confirmado)

> **Actualizado el 2026-10-28 (SPIKE-01).** Las URLs de todos los datasets, sus tamaños, cabeceras
> y la estructura real de cada registro están medidos en `docs/feeds.md`, que manda sobre esta
> sección. En resumen: 7 feeds confirmados, todos CC BY salvo SCT, sin `ETag` ni `Last-Modified`,
> y **tres dialectos DATEX II distintos** (v3.7, v3 y v1.0) que obligan a tres modelos de parseo.

| Dato | Estado |
|---|---|
| Publicador | DGT, Punto de Acceso Nacional de tráfico (`nap.dgt.es`) |
| Dataset principal | **Incidencias DGT DATEX2 v3.7** |
| URL | `https://nap.dgt.es/datex2/v3/dgt/SituationPublication/datex2_v37.xml` |
| Formato | XML DATEX2 v3.7, modo pull por HTTPS |
| Actualización | Cada minuto |
| Licencia | **Creative Commons Atribución (CC BY)**, gratuita |
| Registro o token | No hace falta. El registro es opcional y solo sirve para recibir avisos de cortes de servicio y cambios |
| Cobertura | Red viaria española **sin País Vasco ni Cataluña**: autopistas, autovías, carreteras regionales, urbanas y locales |
| Validación | Agentes de la Agrupación de Tráfico de la Guardia Civil y gestores de los centros de gestión de tráfico de la DGT |
| Documentación | Guía de utilización de DATEX II y perfil "DGT (Spain) RTTI SituationPublication" en el propio portal |

**Otros datasets del mismo portal** (existen; URLs y licencia de cada uno a confirmar en SPIKE-01):

- Paneles de mensaje variable: ubicación de los paneles y mensaje que muestran ahora. Según un proyecto que los usa, viven en `.../DevicePublication/vms_datex2_v37.xml` y `.../VmsPublication/datex2_v37.xml`.
- Radares fijos (sin País Vasco ni Cataluña).
- Tramos INVIVE de vigilancia de velocidad intensificada.
- Tramos con alta siniestralidad por animales (SPFRIA).
- Incidencias de Cataluña (SCT) y del País Vasco (DT-GV): **fuera de la v1 hasta confirmar su licencia (H-01).**

**Aviso histórico importante:** la DGT retiró los feeds DATEX2 v1 el 30 de septiembre de 2026 y varios proyectos se rompieron. Por eso las URLs van en configuración, hay tests de contrato y un código de salida específico para "la fuente cambió".

### 2.1 Atribución (obligatoria por CC BY)

- Toda salida JSON incluye `attribution`:
  ```json
  "attribution": { "source": "DGT, Punto de Acceso Nacional", "license": "CC BY", "url": "https://nap.dgt.es/", "fetched_at": "2026-10-28T09:30:12Z" }
  ```
- `pretty` termina con una línea "Fuente: DGT (CC BY)".
- El README tiene una sección de atribución y aviso de que no es una fuente oficial.

---

## 3. Principios no negociables

| ID | Principio |
|---|---|
| P-01 | **JSON por defecto** en stdout, un único valor JSON por comando (o JSONL). Diagnóstico solo en stderr. |
| P-02 | **Códigos de salida estables** y sobre de error JSON (sección 6.3). |
| P-03 | **Solo lectura.** No hay escrituras frente a terceros. Las únicas escrituras son locales: config, caché, rutas y vigilancias. |
| P-04 | **Fiel a la fuente.** Ningún campo se inventa. Lo que no viene en el feed es `null`. El texto original de la DGT se conserva en `raw_description`. |
| P-05 | **Atribución siempre** (sección 2.1). |
| P-06 | **Educado con el servidor.** Caché compartida de 60 segundos (el feed cambia cada minuto), lectura en streaming, cabeceras condicionales si el servidor las soporta, vigilancias con intervalo mínimo de 2 minutos y 5 por defecto. |
| P-07 | **Tus ubicaciones son privadas.** Casa, trabajo y rutas viven solo en tu config local (`0600`). Nunca salen a ningún servicio ni a un aviso. |
| P-08 | **Frescura visible.** Cada respuesta dice cuándo se publicaron los datos y si la caché está caducada (`stale: true`). Un agente nunca debe dar como "ahora" un dato viejo sin saberlo. |
| P-09 | **El esquema de salida es un contrato** con `schema_version`. Cambios en PR aparte y en el CHANGELOG. |

---

## 4. Modelo de datos normalizado

### 4.1 Incidencia

```json
{
  "schema_version": 1,
  "id": "dgt:SIT-ficticio-123",
  "source": "dgt",
  "source_version": "3",
  "type": "roadworks",
  "subtype_raw": "constructionWorks",
  "severity": "medium",
  "status": "active",
  "road": "A-7",
  "direction": "positive",
  "direction_label": "Valencia",
  "pk": { "from": 102.3, "to": 108.0 },
  "location": {
    "start": { "lat": 38.30, "lon": -0.55 },
    "end": { "lat": 38.34, "lon": -0.50 },
    "province": "Alicante",
    "municipality": "Elche"
  },
  "lanes_affected": "1 de 3",
  "start_time": "2026-10-28T06:00:00Z",
  "end_time": "2026-10-28T14:00:00Z",
  "raw_description": "Texto original de la DGT",
  "updated_at": "2026-10-28T09:29:00Z"
}
```

Reglas:

- `type` normalizado: `accident` | `congestion` | `roadworks` | `closure` | `obstruction` | `weather` | `restriction` | `event` | `other`. La tabla de mapeo desde los tipos DATEX2 vive en `internal/datex2/mapping.yaml` y se construye en SPIKE-01 a partir del perfil oficial de la DGT. **`subtype_raw` conserva siempre el tipo original.**
- `status`: `active` (vigente ahora) | `planned` (empieza en el futuro) | `ended`. Se calcula con las fechas de validez del registro, no por heurística.
- `severity`: la que publique la DGT, mapeada a `low` | `medium` | `high` | `unknown`.
- `pk` en kilómetros con decimales. Si el registro solo trae coordenadas, `pk` es `null`.
- `road` normalizado: mayúsculas, guion (`a7`, `A 7` y `A-7` son `A-7`). Tests con casos raros (`N-332a`, `CV-80`, `AP-7`).
- `province` y `municipality` solo si vienen en el feed. **No se geocodifica en la v1.**

### 4.2 Panel, radar y tramo

> **Actualizado el 2026-10-28 (SPIKE-01).** Varios campos de esta lista no existen en el feed.
> Ver `docs/feeds.md` §4.3, §5, §6, §7 y §8: no hay `speed_limit`, no hay "1 de 3" carriles, no
> hay etiqueta de destino en incidencias y el PK de radares y tramos viene en metros
> (`referencePointDistance`) o codificado en el id del tramo.

- **Panel:** `id`, `road`, `pk`, `direction`, `location`, `message` (texto mostrado ahora, o `null` si apagado), `message_updated_at`.
- **Radar fijo:** `id`, `road`, `pk`, `direction`, `location`, `type` si viene, `speed_limit` si viene.
- **Tramo** (INVIVE, animales): `id`, `kind`, `road`, `pk.from`, `pk.to`, `location`.

Los campos exactos se confirman en SPIKE-01. Si un campo no existe en el feed, no existe en el modelo.

---

## 5. Diseño de la CLI

### 5.1 Comandos

| Comando | Qué hace |
|---|---|
| `trafico incidents [filtros]` | Incidencias vigentes o programadas |
| `trafico incidents show <id>` | Detalle de una incidencia |
| `trafico route add <nombre> --segment A-7:100-140 --segment N-332:50-70` | Guardar una ruta como tramos de carretera |
| `trafico route list` / `show <nombre>` / `remove <nombre>` | Rutas guardadas |
| `trafico route check <nombre>` | Incidencias, paneles y radares en una ruta |
| `trafico place add casa --lat 38.34 --lon -0.49` / `list` / `remove` | Lugares guardados para `--near` |
| `trafico signs [filtros]` | Paneles de mensaje variable y lo que muestran ahora |
| `trafico radars [filtros]` | Radares fijos |
| `trafico sections --kind invive\|animals [filtros]` | Tramos especiales |
| `trafico watch add route\|road\|near ...` / `list` / `remove` / `check` / `run` | Vigilancias |
| `trafico watch service install / uninstall / status` | Programar `watch run` con systemd o launchd |
| `trafico sink list / test` | Destinos de avisos (ntfy, webhook, exec) |
| `trafico feeds list` / `status` | URLs, última descarga, fecha de publicación, tamaño, estado |
| `trafico doctor` | Config, red, feeds, programador |
| `trafico skills list / get` | Guía embebida para agentes |
| `trafico schema [comando]` | JSON Schema de cada salida |
| `trafico config ...` / `version` / `update` | |

### 5.2 Filtros comunes

```
--road A-7 --road AP-7
--pk 100-140                    (solo con una --road)
--direction positive|negative|both
--province alicante
--near casa --radius 15km       (o --lat/--lon)
--type roadworks,accident,closure
--severity high
--status active|planned|all     (por defecto active)
--from 2026-10-31 --to 2026-11-02   (para programadas)
--limit 50 --sort severity|pk|start
```

- `--near` usa distancia en línea recta (haversine) sobre las coordenadas del feed. Sin geocodificación externa: solo lugares guardados o coordenadas.
- `--province` filtra por el campo del feed. Si el registro no lo trae, no se incluye y la respuesta lo cuenta en `meta.unlocated`.

### 5.3 Forma de la respuesta

```json
{
  "schema_version": 1,
  "query": { "road": ["A-7"], "pk": { "from": 100, "to": 140 }, "status": "active" },
  "meta": {
    "feed": "dgt-situations",
    "published_at": "2026-10-28T09:29:00Z",
    "fetched_at": "2026-10-28T09:30:12Z",
    "age_seconds": 72,
    "stale": false,
    "from_cache": true,
    "total": 2,
    "unlocated": 0,
    "coverage_note": "Cobertura de la DGT: no incluye las incidencias gestionadas por la SCT (Cataluña) ni por el Gobierno Vasco"
  },
  "incidents": [ { "...": "modelo 4.1" } ],
  "attribution": { "source": "DGT, Punto de Acceso Nacional", "license": "CC BY", "url": "https://nap.dgt.es/" }
}
```

`coverage_note` nombra **al gestor, no a la provincia**, porque el feed sí trae incidencias en
Barcelona, Girona y Tarragona: son carreteras estatales dentro de Cataluña. Lo que falta son las que
gestiona la SCT. Un agente que diga "no hay datos de Cataluña" estaría mintiendo sobre la AP-7 en
Barcelona. Aparece salvo que el filtro restrinja a provincias donde no hay un gestor autonómico
distinto del Estado.

### 5.4 Rutas

```
trafico route add trabajo --segment A-7:102-118 --segment CV-86:0-6 --direction both
trafico route check trabajo
```

`route check` agrupa la salida por tramo, en el orden de la ruta:

```json
{
  "route": "trabajo",
  "segments": [
    { "road": "A-7", "pk": { "from": 102, "to": 118 }, "incidents": [], "signs": [ { "...": "panel" } ], "radars": [] },
    { "road": "CV-86", "pk": { "from": 0, "to": 6 }, "incidents": [ { "...": "incidencia" } ], "signs": [], "radars": [] }
  ],
  "summary": { "incidents": 1, "high_severity": 0, "closures": 0 },
  "meta": { "...": "como en 5.3" },
  "attribution": { "...": "" }
}
```

### 5.5 Vigilancias

```
trafico watch add route trabajo --window "lun-vie 07:00-09:00" --window "lun-vie 17:30-19:30" --min-severity medium --notify phone
trafico watch add road AP-7 --pk 600-700 --type closure,accident --notify phone
trafico watch service install --interval 5m
```

- **Línea base silenciosa** al crear (salvo `--emit-initial`, que anuncia con `incident.new` en la primera comprobación lo que ya estaba pasando; la elección se guarda con la vigilancia).
- **Ventanas horarias** (`--window`, mismo formato que el `--when` de booksy-cli, días en español o inglés): fuera de la ventana no se comprueba ni se avisa. Hora de Europe/Madrid.
- Eventos: `incident.new`, `incident.updated` (cambia severidad, carriles o fin previsto), `incident.cleared` (desaparece en dos comprobaciones seguidas).
- Intervalo mínimo: 2 minutos. Por defecto: 5.
- Salida de `watch run`: una entrada por vigilancia con `baseline` (primera comprobación), `skipped` (fuera de ventana), `skipped_interval` (el intervalo desde la última comprobación real aún no ha pasado) y `events` con sus tipos. La última comprobación real queda registrada en la base de datos (`last_check_at`, §7) y es la que sostiene el intervalo, sea cual sea la frecuencia del planificador.
- Destinos: `ntfy`, `webhook` (plantillas `discord`, `slack` y `telegram`; la plantilla `telegram` necesita `chat_id` en `[sinks.<nombre>]`) y `exec`. **El aviso nunca incluye el nombre ni las coordenadas de tu ruta**: solo carretera, PK, tipo, severidad y la atribución.

### 5.6 Frescura

- La caché compartida dura 60 segundos. Dos comandos seguidos usan la misma descarga.
- `stale: true` si la fecha de publicación del feed tiene más de 10 minutos (configurable).
- Si la red falla y hay caché, se responde con la caché, `stale: true`, `from_cache: true` y código 0, con un aviso en stderr. Si no hay caché, código 6.

---

## 6. Contrato para agentes

### 6.1 Salida

- stdout: un valor JSON (o JSONL). stderr: diagnóstico y errores en JSON si stdout no es terminal.
- Fechas en ISO 8601 UTC. PK en número.
- Sin ANSI salvo `pretty` en terminal. `pretty` en español, con la hora local de Madrid.

### 6.2 `schema`

`trafico schema incidents` imprime el JSON Schema. Los tests validan la salida real contra él.

### 6.3 Errores y códigos de salida

```json
{"error": {"type": "feed_unavailable", "message": "La DGT no responde y no hay caché local.", "exit_code": 6, "feed": "dgt-situations"}}
```

| Código | Tipos | Significado |
|---|---|---|
| 0 | | Éxito (también con caché caducada: mira `meta.stale`) |
| 1 | `internal` | Error inesperado |
| 2 | `usage`, `cancelled`, `confirmation_required` | Argumentos inválidos (por ejemplo `--pk` sin `--road`), cancelado, falta `--yes` en un borrado local |
| 4 | `not_found` | Incidencia, ruta, lugar o vigilancia inexistente |
| 5 | `rate_limited`, `upstream_unavailable` | 429 o 5xx del servidor de la DGT |
| 6 | `network`, `feed_unavailable` | Sin red y sin caché |
| 7 | `feed_changed` | URL desaparecida (404) o XML con forma inesperada. El mensaje incluye un enlace para abrir issue prellenada |
| 130 | | Interrumpido |

No hay código 3: no existe autenticación.

### 6.4 Skill embebida

```markdown
---
name: trafico
description: Consultar incidencias, obras, paneles y radares de la DGT por carretera, punto kilométrico o zona, y vigilar rutas con la CLI trafico.
---

# trafico

## Reglas
- Usa la salida JSON. Mira siempre meta.published_at y meta.stale antes de decir "ahora".
- Si meta.coverage_note aparece, dilo: la cobertura no incluye lo que gestionan la SCT (Cataluña) ni el Gobierno Vasco. No digas "no hay datos de Cataluña": puede haber incidencias de la DGT en Barcelona, Girona o Tarragona.
- Cita la fuente: "Datos de la DGT".
- No inventes tiempos de viaje ni retenciones: solo lo que viene en el feed.
- Código 7: la DGT ha cambiado algo; díselo al usuario y no reintentes en bucle.

## Recetas
- Estado de una carretera: `trafico incidents --road A-7 --pk 100-140`
- Mi ruta ahora: `trafico route check trabajo`
- Obras del fin de semana: `trafico incidents --road N-332 --type roadworks --status planned --from <sab> --to <dom>`
- Lo que dicen los paneles: `trafico signs --road AP-7 --direction positive`
```

---

## 7. Configuración y almacenamiento

| Qué | Dónde | Permisos |
|---|---|---|
| Config (feeds, lugares, rutas, sinks) | `$XDG_CONFIG_HOME/trafico-cli/config.toml` | `0600` |
| Base de datos (vigilancias, eventos, estado previo) | `$XDG_DATA_HOME/trafico-cli/trafico.db` | `0600` |

El estado de cada vigilancia vive en la base de datos: la línea base de cada incidente, el contador de ausencias, la elección de `--emit-initial` y `last_check_at` (la última comprobación real que sostiene el intervalo mínimo de §5.5).
| Caché de feeds | `$XDG_CACHE_HOME/trafico-cli/` | `0600` |

```toml
[feeds.dgt-situations]
url = "https://nap.dgt.es/datex2/v3/dgt/SituationPublication/datex2_v37.xml"
enabled = true

[feeds.dgt-signs]
url = ""          # se rellena en SPIKE-01
enabled = false

[feeds.dgt-radars]
url = ""          # se rellena en SPIKE-01
enabled = false

[places]
casa = "38.3452,-0.4810"

[stale]
max_age = "10m"

[sinks.phone]
type = "ntfy"
url = "https://ntfy.sh"
topic = "elige-un-topic-largo-y-aleatorio"

[sinks.telegram]
type = "webhook"
url = "https://api.telegram.org/bot<token-del-bot>/sendMessage"
template = "telegram"
chat_id = "123456789"
```

Las URLs por defecto van compiladas en el binario y se pueden sobrescribir en config, para que un cambio de la DGT se arregle sin esperar a una release.

---

## 8. Arquitectura

### 8.1 Stack

| Pieza | Elección | Motivo |
|---|---|---|
| Lenguaje | **Go 1.25+** | Binario único multiplataforma; `encoding/xml` con decodificación en streaming para un feed grande que se lee cada minuto |
| CLI | `cobra` | |
| Config | `go-toml/v2` | |
| SQLite | `modernc.org/sqlite` | Sin CGO |
| Fechas | `time` con `time/tzdata` embebido | Ventanas en hora de Madrid en cualquier máquina |
| Releases | GoReleaser, `install.sh`, Homebrew tap, Scoop | |

Sin dependencias de red más allá de `nap.dgt.es`. Sin geocodificador, sin mapas.

### 8.2 Capas

```
cli (cobra) → app (incidents, route, watch...) → feeds (descarga, caché, frescura)
                    ↓                                   ↓
               model, filter, geo (haversine)      datex2 (parser en streaming + mapping.yaml)
```

- `internal/datex2` es el **único** paquete que conoce el XML. Si la DGT cambia el formato, solo cambia ahí.
- `internal/feeds` descarga con timeout, guarda en caché y expone la fecha de publicación. Usa `If-Modified-Since` o `ETag` solo si SPIKE-01 confirma que el servidor los soporta.

### 8.3 Estructura del repositorio

```
trafico-cli/
├── AGENTS.md
├── CLAUDE.md                 una línea: @AGENTS.md
├── README.md                 con atribución DGT y aviso de fuente no oficial
├── LICENSE                   MIT (el código; los datos son CC BY de la DGT)
├── CHANGELOG.md
├── go.mod                    module github.com/aam9063/trafico-cli
├── Makefile
├── .goreleaser.yaml
├── cmd/trafico/main.go
├── internal/
│   ├── cli/
│   ├── app/
│   ├── feeds/
│   ├── datex2/               parser y mapping.yaml
│   ├── model/
│   ├── road/                 normalización de nombres de carretera
│   ├── filter/
│   ├── geo/                  haversine y lugares guardados
│   ├── window/               parser de --window
│   ├── store/
│   ├── watch/                vigilancias, eventos y sinks
│   ├── output/
│   ├── errs/
│   ├── config/
│   └── agentdocs/            SKILL.md embebido
├── schemas/
├── testdata/
│   └── feeds/                instantáneas reales del feed (datos públicos CC BY)
├── docs/
│   ├── ARRANQUE.md
│   ├── feeds.md              resultado de SPIKE-01: URLs, tamaños, cabeceras, campos
│   ├── cli-spec.md
│   └── adr/
├── odd/
│   ├── tasks/
│   └── reviews/
├── scripts/install.sh
└── .github/workflows/ci.yml
```

---

## 9. Calidad y tests

| Nivel | Qué prueba | En CI |
|---|---|---|
| Unidad | Normalización de carreteras, parser de `--window`, haversine, filtros por PK, mapeo de tipos, cálculo de `status` | Sí |
| Parser | `internal/datex2` contra instantáneas reales guardadas en `testdata/feeds/` | Sí |
| Contrato | Salida valida contra `schemas/` y golden files | Sí |
| Vigilancias | Dos instantáneas consecutivas producen exactamente los eventos esperados; ventanas con reloj controlado | Sí |
| CLI punta a punta | Binario real con HOME temporal; servidor `httptest` que sirve las instantáneas; caché, `stale`, códigos 6 y 7 | Sí |
| En vivo | Descarga real del feed y validación del esquema | Job nocturno (no bloquea PR) |

Reglas:

- TDD: primero el test en rojo.
- Ningún test de PR toca la red. El job nocturno en vivo es el que avisa si la DGT cambia algo.
- Las instantáneas del feed son **datos públicos CC BY**: se pueden versionar tal cual, con un `testdata/feeds/README.md` que indica fecha de captura y atribución. Se recortan a un tamaño razonable con `make fixtures-trim`.
- Benchmark del parser (`go test -bench`) para que leer el feed completo siga siendo rápido.
- `make check` = `gofmt`, `go vet`, `staticcheck`, `govulncheck`, tests.

---

## 10. Flujo de trabajo: ODD en gentle-shell (Pi)

### 10.1 Qué es cada pieza (según su documentación, leída hoy)

- **gentle-ai** configura el agente que ya usas con memoria persistente (Engram), skills y ODD. No instala agentes.
- **gentle-shell** es el espacio de trabajo nativo de Pi con subagentes, vista de cambios, revisión y ODD por defecto. Su paquete npm sigue llamándose `gentle-pi`.
- **ODD**: Authorize, Explore, Resolve uncertainty, Classify, Track (antes de la primera escritura), Implement (tarea a tarea) y Close.
- Las tareas se dimensionan por comprensión, riesgo y capacidad de retomarse. Las pequeñas se hacen en línea; las grandes crean `odd/tasks/<feature>.md` (objetivo, problema, motivo, alcance, restricciones, checklist con IDs estables y criterios de aceptación, evidencia, progreso, siguiente paso), con copia en Engram `odd/<feature>/tasks`.
- Test-first por defecto si hay test ejecutable y resultado esperado claro.
- RDD activo por defecto (`gentle-ai review mode disable` para quitarlo); la profundidad la decide el riesgo del cambio congelado.
- Delegación solo con motivo: exploración, escritores en paralelo con superficies disjuntas, relevo por sesión larga o verificador independiente para cambios de alto riesgo.
- Push, PR y merge son decisiones aparte del commit.

Puede cambiar entre versiones: compruébalo en H-02.

### 10.2 Instalación y comprobaciones (H-02)

```bash
brew install gentleman-programming/tap/gentle-ai
gentle-ai && gentle-ai doctor && gentle-ai review mode status
npm i -g gentle-pi
gentle-shell              # dentro: /gentle:status y /gentle:doctor
# tras actualizar gentle-ai: gentle-ai sync
```

### 10.3 Backlog y ODD

| Este documento | ODD |
|---|---|
| Una tarea (`FEED-01`) | Una funcionalidad, con su documento si es grande |
| Una ola | Un lote autorizado por ti en Authorize |
| Criterios de aceptación | Checklist con IDs estables |
| Superficie de edición | Permite escritores en paralelo sin solaparse |
| "Cuándo parar y preguntar" | Resolve uncertainty |

### 10.4 Plantilla del documento de funcionalidad

````markdown
# <ID>: <título>

## Objetivo

## Problema y motivo

## Alcance
- Dentro:
- Fuera:

## Restricciones
Principios de la sección 3 que aplican.

## Superficie de edición (nuestra)

## Checklist
- [ ] <ID>.1 ... · Criterio: ...

## Evidencia de verificación

## Progreso

## Siguiente paso

## Log
````

### 10.5 Review gate tras cada implementación (capa propia)

1. Tarea implementada y `make check` en verde.
2. Se congela `<base>..<sha>`.
3. Subagente revisor de contexto limpio y **solo lectura**, con otro modelo si es posible.
4. Orden: corrección; **P-04, P-05, P-07 y P-08** (fidelidad, atribución, privacidad de ubicaciones, frescura); contrato JSON y códigos de salida; zonas horarias; simplicidad y duplicación; tests; rendimiento del parser con evidencia del benchmark.
5. Informe en `odd/reviews/<id>.md` con severidades, archivo y línea, propuesta y veredicto.
6. Una sola ronda de correcciones. Refactores al backlog como `REF-<n>`.
7. El CI falla si un PR con ID de tarea no trae su informe.

### 10.6 Entrega

Ramas `tr/<id>-descripcion`, un PR por tarea.

> **Corregido el 2026-10-28 (ADR 0002).** El agente commitea, pushea, abre el PR y lo mergea
> contra `dev` con el CI en verde. `main` queda reservada a releases y solo la mueve una persona.
> Un PR por tarea, historia lineal en `dev` (merge con rebase) y la rama se borra al mergear.

---

## 11. AGENTS.md (copiar tal cual)

````markdown
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
- Rama `tr/<id>-descripcion`. Commit y PR. No hagas merge a `main`.

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
````

---

## 12. CI

```yaml
name: ci
on:
  push:
  pull_request:
  schedule:
    - cron: "17 5 * * *"     # comprobación nocturna del feed real
jobs:
  check:
    if: github.event_name != 'schedule'
    strategy:
      matrix: { os: [ubuntu-latest, macos-latest, windows-latest] }
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.25" }
      - run: make check
  review-report:
    if: github.event_name == 'pull_request'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: ./scripts/require_review.sh "$PR_TITLE"
        env:
          PR_TITLE: ${{ github.event.pull_request.title }}
  live:
    if: github.event_name == 'schedule'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.25" }
      - run: TRAFICO_LIVE=1 go test -tags live ./...
```

Si el job nocturno falla, abre una issue automática "La DGT ha cambiado el feed".

---

## 13. Riesgos

| Riesgo | Mitigación |
|---|---|
| La DGT cambia URLs o formato (ya pasó el 30-09-2026) | URLs en config, código 7, tests contra instantáneas reales, job nocturno con issue automática |
| Un agente da por actual un dato viejo | `published_at`, `age_seconds`, `stale` en cada respuesta y regla en la skill |
| "No hay incidencias" en Cataluña o País Vasco por falta de datos | `coverage_note` y regla en la skill |
| Feed grande leído muchas veces | Caché de 60 s compartida, streaming, benchmark, cabeceras condicionales si existen |
| Mapeo de tipos DATEX2 incorrecto | Construido sobre el perfil oficial en SPIKE-01, `subtype_raw` siempre presente, tests tabulados |
| Fuga de ubicaciones personales | P-07, avisos sin ruta ni coordenadas, config `0600` |
| Uso como si fuera oficial | Aviso en README y en `doctor`, atribución en cada salida |

---

## 14. Backlog

Prefijos: H, SPIKE, OPS, CORE, FEED, MODEL, CMD, ROUTE, WATCH, AGENT, QA, REL, REF.

### Ola 0: preparación

| ID | Tarea | Depende | Criterio de aceptación |
|---|---|---|---|
| H-01 | Leer el aviso legal de la DGT y la ficha de licencia de cada dataset que se quiera usar (incidencias, paneles, radares, tramos, SCT, DT-GV) | | `docs/adr/0001-licencias.md` con la licencia de cada uno y si se puede usar |
| H-02 | Instalar y comprobar gentle-ai y gentle-shell | | Notas en `docs/adr/0002-workflow.md` |
| H-03 | Proteger `main` | OPS-01 | Regla activa |
| SPIKE-01 | Descargar los feeds reales y documentar: URLs vigentes, tamaño, frecuencia, cabeceras (`ETag`, `Last-Modified`, compresión), estructura de cada registro, tipos DATEX2 presentes, campos de provincia y PK. Construir `mapping.yaml` desde el perfil oficial. Guardar instantáneas | H-01 | `docs/feeds.md` y `testdata/feeds/` con al menos 3 instantáneas consecutivas por feed |

### Ola 1: núcleo

| ID | Tarea | Depende | Criterio de aceptación |
|---|---|---|---|
| OPS-01 | Repo con estructura 8.3, Makefile, CI, AGENTS.md | | `make check` verde en los tres SO |
| CORE-01 | Salida, sobre de error, códigos de salida y bloque `attribution` | OPS-01 | Tests e2e de stdout limpio, códigos y atribución presente |
| CORE-02 | Config, XDG, escrituras atómicas | OPS-01 | Tests con HOME temporal |
| FEED-01 | Descarga con timeout, caché de 60 s, frescura (`published_at`, `stale`), respaldo en caché si falla la red | CORE-02, SPIKE-01 | Tests con `httptest`: caché, `stale`, códigos 6 y 7 |
| FEED-02 | Parser DATEX2 de situaciones en streaming y `mapping.yaml` | SPIKE-01 | Parsea las instantáneas sin errores; benchmark documentado |
| MODEL-01 | Modelo `Incident`, normalización de carreteras, cálculo de `status`, JSON Schema v1 | FEED-02 | Golden y tests tabulados (`N-332a`, `CV-80`, `AP-7`) |
| OPS-02 | Review gate en CI y plantillas | OPS-01 | PR sin informe falla |

### Ola 2: consultas

| ID | Tarea | Depende | Criterio de aceptación |
|---|---|---|---|
| CMD-01 | `incidents` y `incidents show` con todos los filtros de 5.2 y `coverage_note` | MODEL-01, FEED-01 | Golden por filtro; `--pk` sin `--road` da código 2 |
| ROUTE-01 | `place` y `route add/list/show/remove/check` | CMD-01 | `route check` agrupa por tramo en orden |
| CMD-02 | `feeds list/status` y `doctor` | FEED-01 | Muestra URL, edad y estado de cada feed |

### Ola 3: vigilancias

| ID | Tarea | Depende | Criterio de aceptación |
|---|---|---|---|
| WATCH-01 | `watch add/list/remove/check/run`, línea base, ventanas, eventos y sinks | ROUTE-01 | Dos instantáneas producen exactamente un `incident.new`; fuera de ventana no hay aviso; el aviso no contiene coordenadas ni nombre de ruta |
| WATCH-02 | `watch service install/uninstall/status` | WATCH-01 | Linux y macOS sin restos al desinstalar |

### Ola 4: más datasets (solo los que H-01 apruebe)

| ID | Tarea | Depende | Criterio de aceptación |
|---|---|---|---|
| FEED-03 | Paneles: parser y `signs` | H-01, SPIKE-01 | Golden con instantánea real |
| FEED-04 | Radares fijos: parser y `radars` | H-01, SPIKE-01 | Igual |
| FEED-05 | Tramos INVIVE y animales: `sections` | H-01, SPIKE-01 | Igual |
| ROUTE-02 | `route check` incluye paneles y radares | FEED-03, FEED-04 | Golden de ruta completa |
| FEED-06 | Cataluña (SCT) y País Vasco (DT-GV) | H-01 positivo para ambos | `coverage_note` desaparece donde haya datos |

### Ola 5: agentes y publicación

| ID | Tarea | Depende | Criterio de aceptación |
|---|---|---|---|
| AGENT-01 | `skills` y `schema` | Olas 2 y 3 | Un agente sin contexto resuelve las cuatro recetas de 6.4 solo con `skills get` |
| AGENT-02 | `docs/cli-spec.md` generado y README con atribución | AGENT-01 | Referencia al día con el binario |
| REL-01 | GoReleaser, checksums, `install.sh`, tap, Scoop, `update` | AGENT-02 | `v0.1.0` instalable en una línea |
| QA-01 | Pase de arquitectura final | REL-01 | `odd/reviews/ola-5.md` y tareas REF |

**Sugerencia de corte:** `v0.1.0` tras las Olas 1 a 3 (incidencias, rutas y vigilancias). Paneles y radares en `v0.2.0`.

---

## 15. Decisiones abiertas

| ID | Decisión | Recomendación |
|---|---|---|
| D-01 | Comandos en inglés (`incidents`, `route`) o en español (`incidencias`, `ruta`) | Inglés, como el resto de tus CLIs y el ecosistema; salida `pretty` en español. Alias en español si te apetece |
| D-02 | Nombre del binario | `trafico` |
| D-03 | ¿Añadir geocodificación (`--near "Elche"`) más adelante? | No en la v1. Si se añade, con política de uso confirmada del servicio elegido |
| D-04 | Modelo del implementador y del revisor | Distintos; decidir con la tasa de hallazgos aceptados |

---

## 16. Prompt de arranque para gentle-shell

```
Lee docs/ARRANQUE.md, docs/feeds.md y AGENTS.md. Trabajamos con ODD:
Authorize, Explore, Resolve uncertainty, Classify, Track, Implement, Close.

Autorizo la Ola 1 (sección 14). Test-first activo.

Orden:
1. OPS-01.
2. Cuando cierre, en paralelo si las superficies son disjuntas:
   CORE-01, CORE-02 y FEED-02.
3. Después: FEED-01, MODEL-01 y OPS-02.

Reglas:
- Si la tarea es grande, su documento odd/tasks/<id>-<slug>.md con la plantilla
  de la sección 10.4 antes de escribir código.
- make check en verde y review gate (sección 10.5) antes de cerrar.
- Ningún test de PR toca la red: usa las instantáneas de testdata/feeds/.
- Commit en ramas tr/<id>-descripcion y PR. No hagas merge a main.
- Para y pregunta ante cualquier punto de "Cuándo parar y preguntar".

Al terminar cada tarea: qué cambió, cómo lo verificaste y qué dijo la review,
en tres líneas.
```

---

## 17. Fuentes consultadas

- [Incidencias DGT DATEX2 v3.7 en el NAP](https://nap.dgt.es/en/dataset/incidencias-dgt-datex2-v3-7): URL, licencia CC BY, frecuencia, cobertura.
- [Datasets DATEX2 de la DGT en el NAP](https://nap.dgt.es/en/dataset?tags=datex2&tags=dgt&tags=datex2v3)
- [Guía de utilización de DATEX II](https://nap.dgt.es/en/dataset/guia-de-utilizacion-de-datex-ii)
- [Issue de trafico.live sobre la retirada de los feeds v1](https://github.com/abemon-es/trafico.live/issues/75)
- Proyectos con los mismos datos: [HayTrafico](https://github.com/ivansosaglez/HayTrafico), [copiloto-alerts](https://github.com/novaqlabs/copiloto-alerts), [ICEbot-dgt](https://github.com/scaverod/ICEbot-dgt)
- Patrón de CLI para agentes: [booksy-cli](https://github.com/GodGMN/booksy-cli), [vodafone-cli](https://github.com/luisgonzaleznf/vodafone-cli), [wallapop-cli](https://github.com/Microck/wallapop-cli)
- [gentle-ai](https://github.com/Gentleman-Programming/gentle-ai) · [gentle-shell](https://github.com/Gentleman-Programming/gentle-shell)
