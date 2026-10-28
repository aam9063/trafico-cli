# Feeds de la DGT — resultado de SPIKE-01

- **Fecha de la medición**: 2026-10-28, 13:01–13:17 (Europe/Madrid)
- **Tarea**: SPIKE-01 (`docs/ARRANQUE.md` §14)
- **Método**: tres descargas por feed separadas ~70 s con `Accept-Encoding: gzip`, cabeceras
  registradas, sin token ni registro. Los recortes están en `testdata/feeds/`.
- **Licencia**: CC BY en todos los feeds de la organización DGT (ver ADR 0001)

Este documento manda sobre `docs/ARRANQUE.md` §2 y §4.2: donde la realidad del feed contradice la
predicción del documento, manda lo medido, y la contradicción queda anotada en §13.

---

## 1. Resumen

| # | Feed | URL | Dialecto | Bloques | Tamaño (gz / plano) | Cambia cada |
|---|---|---|---|---|---|---|
| 1 | Incidencias | `datex2/v3/dgt/SituationPublication/datex2_v37.xml` | DATEX II **v3.7** | 1002 situaciones / 1614 registros | 224 KB / 7,17 MB | **60 s exactos** |
| 2 | Paneles · localizaciones | `datex2/v3/dgt/DevicePublication/vms_datex2_v37.xml` | DATEX II **v3** (`DevicePublication`) | 2517 dispositivos | 108 KB / 4,56 MB | cuasi estático (>55 min) |
| 3 | Paneles · mensajes | `datex2/v3/dgt/VmsPublication/datex2_v37.xml` | DATEX II **v3.7** (`VmsPublication`) | 2473 controladores | 68 KB / 3,51 MB | 1–2 min |
| 4 | Radares fijos | `datex2/dgt/PredefinedLocationsPublication/radares/content.xml` | DATEX II **v1.0** | 737 tramos | 62 KB / 2,05 MB | estático en la ventana medida |
| 5 | Tramos INVIVE | `datex2/dgt/PredefinedLocationsPublication/tramos_invive/content.xml` | DATEX II **v1.0** | 1331 tramos | 99 KB / 4,38 MB | estático |
| 6 | SPFRIA (fauna) | `datex2/dgt/PredefinedLocationsPublication/tefiva/content.xml` | DATEX II **v3** | 150 tramos | 17 KB / 0,73 MB | estático |
| — | Tramos riesgo motos | `datex2/dgt/PredefinedLocationsPublication/tramosriesgomotos/content.xml` | DATEX II **v3** | 97 tramos | 12 KB / 0,47 MB | estático, fuera de alcance |

**Ningún feed requiere registro ni token.** Todos son `GET` sin parámetros.

### 1.1 La sorpresa de arquitectura: tres dialectos, no uno

El documento de arranque asume un único formato DATEX II. La realidad son **tres dialectos
incompatibles** que conviven en el mismo portal:

| Dialecto | Raíz | Feeds | Implicación |
|---|---|---|---|
| DATEX II **v3.7** (perfil DGT RTTI) | `<d2:payload xsi:type="sit:SituationPublication">` con `xmlns:com/nl=levelC/schema/3` | incidencias, paneles-mensajes | `encoding/xml` con namespace `levelC/schema/3` |
| DATEX II **v3** (esquema estándar) | `<d2:payload xsi:type="loc:PredefinedLocationsPublication">` con `xmlns:loc=datex2.eu/schema/3` | tefiva, motos, paneles-localizaciones | namespace distinto (`datex2.eu/schema/3`, no `levelC`) |
| DATEX II **v1.0** | `<d2LogicalModel modelBaseVersion="1.0">`, namespace por defecto `http://datex2.eu/schema/1_0/1_0` | radares, INVIVE | estructura totalmente distinta: `predefinedLocationSet`, `referencePoint` |

Consecuencia para `internal/datex2`: **no puede haber un único decoder**. Hacen falta tres modelos
de parseo en el mismo paquete (el paquete sigue siendo el único que conoce el XML, como pide
§8.2, pero internamente son tres archivos). Esto es una decisión de FEED-03/04/05, no de FEED-02
(FEED-02 solo necesita v3.7 de situaciones).

