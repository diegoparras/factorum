# API de Factorum

Base: `http://factorum:8095` (puerto por defecto `FACTORUM_PUERTO=8095`).

Autenticación: `X-API-Key: <clave>` o `Authorization: Bearer <clave>`.

- `FACTORUM_API_KEY`: clave con todos los permisos (operador).
- `FACTORUM_API_KEY_LECTURA` (opcional): solo `GET` y `POST /api/verificar`.

Sin clave, o con una clave inválida, la respuesta es `401`. `/healthz` no pide clave.

Todas las respuestas son JSON con `ok: true|false`. Ante un error, la respuesta lleva `error` (un mensaje en castellano) y el código HTTP que corresponde.

Los importes van como números con dos decimales (`121000.00`) y las fechas como `AAAA-MM-DD`.

## Salud

`GET /healthz` devuelve:

```json
{ "ok": true, "version": "0.1.0", "base": true, "ocr": true, "ia": { "habilitada": true, "modelo": "qwen2.5vl:7b", "local": true }, "arcanum": true, "almacen": "r2" }
```

## Verificar una factura (¿es legal?)

`POST /api/verificar`

El archivo va de una de estas dos formas:

- **Multipart:** campo `archivo`.
- **JSON:** `{ "nombre": "f.pdf", "contenido_base64": "..." }`.

Opciones (como campos del form o del JSON):

| Opción | Para qué |
|---|---|
| `cuit_consultante` | CUIT con certificado en Arcanum. Si no viene, se usa `ARCANUM_CUIT_CONSULTANTE`. |
| `ia` | `true` o `false`. |
| `guardar` | `true` o `false` (por defecto `false`: verificar no deja nada guardado). |

Lee la factura, corre los controles y, si Arcanum está configurado, la constata con WSCDC y busca al emisor en la base de apócrifos. Responde:

```json
{
  "ok": true,
  "veredicto": "valida" | "invalida" | "apocrifa" | "tique_controlado" | "no_verificable" | "con_errores",
  "mensaje": "ARCA confirma que el comprobante existe con esos datos.",
  "resultado": { /* modelo.Resultado: archivo, sha256, paginas, comprobantes[], extractores, duracion_ms */ },
  "constatacion": [ { "valido": true, "resultado": "A", "observaciones": [], "apoc": { "consultado": true, "esApocrifo": false } } ]
}
```

Cómo se decide el veredicto:

| Veredicto | Cuándo |
|---|---|
| `apocrifa` | El emisor está en la base de apócrifos. |
| `invalida` | ARCA rechaza la constatación (`resultado` = `R`). |
| `valida` | ARCA la aprueba (`A`) y no hay errores locales. |
| `tique_controlado` | Un ticket de controlador fiscal sin errores locales: el CUIT es válido y el emisor no figura como apócrifo. |
| `con_errores` | Hay errores locales (por ejemplo, QR distinto del texto) aunque ARCA la apruebe. |
| `no_verificable` | No hay Arcanum, faltan datos clave (CAE, número...) o ARCA no respondió. |

Si hay varios comprobantes en el archivo, el veredicto general es el más grave, en este orden: `valida`, `tique_controlado`, `no_verificable`, `con_errores`, `invalida`, `apocrifa`.

### Tickets de compra

Los tiques y tiques factura de controlador fiscal (supermercados, estaciones de servicio, farmacias, bares) **no se constatan en ARCA**: WSCDC no los cubre. Para un ticket, Factorum consulta solo la base de apócrifos (`GET /api/apoc/:cuit` de Arcanum) y guarda el resultado con la misma forma que la constatación, con `valido: null`:

```json
{ "valido": null, "resultado": null, "observaciones": [], "apoc": { "consultado": true, "esApocrifo": false, "fechaCondicion": null }, "fecha": "..." }
```

Veredicto de un ticket:

| Veredicto | Cuándo |
|---|---|
| `apocrifa` | El emisor figura en la base de apócrifos. |
| `con_errores` | Hay errores locales (CUIT inválido, sumas que no cierran, falta el total...). |
| `tique_controlado` | "Ticket controlado: el CUIT es válido y el emisor no figura como apócrifo (los tiques no se constatan en ARCA)." |
| `no_verificable` | No hay Arcanum o la base de apócrifos no respondió. |

Los controles de un ticket no piden CAE y suman neto + IVA + otros solo si el ticket trae ese detalle. Siempre agregan el hallazgo informativo `tique_sin_constatacion`. Si la base de apócrifos responde que el emisor no figura, se agrega `apoc_controlado` (info).

