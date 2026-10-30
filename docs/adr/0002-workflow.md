# ADR 0002 — Flujo de trabajo, ramas y harness

- **Estado**: aceptado
- **Fecha**: 2026-10-28
- **Tarea**: H-02
- **Sustituye**: `docs/ARRANQUE.md` §10.6 ("Merge siempre humano"), por decisión del 2026-10-28

## Contexto

El documento de arranque asumía un flujo con PR abierto por el agente y merge humano. La decisión
del 2026-10-28 cambia la frontera: el agente commitea, pushea, abre el PR y lo **mergea contra
`dev`**; `main` queda reservada a releases y solo la mueve una persona.

Al mismo tiempo hay que dejar constancia del harness real con el que se trabaja, porque el
`ARRANQUE.md` describe herramientas que pueden cambiar entre versiones ("puede cambiar entre
versiones: compruébalo en H-02").

## Decisión

### Ramas

| Rama | Quién escribe | Regla |
|---|---|---|
| `main` | solo una persona | Protegida. Solo releases (`v0.x.y`). Nunca se pushea ni se mergea desde el agente. |
| `dev` | el agente, vía PR | Integración continua. Es la rama por defecto del repositorio. |
| `tr/<id>-<slug>` | el agente | Una tarea del backlog por rama, nacida de `dev`. |
| `fix/<slug>` | el agente | Corrección puntual sin ID de tarea. |

Ciclo por tarea:

1. `git checkout dev && git pull --ff-only` antes de crear la rama (evita ramas out of date).
2. Trabajo, commits en unidades revisables (~400 líneas cambiadas), tests y docs junto al código.
3. `git rebase dev` si `dev` avanzó mientras tanto.
4. `git push -u origin tr/<id>-<slug>` y `gh pr create --base dev`.
5. Merge a `dev` con `gh pr merge --rebase --delete-branch`, solo con el CI en verde.
6. La rama se borra: no se dejan ramas muertas.

`dev` nunca se mergea a `main` sin decisión humana explícita. El cierre de una ola es el momento
natural para proponerlo, no para ejecutarlo.

### Contenido de un PR

- Título en formato Conventional Commits con el ID de tarea: `feat(incidents): ... (CMD-01)`.
- Cuerpo con: tarea, qué cambió, hallazgos, cómo se verificó, y qué queda fuera.
- El CI de PR no toca la red (ver `ARRANQUE.md` §9). La comprobación en vivo es el job nocturno.
- Un PR de tarea incluye su informe de review gate en `odd/reviews/<id>.md` (§10.5).

### Idioma de los artefactos

- **Documentación del proyecto** (`docs/`, `README.md`, ADR, `odd/`): español, porque la fuente de
  verdad (`docs/ARRANQUE.md`) y el público objetivo son hispanohablantes.
- **Código, comentarios de código, nombres de tests, mensajes de commit, mensajes de error del
  programa y claves JSON**: inglés, como el resto del ecosistema Go y las CLIs de referencia.
- **Salida `pretty` de la CLI**: español, con hora local de Madrid.
- La ayuda de cobra (`--help`) va en inglés por coherencia con los nombres de comandos y flags.

### Decisiones abiertas que quedan cerradas

- **D-01**: comandos y flags en inglés (`trafico incidents --road A-7 --pk 100-140`). Sin alias en
  español en la v1: dos vocabularios duplican la superficie de test y de documentación. Se pueden
  añadir después como alias de cobra sin romper el contrato.
- **D-02**: el binario se llama `trafico`.
- **D-03**: sin geocodificación en la v1. `--near` acepta un lugar guardado o `--lat/--lon`; nunca
  una dirección escrita en lenguaje natural.
- **D-04** (modelo del implementador y del revisor): queda pendiente hasta que exista el primer
  ciclo de review gate con evidencia. El criterio de decisión es la tasa de hallazgos aceptados,
  no una preferencia previa.

### Harness verificado (2026-10-28)

| Pieza | Versión / estado |
|---|---|
| `pi` (gentle-shell) | 1.4.0, en `C:\nvm4w\nodejs\pi.cmd` |
| `gentle-ai` | 4.3.1 |
| Extensiones de Pi | `gentle-pi` 1.4.0, `gentle-engram`, `pi-btw`, `pi-web-access` |
| Memoria | Engram alcanzable por handshake MCP stdio |
| RDD | `on` por decisión global; `clone-local` sin definir |
| Agentes configurados por gentle-ai | cursor, vscode-copilot, claude-code, pi |

`gentle-ai doctor` → 6 comprobaciones OK, 0 fallos, 5 avisos, estado `degraded`. Avisos y qué
hacer con ellos:

1. **`tool:gentle-ai` duplicado en PATH** (`C:\Users\alber\go\bin\gentle-ai.exe` 4.3.1 y
   `C:\Users\alber\AppData\Local\gentle-ai\bin\gentle-ai.exe` 2.1.5). Decisión del propietario
   del entorno (2026-10-28): **se dejan las dos copias**. Riesgo aceptado: si el orden del PATH
   cambia en otra terminal o en un IDE, se ejecutaría la 2.1.5 sin aviso. La copia buena (4.3.1)
   es la que resuelve hoy.
2. **`tool:engram` duplicado** (`AppData\Local\engram\bin` 3.6.0 y `go\bin` 2.0.0). Misma
   decisión: se dejan. La buena (3.6.0) es la que resuelve hoy.
3. **`installed:asset_version`** — *resuelto el 2026-10-28*. Los assets estaban configurados por
   gentle-ai 4.2.0 y el binario era 4.3.1. Se ejecutó `gentle-ai sync`: 22 pasos aplicados, sin
   tocar ningún archivo del repositorio (`git status` limpio, `AGENTS.md` verificado por hash).
   El doctor pasó a `installed assets match running binary version (4.3.1)`, 7 comprobaciones OK
   y 4 avisos. Efecto colateral a tener en cuenta: los 8 agentes de review de
   `~/.claude/agents/` (`jd-fix-agent`, `jd-judge-a/b`, `review-readable/refuter/reliability/
   resilience/risk`) se **preservaron sin actualizar** porque su ownership no se pudo verificar.
   Siguen funcionando; adoptarlos requeriría borrarlos y re-sincronizar, que es una decisión del
   propietario, no de este repo.
4. **`tool:claude`**: el sondeo de versión falla y `claude` no se usa en este proyecto. Se ignora.
5. **`pi:mcp`**: Pi tiene MCP deshabilitado en `settings.json` y `pi-mcp-adapter` no está. Engram
   responde igualmente por su propia integración, así que no bloquea nada de este repo.

Ninguno de los avisos que quedan bloquea el trabajo. Los dos de duplicados son deuda de entorno
del usuario, no del proyecto.

## Consecuencias

- El agente tiene autonomía hasta `dev` y ninguna sobre `main`. Es la frontera correcta: `dev` es
  reversible con un revert de PR; una release publicada, no.
- El CI pasa a ser el guardián real del merge. Mientras no exista CI (llega en OPS-01), el merge a
  `dev` se apoya en `make check` local ejecutado por el agente, y eso debe decirse en el PR.
- Se corrige `docs/ARRANQUE.md` §10.6 para que no contradiga este ADR.

## Verificación

- `gentle-ai --version`, `pi --version`, `gentle-ai doctor` y `gentle-ai review mode status`
  ejecutados el 2026-10-28; sus salidas están resumidas arriba.
- `git remote -v` apunta a `github.com/aam9063/trafico-cli`; `main` y `dev` existen en el remoto y
  `dev` es la rama por defecto.
