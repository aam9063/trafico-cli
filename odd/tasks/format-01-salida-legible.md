# FORMAT-01 · invertir el formato de salida por defecto

## Specs

- **S1** — La salida legible (`pretty`) pasa a ser el comportamiento **por defecto** de todos los comandos.
  > "quiero que tenga formato asi que tendras que integrar pretty para que no salga en formato json"

- **S2** — Se ofrece un flag global `--json` para forzar JSON puro, que es lo que un agente necesita para parsear.

- **S3** — El JSON emitido con `--json` es **byte por byte idéntico** al que hoy emite el comando, incluidos `schema_version`, `meta` y `attribution`. El cambio es qué se muestra por defecto, no qué se serializa.

- **S4** — `AGENTS.md` deja de declarar `stdout solo JSON` como regla dura y pasa a declarar: por defecto texto legible; con `--json`, un único valor JSON. `README.md` y `docs/` reflejan el mismo contrato.

- **S5** — Ningún test toca la red, y cada comando conserva una prueba de que `--json` produce el contrato completo.

## Alcance medido (hecho antes de tocar código)

- 15 llamadas a `flag.GetBool("pretty")` en `internal/cli/*.go`, todas reemplazables por un helper único.
- `addPrettyFlag` en `internal/cli/root.go:47` registra el flag con `default false`; hay que invertirlo y añadir `--json`.
- **37 tests de CLI asumen JSON por defecto.** Con un helper en `runCLI` que añade `--json` salvo que la invocación ya elija formato, bajan a **4**.
- Esos 4 restantes revelan un defecto de fondo: **varios comandos que emiten envelope no registran el flag**. `incidents show`, `route list` y `route show` responden `unknown flag: --json`. Con el default invertido eso es una inconsistencia de contrato, no solo un test rojo: esos comandos seguirían escupiendo JSON crudo mientras el resto muestra texto.
- `route check` y otros tres sí lo registran (`internal/cli/route.go:94,115,192`).

## Tasks

| ID | Spec | Descripción |
| --- | --- | --- |
| T1 | S1, S2 | Helper único de decisión de formato, `--json` global y `--pretty` oculto y aceptado |
| T2 | S3 | Migrar los 15 comandos al helper sin cambiar bytes del JSON |
| T3 | S1 | Registrar los flags en **todos** los comandos que emiten envelope (`incidents show`, `route list`, `route show` y los que falten) |
| T4 | S5 | Arreglar los 4 tests restantes y añadir una prueba por comando de que `--json` sigue dando el contrato |
| T5 | S4 | `AGENTS.md`, `README.md` y `docs/` con el contrato nuevo |

## Decisión abierta

`--pretty` queda como flag aceptado y oculto (no rompe guiones existentes) o se elimina (superficie más chica). Se decide en T1.

## Log

- **L1** — «ves por orden. empieza por realineae dev, luego el tag, luego R3-001 y por yltimo ola 5. Quiero que tengas presente que quiero que tenga formato asi que tendras que integrar pretty para que no salga en formato json.»
- **L2** — Elegida la opción "pretty por defecto siempre, --json para forzar" frente a la detección automática de terminal. Queda registrado que es un cambio incompatible con la regla dura vigente y que el release no se etiqueta antes de cerrarlo.
- **L3** — `dev` realineado con `main` por merge (`fad2d9d`), sin reescribir historial: `main` es ancestro de `dev` y el árbol es idéntico.
- **L4** — Primer intento de implementación medido y **revertido**: llegó a 33 de 37 tests en verde, pero dejaba el árbol en rojo y revelaba que faltan flags en tres comandos. El código se descartó para no commitear tests rotos; queda el alcance medido, que es lo que faltaba saber.