---

## 2. Cabeceras HTTP y caché

Medido en los 18 `GET`.

| Cabecera | Realidad | Consecuencia |
|---|---|---|
| `Content-Type` | `text/xml; charset=utf-8` | Decodificar como UTF-8; hay acentos y `ñ` en `municipality` y `roadName` |
| `Content-Encoding` | `gzip` si se pide `Accept-Encoding: gzip`; `Vary: Accept-Encoding` | **Usar gzip siempre**: 7,17 MB → 224 KB (32×) |
| `ETag` | **ausente en los 6 feeds** | No hay validación por ETag |
| `Last-Modified` | **ausente en los 6 feeds** | **No hay cabeceras condicionales**. FEED-01 no debe implementar `If-None-Match` ni `If-Modified-Since`: no servirían de nada |
| `Cache-Control` | `max-age=4` (mensajes de paneles), `32` (incidencias), `60` (radares, INVIVE, motos), `3327` (localizaciones de paneles) | Es una sugerencia del CDN, no un contrato. Nuestra caché de 60 s (P-06) es independiente y no debe depender de este valor |
| `Connection` | sin `keep-alive` explícito; hay `Alt-Svc: h3` | Irrelevante: una descarga por minuto |

La frescura se calcula con `<com:publicationTime>` del propio XML, no con `Date` ni con
`Cache-Control`.

---

## 3. Frecuencia de publicación (medida)

| Feed | `publicationTime` de las 3 instantáneas | Frecuencia |
|---|---|---|
| Incidencias | `13:12:17.098` → `13:13:17.099` → `13:14:17.099` | **60,0 s**, exacta |
| Paneles · mensajes | `13:04:37` → `13:06:40` → `13:07:41` | 1–2 min, irregular |
| Paneles · localizaciones | `12:05:01` en las tres | >55 min (cuasi estático) |
| Radares, INVIVE, TEFIVA, motos | sin `publicationTime` utilizable; contenido idéntico en las tres | estático |

El número de incidencias creció 1002 → 1004 → 1009 en tres minutos: la caché de 60 s es la
duración correcta, ni más ni menos.

---

## 4. Feed 1 — Incidencias (`sit:SituationPublication`, v3.7)

`profileName="DGT (Spain) Profile RTTI SituationPublication"`, `profileVersion="3.7_1.0"`,
`lang="es"`.

### 4.1 Jerarquía

```
d2:payload
├── com:publicationTime                    ← frescura (única fuente válida)
├── com:publicationCreator/com:nationalIdentifier = "DGT"
└── sit:situation id="2816645"             ← 1 situación = N registros
    ├── sit:overallSeverity                ← opcional
    ├── sit:headerInformation/com:informationStatus = "real"
    └── sit:situationRecord xsi:type="sit:…" id="18811074" version="2"
        ├── sit:situationRecordCreationTime
        ├── sit:situationRecordVersionTime  ← `updated_at`
        ├── sit:probabilityOfOccurrence     ← siempre "certain"
        ├── sit:severity                    ← opcional
        ├── sit:source/com:sourceIdentification = "DGT"
        ├── sit:validity
        │   ├── com:validityStatus          ← siempre "active" en lo medido
        │   └── com:validityTimeSpecification
        │       ├── com:overallStartTime
        │       └── com:overallEndTime      ← opcional (64,6 % lo trae)
        ├── sit:cause
        │   ├── sit:causeType
        │   └── sit:detailedCauseType/<sit:xxxType>
        ├── sit:locationReference xsi:type="loc:SingleRoadLinearLocation" | "loc:PointLocation"
        │   ├── loc:supplementaryPositionalDescription
        │   │   ├── loc:carriageway/loc:carriageway + loc:lane/loc:laneUsage
        │   │   └── loc:roadInformation/loc:roadName
        │   └── loc:tpegLinearLocation (o loc:tpegPointLocation)
        │       ├── loc:tpegDirection                ← rumbo cardinal
        │       ├── loc:to / loc:from  (xsi:type="loc:TpegNonJunctionPoint")
        │       │   ├── loc:pointCoordinates/loc:latitude + loc:longitude
        │       │   └── loc:extendedTpegNonJunctionPoint
        │       │       ├── lse:kilometerPoint
        │       │       ├── lse:province
        │       │       ├── lse:municipality
        │       │       └── lse:autonomousCommunity
        │       └── loc:extendedTpegLinearLocation/lse:tpegDirectionRoad
        └── <campo propio del tipo>
```

