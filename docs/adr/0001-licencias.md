# ADR 0001 — Licencias de los datos de la DGT

- **Estado**: aceptado
- **Fecha**: 2026-10-28
- **Tarea**: H-01
- **Consulta de las fuentes**: 2026-10-28, fichas de dataset del Punto de Acceso Nacional
  (`nap.dgt.es`) y aviso legal de `dgt.es`

## Contexto

`trafico-cli` redistribuye y transforma datos publicados por la DGT. Antes de escribir una línea
de código hay que saber, dataset por dataset, si se pueden usar y con qué obligaciones. El
backlog solo autoriza los datasets cuya licencia esté confirmada (H-01), y deja Cataluña (SCT) y
País Vasco (DT-GV) fuera de la v1 hasta que esta tarea los resuelva.

Cada ficha de dataset del NAP declara su licencia en el campo de metadatos `dc:rights`, con un
enlace al identificador de licencia de `opendefinition.org`. Eso es la declaración específica de
licencia de los datos y prevalece sobre el aviso legal genérico del portal `dgt.es`, que habla de
los derechos de autor del sitio web (diseño, código, marcas), no de la licencia de reutilización
de los conjuntos de datos abiertos.

## Decisión

Todos los datasets de la organización **DGT** que necesitamos están publicados bajo
**Creative Commons Attribution (CC BY)** y son utilizables. Los de **SCT** (Cataluña) no declaran
licencia y quedan excluidos. Los de **DT-GV** (País Vasco) declaran CC BY y son utilizables, pero
no se integran en la v1.

| Dataset | Organización | Recurso | Licencia declarada | Uso |
|---|---|---|---|---|
| Incidencias DGT DATEX2 v3.7 | DGT | `datex2/v3/dgt/SituationPublication/datex2_v37.xml` | CC BY | **v0.1** (núcleo) |
| Paneles DGT · localizaciones | DGT | `datex2/v3/dgt/DevicePublication/vms_datex2_v37.xml` | CC BY | **v0.2** |
| Paneles DGT · tiempo real | DGT | `datex2/v3/dgt/VmsPublication/datex2_v37.xml` | CC BY | **v0.2** |
| Radares fijos DGT | DGT | `datex2/dgt/PredefinedLocationsPublication/radares/content.xml` | CC BY | **v0.2** |
| Tramos INVIVE | DGT | `datex2/dgt/PredefinedLocationsPublication/tramos_invive/content.xml` | CC BY | **v0.2** |
| SPFRIA (fauna) | DGT | `datex2/dgt/PredefinedLocationsPublication/tefiva/content.xml` | CC BY | **v0.2** |
| Tramos de elevado riesgo para motocicletas | DGT | `datex2/dgt/PredefinedLocationsPublication/tramosriesgomotos/content.xml` | CC BY | fuera de alcance (sin comando) |
| Cámaras DGT DATEX2 v3.7 | DGT | `datex2/v3/dgt/DevicePublication/camaras_datex2_v37.xml` | CC BY | fuera de alcance (sin comando) |
| Incidencias SCT (Cataluña) | SCT | `datex2/sct/SituationPublication/all/content.xml` | **no especificada** | **excluido** |
| Incidencias DT-GV (País Vasco) | DT-GV | `datex2/dt-gv/SituationPublication/all/content.xml` | CC BY | licencia OK, **no en v1** |

Consecuencias directas en el backlog:

- **FEED-03, FEED-04, FEED-05** (paneles, radares, tramos) quedan desbloqueados: los tres
  datasets son CC BY.
- **FEED-06** (SCT + DT-GV) **no se puede completar**: su criterio es "H-01 positivo para ambos" y
  SCT es negativo. Sigue fuera de la v1. Si en el futuro la SCT publica una licencia, se revisa
  este ADR.
- `coverage_note` ("Sin datos de Cataluña ni País Vasco") **se mantiene** en la v1, porque aunque
  DT-GV tenga licencia usable, no se integra todavía.

## Obligaciones que impone CC BY

1. **Atribuir** la fuente en toda salida y en toda redistribución. Implementado como el bloque
   `attribution` de cada respuesta JSON (P-05) y como la línea "Fuente: DGT (CC BY)" de `pretty`.
2. **Enlazar la licencia**, no solo nombrarla: el bloque `attribution` incluye la URL del NAP y el
   README explica la licencia.
3. **Indicar si hubo cambios**: normalizamos, filtramos y agregamos los datos. El README y
   `docs/ARRANQUE.md` deben decir explícitamente que la salida es una transformación y no el
   documento original de la DGT.
4. **No insinuar endoso**: nada en el README, en la ayuda de la CLI ni en la salida puede
   sugerir que la DGT avala, patrocina o mantiene esta herramienta. De ahí el aviso de "fuente no
   oficial".
5. La CC BY **no impone compartir igual**: el código puede ir con MIT y los datos conservan su
   licencia. `LICENSE` lo separa explícitamente.

## Alternativas descartadas

- **Usar SCT sin licencia declarada.** Descartado: sin licencia no hay cesión de derechos, y el
  aviso legal del portal no la sustituye.
- **Interpretar el aviso legal de `dgt.es` como restrictivo para todo.** Descartado: las fichas
  del NAP son la declaración específica de licencia de los datos abiertos y son las que la propia
  DGT enlaza desde cada recurso.
- **Integrar DT-GV ya, porque su licencia es válida.** Pospuesto: la v1 se define sin País Vasco
  y abrir ese frente cambia `coverage_note`, el modelo de `province` y los tests. Va a un PR
  aparte, no a este.

## Verificación

- Cada fila de la tabla se comprobó leyendo el campo `dc:rights` de la ficha del dataset en
  `nap.dgt.es` el 2026-10-28, y su enlace a `opendefinition.org/licenses/cc-by`.
- Los recursos listados se descargaron en SPIKE-01 con `HTTP 200` (ver `docs/feeds.md`); ninguno
  requiere token ni registro.
- SCT: la ficha responde literalmente `No se especificó la licencia`.
