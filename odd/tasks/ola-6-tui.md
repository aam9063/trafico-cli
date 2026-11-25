# Ola 6 · TUI

## Specs

- **S1** — `trafico` sin argumentos, en una terminal interactiva, abre la TUI. Sin terminal (pipe, CI, agente) imprime la ayuda como hoy y **nunca** intenta dibujar. Es la misma regla que `alsa`: `stdin` o `stdout` que no son TTY significan que no hay nadie mirando.
- **S2** — La TUI tiene estas vistas: incidencias, paneles, radares, tramos, rutas y ayuda. Se cambia entre ellas con las teclas `1`–`5` y `?` para la ayuda; `q` y `ctrl+c` salen.
- **S3** — El estilo es una ventana: borde redondeado, barra de título con el nombre de la vista y el contador, y una barra de atajos al pie. Paleta oscura con dos acentos, sin ANSI de 16 colores.
- **S4** — Las incidencias se navegan con `↑`/`↓`, `j`/`k`, `g`/`G` (principio/fin) y `enter` abre el detalle de la seleccionada. El detalle es una pantalla, no un panel lateral: en 80 columnas no cabe al lado.
- **S5** — Toda la TUI lee por las mismas funciones de `internal/app` que usan los comandos. **Ninguna consulta nueva, ningún parser nuevo.** Si la TUI y el CLI se desviaran, sería un segundo producto.
- **S6** — Los feeds se cargan en una goroutine al abrir cada vista, con un indicador de carga y un mensaje de error legible si falla. La interfaz no se bloquea esperando la red.
- **S7** — `CGO_ENABLED=0` sigue compilando y los tres targets del release siguen construyéndose.
- **S8** — Un test cubre el modelo (teclas, cambio de vista, selección) sin terminal real, y una grabación con VHS se revisa a ojo: la interfaz se mira, no se supone.

## Alcance medido

- El binario tiene 12 comandos con salida de texto ya resuelta (`internal/cli/render_*.go`). La TUI **no reutiliza esos renderers**: reutiliza los datos de `internal/app` y dibuja con Lipgloss. Son dos presentaciones del mismo dato, y la de texto sigue siendo la que un agente consume.
- No hay dependencias TUI hoy. Entran `bubbletea`, `lipgloss` y `bubbles`, las tres pure Go y sin cgo.

## Decisiones

- **Bubbletea, no tview.** Elm architecture: `Update` devuelve un modelo nuevo, así el estado es testeable sin terminal. tview muta widgets y obliga a testear contra una pantalla falsa.
- **Un `internal/tui` propio, aislado de `internal/cli`.** El paquete `cli` es cobra y flags; mezclar widgets ahí haría que un cambio de estilo pudiera romper un contrato de salida.
- **La TUI no es la forma principal.** El contrato del proyecto es JSON para agentes y texto para personas; la TUI es una tercera comodidad para quien abre el binario a mano.

## Tasks

| ID | Spec | Descripción |
| --- | --- | --- |
| T1 | S7 | Añadir bubbletea, lipgloss y bubbles; verificar `CGO_ENABLED=0` y los tres targets |
| T2 | S3 | Tema, ventana, barra de título y barra de atajos compartidos por todas las vistas |
| T3 | S5, S6 | Carga asíncrona de feeds con estados de carga y error |
| T4 | S2 | Navegación entre vistas y ayuda |
| T5 | S4 | Lista de incidencias navegable y su detalle |
| T6 | S2 | Las otras cuatro vistas, más simples: tabla y navegación |
| T7 | S1 | Enganche en `cmd/trafico`: sin argumentos y con TTY |
| T8 | S8 | Tests del modelo y grabación de verificación con VHS |
| T9 | S5, S6 | Buscador por provincia (`/`, `enter`, `esc`) en las cuatro vistas |
| T10 | S3 | Marco con métrica exacta: ninguna línea se sale del ancho terminal |

## Log

- **L1** — «quiero hacerle TUI y que tenga buena UI la terminal como este proyecto https://github.com/ahorrandocon20/alsa-cli ... no tan pro pero que sea bonita de ver. Y bueno terminar de pulir detalles y que sea 100% funcional.»
- **L2** — La referencia usa Textual (Python). El equivalente en Go es Bubbletea + Lipgloss; la paleta y la estructura de ventana se copian, la dependencia no.
- **L3** — «me faltó el buscador por provincia, como alsa-cli, solo provincia no municipio». El buscador entra en `internal/tui` únicamente: `filter.Criteria` ya acepta `Provinces` y `ProvinceEqual` pliega acentos y pares bilingües (`alicante` encuentra `Alacant/Alicante`). `Criteria` no acepta municipio, y añadirlo habría exigido tocar `filter` + `app` + `tui`; queda descartado (T9).
- **L4** — «los bordes están desmaquetados». Tres causas: `styleFrame` llevaba `Border()`, así que cada fragmento pintado se envolvía en su propia caja; el arte y la tabla son bloques multilínea y se insertaban como un solo elemento, recibiendo borde solo la primera línea; y el borde inferior calculaba su relleno con `len()` en vez de celdas, saliendo una columna de más. Ahora el marco se ensambla a mano con anchura exacta medida en celdas (`ansi.StringWidth`), el arte se oculta por debajo de 80 columnas, y un test mide todas las líneas del marco de 40 a 140 columnas (T10). El banner se rehízo para escribir `TRAFICO-CLI` completo, guion incluido.
- **L5** — La barra de atajos degrada por prioridad: primero caen las pistas de movimiento, la del buscador sobrevive hasta 90 columnas, y en terminales muy estrechas queda solo la vista actual entre corchetes más `q salir`. `q salir` no se cae nunca.