### 4.2 Campos que SÍ existen

| Concepto del modelo (§4.1) | Campo real |
|---|---|
| `road` | `loc:roadInformation/loc:roadName` — presente en **1614/1614** registros |
| `pk.from` / `pk.to` | `lse:kilometerPoint` en el punto `from` y en el `to`. Presente en **100 %** de los puntos (2759 puntos, 1614 registros) |
| `location.start` / `end` | `loc:pointCoordinates` de `from`/`to`, siempre presente |
| `province` | `lse:province`, siempre presente |
| `municipality` | `lse:municipality`, siempre presente |
| `direction` | `lse:tpegDirectionRoad`: `positive` (853), `negative` (683), `both` (78) |
| `severity` | `sit:severity` (122 registros) o `sit:overallSeverity` (89 situaciones). **13 % del total** |
| `start_time` / `end_time` | `com:overallStartTime` (100 %) / `com:overallEndTime` (64,6 %) |
| `updated_at` | `sit:situationRecordVersionTime` |
| `subtype_raw` | `sit:causeType` + el hijo de `sit:detailedCauseType` |
| `lanes_affected` (parcial) | `loc:laneUsage`: `rightLane`, `leftLane`, `allLanesCompleteCarriageway`, `middleLane`, `tidalFlowLane`, `centralReservation`, `turningLane`, `carPoolLane`, `_extended`. La lista **no es cerrada**: los valores se conservan tal cual llegan, sin filtrar los que no estén en el catálogo DATEX II |

### 4.3 Campos que NO existen (no inventar)

| Campo del modelo | Realidad |
|---|---|
| `direction_label` ("Valencia") | **No viene.** `loc:tpegDirection` es un rumbo cardinal (`westBound`, `northEastBound`). En los feeds de paneles sí aparece `loc:roadDestination` ("MADRID"), pero **no en incidencias**. En v1: `direction_label` = `null` o el rumbo cardinal, nunca un destino inventado |
| `lanes_affected` como "1 de 3" | **No hay número total de carriles.** Solo se sabe qué carriles están afectados. Modelar como lista de `laneUsage`, no como string "1 de 3" |
| `raw_description` (texto libre de la DGT) | **No existe** en el 99,9 % de los registros: `com:value` aparece 2 veces y es la `feedDescription`. `raw_description` será `null` en la práctica; el "texto original" que hay que preservar es el par `causeType` + `detailedCauseType` |
| `type` como lista cerrada | El feed usa 10 `causeType` y 13 elementos de detalle (ver `internal/datex2/mapping.yaml`) |
| IDs | Hay dos niveles: `sit:situation/@id` (1002) y `sit:situationRecord/@id` (1614). El modelo necesita decidir cuál es `id` (recomendación: el registro, que es la unidad con ubicación y causa, y conservar el de la situación en `situation_id`) |

### 4.4 Dirección: dos conceptos distintos que no hay que confundir

- `lse:tpegDirectionRoad` ∈ {`positive`, `negative`, `both`} → **es la dirección de circulación**.
  Es lo que el modelo llama `direction`.
- `loc:tpegDirection` ∈ {`westBound`, `northEastBound`, …} → **es el rumbo geográfico**.
  No es una etiqueta de destino.

### 4.5 Severidad

| Valor en el feed | Ocurrencias | Normalizado |
|---|---|---|
| `highest` | 173 | `high` |
| `medium` | 36 | `medium` |
| `low` | 2 | `low` |
| `high` | 2 | `high` |
| (ausente) | ~1400 | `unknown` |

