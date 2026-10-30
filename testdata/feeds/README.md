# testdata/feeds — instantáneas reales de los feeds de la DGT

**Origen de los datos:** DGT, Punto de Acceso Nacional de tráfico y movilidad
(<https://nap.dgt.es/>), licencia **CC BY**.

**Fecha de captura:** 2026-10-28, entre las 13:01 y las 13:17 (hora de Madrid).

**Por qué están aquí:** son datos públicos CC BY y son la única forma de que los tests del parser
corran sin red (regla de `docs/ARRANQUE.md` §9). El feed real cambia cada minuto; estas copias
congelan su forma para que un cambio de la DGT rompa un test y no la CLI en producción.

## Qué es una "instantánea" y qué es un recorte

De cada feed se descargaron **tres copias separadas ~70 s**, para poder probar transiciones
(`incident.new`, `incident.cleared`) con datos reales consecutivos, no inventados.

Los archivos completos pesan entre 0,5 MB y 7 MB. Versionarlos crudos sumaría más de 60 MB de XML
que cambia cada minuto, así que cada uno se redujo con
[`scripts/trim-fixtures.sh`](../../scripts/trim-fixtures.sh), que conserva **bloques completos**
del documento (nunca elementos partidos) y garantiza que el resultado sigue siendo XML válido y
parseable por el parser real.

| Feed | Archivo original (descomprimido) | Bloques en el original | Qué se conserva | Bloques en la fixture |
|---|---|---|---|---|
| `situations` | 7,17 MB | 1002 situaciones / 1614 registros | 2 situaciones por cada `causeType`, cubriendo los 10 tipos | 18 |
| `vms-locations` | 4,56 MB | 2517 dispositivos | los 5 primeros dispositivos | 5 |
| `vms-messages` | 3,51 MB | 2473 controladores | los 3 primeros controladores | 3 |
| `radars` | 2,05 MB | 737 tramos | 1 tramo por cada valor de `singularity` (12 tipos de vía) | 12 |
| `invive` | 4,38 MB | 1331 tramos | los 3 primeros tramos | 3 |
| `tefiva` | 0,73 MB | 150 tramos | los 3 primeros tramos | 3 |

Total versionado: **628 KB**.

Cada archivo lleva un comentario XML con la atribución justo después de la cabecera del payload.

## Reproducir el recorte

```bash
# situations: 2 por tipo de causa, para cubrir todos los tipos DATEX II
scripts/trim-fixtures.sh --tag sit:situation --keep 2 --group-by sit:causeType \
  feed-completo.xml testdata/feeds/situations/snap-1.xml

# el resto: los N primeros bloques
scripts/trim-fixtures.sh --tag _0:predefinedLocation --keep 1 --group-by _0:singularity \
  radares-completo.xml testdata/feeds/radars/snap-1.xml
```

Una vez exista el `Makefile` (OPS-01), `make fixtures-trim` envolverá estas invocaciones.

## Qué NO demuestran estas fixtures

- **No son la verdad sobre el presente.** Son la forma del feed en un instante concreto del
  2026-10-28. Un cambio de esquema posterior solo lo detecta el job nocturno en vivo.
- **No cubren el catálogo entero.** Hay valores de `causeType`, de `singularity` o de formato de
  nombre de carretera que pueden aparecer cualquier día; ver `docs/feeds.md` §12.
- Los valores concretos (matrículas, tramos, obras) son datos reales: no se anonimizan porque son
  datos públicos de tráfico, no datos personales.

## Feed por feed

| Directorio | URL original |
|---|---|
| `situations/` | `https://nap.dgt.es/datex2/v3/dgt/SituationPublication/datex2_v37.xml` |
| `vms-locations/` | `https://nap.dgt.es/datex2/v3/dgt/DevicePublication/vms_datex2_v37.xml` |
| `vms-messages/` | `https://nap.dgt.es/datex2/v3/dgt/VmsPublication/datex2_v37.xml` |
| `radars/` | `https://nap.dgt.es/datex2/dgt/PredefinedLocationsPublication/radares/content.xml` |
| `invive/` | `https://nap.dgt.es/datex2/dgt/PredefinedLocationsPublication/tramos_invive/content.xml` |
| `tefiva/` | `https://nap.dgt.es/datex2/dgt/PredefinedLocationsPublication/tefiva/content.xml` |

En `docs/feeds.md` está el detalle de estructura y campos de cada uno.