## Lotes (procesamiento masivo)

`POST /api/lotes` (multipart)

| Campo | Contenido |
|---|---|
| `archivos` | Uno o más: PDF, JPG, PNG, WEBP o ZIP con esos archivos adentro. |
| `cliente_cuit` | Opcional: el cliente del estudio al que pertenecen. |
| `nombre` | Opcional. |
| `ia` | `true` o `false`. |
| `constatar` | `true` o `false`: constatar con Arcanum al terminar. |

La respuesta es `202 { "ok": true, "lote": Lote }`. El procesamiento corre en segundo plano.

`GET /api/lotes?limite=50&offset=0` devuelve `{ ok, lotes: Lote[], total }`.

`GET /api/lotes/{id}` devuelve `{ ok, lote: Lote }`.

```json
Lote = { "id": 12, "nombre": "Noviembre", "cliente_cuit": "20222222223", "origen": "web|telegram|whatsapp|webhook|mcp|arcanum",
         "creado": "2026-11-25T12:00:00Z", "estado": "procesando|terminado",
         "documentos": 120, "procesados": 87, "con_error": 1,
         "comprobantes": { "ok": 80, "revisar": 5, "error": 2, "aprobado": 0 } }
```

## Comprobantes

`GET /api/comprobantes` devuelve `{ ok, comprobantes: Cbte[], total }`. Filtros por query:

| Filtro | Valores |
|---|---|
| `lote` | id del lote |
| `estado` | `ok`, `revisar`, `error` o `aprobado` |
| `cliente` | CUIT del cliente |
| `emisor` | CUIT del emisor |
| `desde`, `hasta` | fecha de emisión |
| `q` | busca en razón social, CUIT y número |
| `clase` | `facturas` (todo lo que no es un ticket) o `tickets` (tiques de controlador fiscal, códigos 81, 82, 83 y del 109 al 120). Vacío trae todo |
| `rubro` | `supermercado`, `combustible`, `farmacia`, `gastronomia`, `transporte`, `servicios`, `otros`, o `sin_rubro` para los que no tienen |
| `limite` | hasta 500 (por defecto 100) |

Un valor de `clase` o `rubro` que no se entiende da `400`.
| `offset` | desde dónde |

`GET /api/comprobantes/{id}` devuelve `{ ok, comprobante: Cbte, correcciones: Correccion[] }`.

`PATCH /api/comprobantes/{id}` recibe:

```json
{ "campos": { "razon_social_emisor": "X", "iva_21": "21000.00" }, "aprobar": true, "usuario": "diego" }
```

Cada campo cambiado queda en `correcciones` (antes, después, quién y cuándo) y pasa a tener origen `manual`. Los controles se recalculan. Con `aprobar: true`, el estado pasa a `aprobado`. La respuesta es `{ ok, comprobante: Cbte }`.

Además de los campos de importe, fecha y emisor, se pueden corregir `hora` (`HH:MM` o `HH:MM:SS`) y `rubro` (uno de la lista de abajo; otro valor da `400`).

### Rubro

Cada comprobante puede tener un `rubro` de gasto: `supermercado`, `combustible`, `farmacia`, `gastronomia`, `transporte`, `servicios` u `otros`. Sale de tres capas; la de más arriba gana:

| Capa | De dónde sale | Origen (`origenes.rubro`) |
|---|---|---|
| 3 | La corrección de una persona, recordada por CUIT del emisor | `manual` |
| 2 | La actividad principal del emisor en el padrón A5 de ARCA (vía Arcanum): `actividades` con `idActividad` (código CLAE), `descripcionActividad` y `orden` (1 = principal) | `arcanum` |
| 1 | Palabras clave del nombre del comercio y del texto (YPF, SHELL o "estación de servicio" es combustible; "farmacia" es farmacia...). Solo en tickets | `texto` u `ocr` |

Si no hay con qué decidir, el rubro queda vacío: no se inventa.

Cuando una persona corrige el rubro con `PATCH`, Factorum lo guarda para ese CUIT, lo pasa a los comprobantes ya cargados de ese emisor cuyo rubro no eligió una persona y lo usa en los que vengan.

`GET /api/rubros` devuelve `{ ok, rubros: ["supermercado", ...], por_cuit: [ { cuit_emisor, rubro, usuario, actualizado } ] }`.