Precedencia: `sit:severity` del registro, luego `sit:overallSeverity` de la situación, luego
`unknown`. **No se infiere de `causeType`** (P-04).

Consecuencia práctica: `--severity high` y `--min-severity medium` filtrarán una minoría y
descartarán el 87 % de las incidencias por no tener severidad. La skill (§6.4) debe decir que
`severity: "unknown"` significa "la DGT no lo publica", **no** "severidad baja". El contrato de
§4.1 fija `unknown` como valor del enum, así que ese caso viaja como cadena explícita y no como
`null`: el agente necesita distinguir "no publicado" de "campo que no existe en el modelo".

### 4.6 Cobertura geográfica: el matiz que rompe el mensaje simple

Provincias presentes en el feed: 46. Ausentes: **Lleida, Araba/Álava, Bizkaia, Gipuzkoa, Ceuta,
Melilla**.

Pero **sí aparecen Barcelona (8), Girona (2) y Tarragona (1)**: la DGT publica incidencias de
carreteras de su competencia que cruzan Cataluña, aunque no las que gestiona la SCT.

Consecuencia para `coverage_note` (§5.3): el texto "Sin datos de Cataluña ni País Vasco" es
**impreciso**. Debe decir algo equivalente a "Cobertura de la DGT: sin las incidencias gestionadas
por la SCT (Cataluña) ni por el Gobierno Vasco", porque puede haber filas de esas provincias.

También hay provincias bilingües con barra: `Alacant/Alicante`, `Castelló/Castellón`,
`València/Valencia`, `A Coruña`, `Illes Balears`, `La Rioja`, `Ourense`. El filtro `--province`
tiene que normalizar (minúsculas, sin acentos, y aceptar cualquiera de los dos nombres del par).

### 4.7 Nombres de carretera: la normalización es más difícil de lo previsto

De 564 nombres únicos, hay formatos que rompen la regla "mayúsculas + guion":

```
A-7            AP-7           N-332a         CV-80          (los previstos)
A-356R1        A-4000R1       A-7052R2       (ramales con sufijo)
CG-1.5                        Ma-10          Ma-1032        (provinciales con minúscula y punto)
LU-P-1611      CU-V-5006      EX-A1          DSA-291        (prefijos de varias letras)
GC-1 AM        CHG-1          CV privado Iberdrola          (sufijo de texto, texto libre)
Avda. Andalucía                                              (vía urbana, sin guion, con espacio final)
```

La regla de MODEL-01 ("`a7`, `A 7` y `A-7` son `A-7`") sigue siendo válida para lo que es una
carretera con número, pero el normalizador **no puede descartar** los demás formatos: tiene que
conservarlos tal cual llegan, en mayúsculas y con espacios colapsados, sin inventar guiones donde
no los hay. `--road` debe comparar sobre esa forma normalizada y aceptar la variante con espacio o
con minúsculas.

### 4.8 Localización: lineal y puntual

- `loc:SingleRoadLinearLocation`: 1145 registros, con `from` y `to`.
- `loc:PointLocation`: 469 registros, con un único punto.

Ambos usan la misma extensión (`extendedTpegNonJunctionPoint`) para PK, provincia y municipio. El
modelo necesita `pk` con `from` y `to` opcionales, no ambos obligatorios.

---

## 5. Feeds 2 y 3 — Paneles de mensaje variable

Son **dos feeds que hay que cruzar por id**, no uno.

### 5.1 `DevicePublication/vms_datex2_v37.xml` — dónde está cada panel

`xsi:type="ns2:DevicePublication"`, `profileName="ProfileDGT"`, pero con namespaces
`levelC/schema/3`.

```
ns2:device xsi:type="fse:ExtendedDevice" id="167938" version="3"
├── ns2:typeOfDevice = "vms"                    ← 2517/2517 son vms
├── ns2:lastUpdateOfDeviceInformation
└── ns2:pointLocation
    ├── loc:supplementaryPositionalDescription
    │   ├── loc:roadInformation/loc:roadName            ← "A-54"
    │   └── loc:roadInformation/loc:roadDestination     ← "LUGO" (1545/2517)
    └── loc:tpegPointLocation
        ├── loc:pointCoordinates
        ├── lse:kilometerPoint                          ← 0.65
        ├── lse:province                                ← "LUGO" en MAYÚSCULAS
        └── lse:tpegDirectionRoad                       ← positive/negative/both
```

