# OLA-3: vigilancias (watch, eventos y avisos)

Autorizado el 2026-10-29. Alcance: las tareas de la Ola 3 del backlog de `docs/ARRANQUE.md`.
**Estado: cerrada.** Seis PRs en `dev`, CI verde en los tres SO.

## Specs

- **S1** — Git: `main` solo releases y solo la mueve el usuario; el agente mergea siempre contra
  `dev`. Cita: "Mientras mergea siempre hacia dev".
- **S2** — Alcance autorizado: Ola 3 completa (`WATCH-01`, `WATCH-02`).
- **S3** — **Línea base silenciosa** al crear una vigilancia (§5.5).
- **S4** — **Ventanas horarias**: fuera de la ventana no se comprueba ni se avisa. Europe/Madrid.
- **S5** — **Eventos**: `incident.new`, `incident.updated` (severidad, carriles o fin previsto),
  `incident.cleared` (dos comprobaciones seguidas).
- **S6** — Intervalo mínimo 2 minutos, por defecto 5.
- **S7** — **El aviso nunca incluye el nombre ni las coordenadas de la ruta** (P-07).
- **S8** — Destinos: `ntfy`, `webhook` (discord/slack/telegram) y `exec`.
- **S9** — `watch service install/uninstall/status` sin restos al desinstalar.
- **S10** — Criterio de aceptación: dos instantáneas producen exactamente un `incident.new`; fuera de
  ventana no hay aviso; el aviso no contiene coordenadas ni nombre de ruta.

## Tasks

| ID | Tarea | Commits | PR |
|---|---|---|---|
| T1 | WATCH-01a: `internal/window`, parser de ventanas | `ac31be3` | #21 |
| T2 | WATCH-01b: `internal/store`, SQLite sin CGO | `46b5ef5` | #22 |
| T3 | WATCH-01c: motor de eventos, mensajes y sinks | `20e249d` | #23 |
| T4 | WATCH-01d: definiciones (schema v3) y comandos | `261d23f`, `5601315` | #24, #25 |
| T5 | WATCH-02: servicio systemd y launchd | `eea4dfe` | #26 |

## Evidencia de verificación

**S10, el criterio de aceptación**, verificado por tests y por el revisor independiente:

- Dos instantáneas consecutivas producen **exactamente un** `incident.new`
  (`TestRunWatchBaselineThenExactlyOneNew`, más el probe del revisor).
- Fuera de la ventana no se comprueba ni se avisa, y **no se hace ni una petición** al feed.
- El payload **no tiene campo** para el nombre de la vigilancia, la ruta, el lugar ni las
  coordenadas: P-07 se cumple por construcción, no filtrando. El revisor comparó el conjunto exacto
  de claves del payload para `new` y `cleared` y renderizó mensajes con una vigilancia llamada como
  una persona y una ruta con coordenadas.

**Mensaje real de un aviso de fin:**
`Incidencia finalizada: restricción en A-372 PK 50.25-50.65. Fuente: DGT (CC BY)`

**Transversal**

- CI verde en ubuntu, macOS y Windows en los seis PRs; `check.sh --strict` en cada uno.
- `govulncheck` limpio en toda la ola.
- `CGO_ENABLED=0` en cada build y test: promesa de binario único, sin compilador de C.
- Migraciones verificadas contra bases **creadas por el binario anterior**, no solo por fixtures:
  v1→v2 (estado y cola intactos) y v2→v3 (idem), más dos procesos migrando a la vez sin colisión.

**Defectos que la revisión independiente encontró**

La ola tuvo **dos blockers y siete majors**. Ninguno rompía el programa; todos hacían que funcionara
mal en silencio:

1. **`PruneEvents` borraba avisos sin entregar** (blocker). Un pendiente más viejo que el corte se
   destruía para siempre. El test existente no lo detectaba porque su única fila pendiente era más
   nueva que el corte: verde por la razón equivocada.
2. **Un sink inutilizable se descartaba con exit 0** (blocker). `--notify tg` emitía el evento, no
   podía construir el sink, lo tiraba y devolvía éxito.
3. **`--min-severity low` excluía `unknown`**, que la DGT publica para el ~87 % de los registros: el
   suelo que el usuario lee como "cualquier severidad" dejaba la vigilancia ciega.
4. **Un lock que no bloqueaba**: el takeover del lock caducado no revalidaba identidad, así que dos
   procesos podían quedarse con él.
5. **El intervalo se validaba y nunca se aplicaba**: dos `run` a 30 s hacían dos descargas; con un
   scheduler cada minuto, 1440 peticiones diarias, contra P-06.
6. **`direction: "both"`** no aplicaba a la ola, pero el patrón sí: valores límite aceptados en
   silencio.

**Las dos lecciones de método**

- Los defectos que sobreviven a los tests propios de una tarea son casi siempre **valores límite
  aceptados en silencio**: `NaN`, `-5`, `""`, `{null,null}`, `windows: []`. El camino feliz siempre
  está cubierto. Lo que falta es rechazar lo raro en vez de decidir por el usuario.
- Solo los encuentra quien **muta el código y prueba**. El revisor que borró el chequeo de longitud
  de `validatePlace` para ver si el test lo detectaba; el que corrió dos procesos concurrentes; el
  que hizo no transaccional `ReplaceState` para comprobar que el test de atomicidad tenía dientes.

## Progreso

Ola 3 cerrada. `dev` acumula **33 commits** sobre `main`, que sigue en el bootstrap: solo la mueve
una persona y no hay release todavía.

## Siguiente paso

- **Merge `dev` → `main`**: decisión del usuario.
- **Deudas anotadas**, cada una con su PR: `--emit-initial` (§5.5 lo menciona y no existe);
  `chat_id` en `config.Sink` para que un sink `telegram` sea configurable; documentar
  `skipped_interval` y `last_check_at` en `ARRANQUE` §5.5.
- **Reviews nativas RDD pendientes**: cinco candidatos, bloqueadas porque Pi no recarga las
  definiciones de agentes en el proceso actual. Requieren reiniciar Pi.

## Log

- **L1** (usuario, verbatim): "Vas a empezar la Ola 3 no? lo digo por mergear de mientras dev a main
  yo"
- **L2**: `dev` → `main` lo hará el usuario cuando corresponda; el agente no toca `main`.
- **L3**: `internal/config` quedó fuera de superficie durante toda la ola; las rutas y lugares ya
  estaban en `internal/app` desde ROUTE-01.
- **L4**: el review nativo RDD no funcionó en esta sesión por configuración de modelos del harness;
  el trabajo se validó con revisores independientes de contexto limpio más el gate de CI.