`POST /api/comprobantes/{id}/constatar` constata ese comprobante con Arcanum y devuelve `{ ok, constatacion }`.

```json
Cbte = { "id": 5, "lote_id": 12, "documento_id": 33, "cliente_cuit": "...", "archivo": "f.pdf",
         ...todos los campos de modelo.Comprobante (tipo, letra, punto_venta, numero, fecha, cuit_emisor, ...),
         "es_tique": false, "hora": "13:45:10", "rubro": "supermercado",
         "estado": "ok", "confianza": 1, "hallazgos": [...], "origenes": {...},
         "constatacion": null | { "valido": true, "resultado": "A", "observaciones": [], "apoc": {...}, "fecha": "..." },
         "duplicado_de": null | 4, "creado": "...", "revisado_por": null, "revisado_en": null }
```

`GET /api/documentos/{id}/archivo` devuelve el archivo original, descomprimido y descifrado.

## Exportar

`GET /api/export.xlsx` y `GET /api/export.csv` aceptan los mismos filtros que `GET /api/comprobantes`.

- **Excel:** una fila por comprobante (con `Hora` y `Rubro`), más una hoja "Resumen" con los totales por alícuota, los tickets y el total por rubro.
- **CSV:** separado por `;`, en UTF-8 con BOM, con coma decimal (abre bien en el Excel en castellano).

`GET /api/resumen` acepta los mismos filtros y devuelve los totales por alícuota, por tipo y por estado:

```json
{ "ok": true, "resumen": { "cantidad": 120, "neto_gravado": ..., "iva_21": ..., "iva_10_5": ..., "otros_tributos": ..., "total": ...,
  "por_estado": {...}, "por_tipo": {...},
  "tickets": { "cantidad": 14, "total": 182345.50 },
  "por_rubro": { "combustible": 98000.00, "supermercado": 61200.50, "sin_rubro": 1210000.00 } } }
```

Las notas de crédito restan y los comprobantes en moneda extranjera no se suman. `tickets` cuenta los tiques de controlador fiscal. `por_rubro` es el total por rubro de todo lo que entra en el filtro; con `clase=tickets` son los gastos con ticket por rubro.

## Canales

`GET /api/canales` devuelve `{ ok, canales: [ { canal, habilitado, conectado, detalle, qr } ] }`.

`GET /api/canales/whatsapp/qr.png` devuelve el QR de vinculación como imagen. Si no hay uno pendiente, devuelve `404`.

`POST /api/canales/webhook` es el webhook de entrada genérico, para la secretaria (Baileys), Evolution API, n8n, etc.:

- **Autenticación:** `X-Factorum-Token: <FACTORUM_WEBHOOK_TOKEN>`.
- **Cuerpo:** `{ "chat": "id", "remitente": "id", "nombre_remitente": "x", "texto": "...", "archivos": [ { "nombre": "f.pdf", "mime": "application/pdf", "contenido_base64": "..." } ] }`.
- **Respuesta:** `{ ok, respuesta: "texto para contestar" }`.

Las respuestas que vienen después (cuando termina el lote) se mandan con un `POST` a `FACTORUM_WEBHOOK_SALIDA_URL`:

- **Cuerpo:** `{ chat, texto }`.
- **Cabecera:** `X-Factorum-Firma`, un HMAC-SHA256 del cuerpo con `FACTORUM_WEBHOOK_TOKEN`.

## MCP

`POST /mcp` es Streamable HTTP, sin estado, con la misma autenticación que el resto de la API.

| Herramienta | Para qué | Permiso |
|---|---|---|
| `verificar_factura` | `{ nombre, contenido_base64, cuit_consultante? }`: el veredicto de `/api/verificar` (también para tickets) | lectura |
| `procesar_factura` | `{ nombre, contenido_base64, cliente_cuit? }`: la procesa y la guarda | operador |
| `buscar_comprobantes` | Mismos filtros que `GET /api/comprobantes`, `clase` y `rubro` incluidos. Cada resultado trae `es_tique` y, si los tiene, `hora` y `rubro` | lectura |
| `comprobante` | `{ id }` | lectura |
| `pendientes_revision` | `{ lote?, cliente? }`: lo que está en `revisar` o `error` | lectura |
| `resumen` | Mismos filtros que `/api/resumen` | lectura |
| `estado_lote` | `{ id }` | lectura |

Las respuestas son datos JSON, recortados por debajo de 20.000 caracteres.