Observaciones:

- `loc:roadDestination` **sí existe aquí** y es el equivalente real de `direction_label`.
- `lse:province` viene en **mayúsculas** (`LUGO`, `MADRID`), a diferencia del feed de incidencias
  (`Lugo`). La normalización de provincia tiene que ser insensible a mayúsculas.
- El feed es cuasi estático: los 3 snapshots son idénticos y `publicationTime` no cambió en 55 min.

### 5.2 `VmsPublication/datex2_v37.xml` — qué muestra cada panel

`profileName="DGT (Spain) Profile VMSPublication"` 3.7_1.0, namespaces `levelC/schema/3`.

```
vms:vmsControllerStatus
├── vms:vmsControllerReference id="167938"       ← cruce con ns2:device/@id
├── vms:vmsStatus vmsIndex="1"
│   └── vms:vmsStatus
│       └── vms:vmsMessage messageIndex="1"
│           └── vms:vmsMessage
│               ├── vms:timeLastSet               ← message_updated_at
│               └── vms:displayAreaSettings displayAreaIndex="1"
│                   ├── (xsi:type="vms:PictogramDisplay")
│                   │   ├── vms:pictogramDisplayUrl
│                   │   └── vms:pictogram/vms:customPictogramCode
│                   └── (xsi:type="vms:TextDisplay")
│                       └── vms:textLine          ← "VELOCIDAD", "VIGILANCIA", …
```

Observaciones:

- El **mensaje** es la concatenación de `vms:textLine` y las URLs de pictograma.
- 3855 líneas de texto y 2032 pictogramas para 2473 controladores.
- Un panel puede estar "apagado": `textLine` vacía o solo espacios, pictograma `0.png`.
- El cruce es `vmsControllerReference/@id` ↔ `ns2:device/@id`, con el **mismo identificador**
  (`167938`). No hace falta heurística.
- `vmsIndex` es el índice del panel dentro del controlador (1 en lo medido).

---

## 6. Feed 4 — Radares fijos (`PredefinedLocationsPublication/radares`, DATEX II v1.0)

737 bloques, de los cuales:

| Prefijo del id | Cantidad | Qué es |
|---|---|---|
| `GUID_CABINACINEMOMETRO…` | 690 | cabina de cinemómetro (radar fijo clásico) |
| `GUID_CVM…` | 47 | cinemómetro de velocidad media (radar de tramo) |

Estructura (v1.0): `predefinedLocation` → `predefinedLocation` (`xsi:type="_0:Linear"`) →
`tpeglinearLocation` + `referencePointLinear` → `referencePointPrimaryLocation` y
`referencePointSecondaryLocation`, cada uno con:

```
_0:referencePoint
├── _0:referencePointIdentifier     ← "CVM_161274_ini" / "…_fin"
├── _0:administrativeArea/_0:value  ← "ZARAGOZA"
├── _0:roadName/_0:value            ← "Z-40"
├── _0:roadNumber                   ← "Z-40"
├── _0:directionRelative            ← positive / negative
├── _0:referencePointDistance       ← 26600.0  (metros)
└── _0:referencePointExtension/_0:ExtendedReferencePoint
    ├── _0:directionNamed           ← "MADRID"   ← esto es direction_label
    ├── _0:provinceINEIdentifier    ← "50"       ← provincia por código INE, no por nombre
    ├── _0:roadIdentifier
    ├── _0:segmentIdentifier
    └── _0:singularity              ← "AUTOPISTA / AUTOVÍA", "CARRETERA NACIONAL", "TÚNEL", …
```

Valores de `singularity`: `AUTOPISTA / AUTOVÍA` (298), `CARRETERA NACIONAL` (127), `CARRETERA
AUTONÓMICA` (82), `CARRETERA` (81), `TÚNEL` (15), `CIRCUNVALACIÓN` (7), `CARRETERA PROVINCIAL`
(5), `CARRETERA LOCAL` (4), `DESCONOCIDA` (4), `MEDIANA` (1), `TRAVESIA` (1), `NINGUNA` (159).

