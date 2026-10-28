# trafico-cli

CLI en Go que lee los datos abiertos de tráfico de la DGT, los normaliza a JSON y los deja listos
para que un agente de IA los consulte: incidencias, obras, paneles de mensaje variable, radares
fijos y rutas guardadas con vigilancias.

Estado: **diseño / Ola 0**. Todavía no hay binario publicado. La fuente de verdad del proyecto es
[`docs/ARRANQUE.md`](docs/ARRANQUE.md).

## Qué hace

- Consulta incidencias por carretera, punto kilométrico, zona o ruta guardada.
- Lee lo que muestran ahora los paneles de mensaje variable de la DGT.
- Lista radares fijos.
- Guarda rutas y lugares en tu máquina y vigila cambios con avisos al móvil.
- Emite siempre JSON en stdout, con un contrato estable (`schema_version`) y códigos de salida
  documentados, para que un agente sepa qué significa cada respuesta.

## Qué no hace

- No calcula rutas ni estima tiempos de viaje.
- No predice tráfico: muestra lo que la DGT publica, cuando lo publica.
- No cubre Cataluña ni País Vasco en la v1: el feed de la DGT no las incluye.
- No lleva un LLM dentro: la CLI es determinista, el agente razona.

## Atribución

Los datos son de la **DGT, Punto de Acceso Nacional** (<https://nap.dgt.es/>), publicados bajo
licencia **Creative Commons Atribución (CC BY)**. Cada respuesta JSON incluye un bloque
`attribution` con la fuente, la licencia, la URL y el instante de descarga.

El código de este repositorio se distribuye con licencia MIT (ver [`LICENSE`](LICENSE)). La
licencia MIT cubre el código, no los datos.

## Aviso: no es una fuente oficial

Esta herramienta es un cliente no oficial. No sustituye a los canales oficiales de la DGT ni a
los paneles de tráfico reales. Antes de tomar decisiones de circulación, consulta las fuentes
oficiales.

## Instalación

Pendiente de la tarea `REL-01` (GoReleaser, `install.sh`, Homebrew tap, Scoop).

## Documentación

- [`docs/ARRANQUE.md`](docs/ARRANQUE.md) — diseño, alcance, backlog por olas.
- [`docs/feeds.md`](docs/feeds.md) — catálogo real de feeds de la DGT (SPIKE-01).
- [`AGENTS.md`](AGENTS.md) — reglas de trabajo para agentes.

## Licencia

MIT para el código. CC BY para los datos de la DGT.