**Lo que NO viene:**

- `speed_limit` — el feed no publica el límite de velocidad. `speed_limit` será `null` salvo que
  la DGT lo añada.
- `type` como campo — solo se distingue por el prefijo del id (`CABINACINEMOMETRO` / `CVM`), que
  es un identificador, no un campo tipado. Exponer `type: null` y conservar el id es lo fiel;
  derivar el tipo del prefijo es una decisión a documentar en FEED-04.
- PK por nombre: llega como `referencePointDistance` **en metros** (26600.0 → PK 26,6), no como
  `kilometerPoint`.
- Provincia: **código INE** (`50`), no nombre. Hace falta una tabla INE → nombre para
  `--province`, y decidir qué hacer si no se quiere mantener esa tabla (opción: `province: null`
  y provincia solo en los feeds que la dan por nombre).
- El radar es un **tramo** (`ini`–`fin`), no un punto.

---

## 7. Feed 5 — Tramos INVIVE (`tramos_invive`, DATEX II v1.0)

1331 tramos, mismo dialecto v1.0 que radares:

```
_0:predefinedLocationSet id="GUID_Inventario_Tramos_Invive"
└── _0:predefinedLocation id="GUID_Tramo_Invive_1"
    └── _0:predefinedLocation xsi:type="_0:Linear"
        └── _0:tpeglinearLocation
            ├── _0:tpegDirection          ← bothWays
            ├── _0:to / _0:from (TPEGNonJunctionPoint)
            │   ├── _0:pointCoordinates
            │   └── _0:name/_0:descriptor/_0:value   ← "CM-220", "ALBACETE"
            └── _0:referencePointLinear
                └── …/_0:roadNumber, _0:directionRelative,
                       _0:referencePointDistance     ← 2662 valores (from + to)
```

- 2662 `referencePointDistance` = 1331 × 2 → **todos los tramos tienen PK de inicio y fin**, en
  metros.
- Sin `lse:kilometerPoint`, sin `lse:province`: el nombre de la carretera y la provincia viajan en
  `_0:name/_0:descriptor/_0:value` con `tpegDescriptorType` (`linkName`, `townName`, …).

---

## 8. Feed 6 — SPFRIA / fauna (`tefiva`, DATEX II v3)

150 tramos. El id del recurso es `tefiva`, pero la ficha se llama **SPFRIA**: es el mismo dataset.

```
loc:predefinedLocationReference id="HUELVA_N-433_71400.0" version="20250721"
└── loc:location xsi:type="loc:SingleRoadLinearLocation"
    ├── loc:supplementaryPositionalDescription/loc:lengthAffected  ← 1.7 (km)
    └── loc:tpegLinearLocation
        ├── loc:tpegDirection = bothWays
        └── loc:to / loc:from (xsi:type="loc:TpegJunction")
            ├── loc:pointCoordinates
            ├── loc:ilc/loc:descriptor/com:values/com:value        ← "N-433"
            └── loc:otherName/…/com:value                          ← "HUELVA" (administrativeAreaName)
```

- El **id codifica** provincia, carretera y PK en metros:
  `PROVINCIA_CARRETERA_PK_EN_METROS` → `HUELVA_N-433_71400.0` = PK 71,4.
- `pk.from`/`pk.to` **no** existen como elementos; se derivarían del id y de `lengthAffected`.
  Derivar de un identificador es información de la fuente, pero es frágil: FEED-05 debe decidir y
  documentarlo (propuesta: `pk` derivado del id, con un campo `pk_source: "identifier"`).

El dataset de tramos de riesgo para **motocicletas** (`tramosriesgomotos`) comparte exactamente
esta forma (97 tramos) y no está en el backlog: no hay comando para él.

---

## 9. Atribución y frescura, con datos

- `<com:publicationTime>` existe en incidencias, paneles-mensajes y paneles-localizaciones. En los
  feeds v1.0 (radares, INVIVE) y en tefiva/motos **no hay `publicationTime`**: para esos feeds
  `published_at` será `null` y la frescura se mide por la hora de descarga. Esto afecta a
  `meta.published_at` y a `stale` (P-08) y hay que decidirlo en FEED-03/04/05, no inventar un
  valor.

---

## 10. Impacto en la caché y en el tamaño

- El feed de incidencias son **7,17 MB** de XML cada 60 s. Leerlo entero en memoria por comando es
  aceptable una vez por minuto, pero no dos veces: la caché compartida de 60 s (P-06) no es una
  optimización, es un requisito. Con gzip son 224 KB de red.
- `encoding/xml` en streaming es necesario: 1614 registros con ~4,4 KB de media.
- El benchmark de FEED-02 debe medir sobre las fixtures y sobre una descarga completa real
  (`make bench`) para que el número signifique algo.

---

## 11. Fixtures

Detalle en `testdata/feeds/README.md`. Resumen: 6 feeds × 3 instantáneas, recortadas con
`scripts/trim-fixtures.sh` a bloques completos, 628 KB en total, todas XML válido, con la
atribución embebida como comentario.

---

## 12. Lo que NO se pudo confirmar

| Pregunta | Estado |
|---|---|
| ¿Hay más `causeType` que los 10 observados? | Probablemente sí, con el tiempo. El mapping tiene `default_type: other` y `subtype_raw` conserva el valor |
| ¿Hay `validityStatus` distinto de `active`? | No en las 3 instantáneas. `status` se calcula por fechas, no por este campo |
| ¿Desde cuándo existe `tefiva` con este nombre? | Sin dato; la ficha se llama SPFRIA y el recurso `tefiva` |
| ¿Cuántos radares fijos reales hay? | 690 cabinas + 47 tramos. No se puede distinguir "radar fijo" de "radar de tramo" sin el prefijo del id |
| ¿Sirve algún feed para las provincias vascas o catalanas? | No: SCT no declara licencia (ADR 0001) y DT-GV queda fuera de la v1 |
| ¿El portal publica el perfil DATEX II en XSD descargable? | Los `xsi:schemaLocation` apuntan a XSD por URL, pero no se validaron contra ellos en esta tarea |

---

## 13. Dónde `ARRANQUE.md` decía otra cosa

| `ARRANQUE.md` | Realidad medida | Qué se hace |
|---|---|---|
| §2: "Otros datasets… URLs y licencia de cada uno a confirmar" | Confirmadas 7 URLs y CC BY en todas las de la DGT | ADR 0001 + este documento |
| §2.1 y §4.1: `type` desde una tabla de mapeo | Cierto, pero el detalle está en 10 `causeType` × 13 elementos | `internal/datex2/mapping.yaml` |
| §4.1: `severity` la publica la DGT | La publica en el **13 %** de los registros | §4.5; la skill debe explicar `unknown` |
| §4.1: `lanes_affected: "1 de 3"` | No hay total de carriles | §4.3: lista de `laneUsage` |
| §4.1: `direction_label: "Valencia"` | No existe en incidencias; sí como `roadDestination` en paneles | §4.3 y §5.1 |
| §4.1: `raw_description` con el texto original | Casi nunca hay texto libre | §4.3: se conserva `causeType`/`detail` |
| §4.1: `road` "mayúsculas y guion" | 564 formatos distintos, con ramales, vías urbanas y texto libre | §4.7 |
| §5.3: `coverage_note` "Sin datos de Cataluña ni País Vasco" | La DGT sí publica algunas incidencias en Barcelona, Girona y Tarragona | §4.6 |
| §7: `[feeds.dgt-signs]` y `[feeds.dgt-radars]` con `url = ""` | URLs reales confirmadas | Config por defecto de FEED-01 |
| §8.2: un único parser DATEX II | Tres dialectos incompatibles | §1.1 |
| P-06 / FEED-01: "cabeceras condicionales si el servidor las soporta" | No hay `ETag` ni `Last-Modified` | §2: no implementarlas |
