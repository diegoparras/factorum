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
  "veredicto": "valida" | "invalida" | "apocrifa" | "no_verificable" | "con_errores",
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
| `con_errores` | Hay errores locales (por ejemplo, QR distinto del texto) aunque ARCA la apruebe. |
| `no_verificable` | No hay Arcanum, faltan datos clave (CAE, número...) o ARCA no respondió. |

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
| `limite` | hasta 500 (por defecto 100) |
| `offset` | desde dónde |

`GET /api/comprobantes/{id}` devuelve `{ ok, comprobante: Cbte, correcciones: Correccion[] }`.

`PATCH /api/comprobantes/{id}` recibe:

```json
{ "campos": { "razon_social_emisor": "X", "iva_21": "21000.00" }, "aprobar": true, "usuario": "diego" }
```

Cada campo cambiado queda en `correcciones` (antes, después, quién y cuándo) y pasa a tener origen `manual`. Los controles se recalculan. Con `aprobar: true`, el estado pasa a `aprobado`. La respuesta es `{ ok, comprobante: Cbte }`.

`POST /api/comprobantes/{id}/constatar` constata ese comprobante con Arcanum y devuelve `{ ok, constatacion }`.

```json
Cbte = { "id": 5, "lote_id": 12, "documento_id": 33, "cliente_cuit": "...", "archivo": "f.pdf",
         ...todos los campos de modelo.Comprobante (tipo, letra, punto_venta, numero, fecha, cuit_emisor, ...),
         "estado": "ok", "confianza": 1, "hallazgos": [...], "origenes": {...},
         "constatacion": null | { "valido": true, "resultado": "A", "observaciones": [], "apoc": {...}, "fecha": "..." },
         "duplicado_de": null | 4, "creado": "...", "revisado_por": null, "revisado_en": null }
```

`GET /api/documentos/{id}/archivo` devuelve el archivo original, descomprimido y descifrado.

## Exportar

`GET /api/export.xlsx` y `GET /api/export.csv` aceptan los mismos filtros que `GET /api/comprobantes`.

- **Excel:** una fila por comprobante, más una hoja "Resumen" con los totales por alícuota.
- **CSV:** separado por `;`, en UTF-8 con BOM, con coma decimal (abre bien en el Excel en castellano).

`GET /api/resumen` acepta los mismos filtros y devuelve los totales por alícuota, por tipo y por estado:

```json
{ "ok": true, "resumen": { "cantidad": 120, "neto_gravado": ..., "iva_21": ..., "iva_10_5": ..., "otros_tributos": ..., "total": ..., "por_estado": {...}, "por_tipo": {...} } }
```

Las notas de crédito restan.

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
| `verificar_factura` | `{ nombre, contenido_base64, cuit_consultante? }`: el veredicto de `/api/verificar` | lectura |
| `procesar_factura` | `{ nombre, contenido_base64, cliente_cuit? }`: la procesa y la guarda | operador |
| `buscar_comprobantes` | Mismos filtros que `GET /api/comprobantes` | lectura |
| `comprobante` | `{ id }` | lectura |
| `pendientes_revision` | `{ lote?, cliente? }`: lo que está en `revisar` o `error` | lectura |
| `resumen` | Mismos filtros que `/api/resumen` | lectura |
| `estado_lote` | `{ id }` | lectura |

Las respuestas son datos JSON, recortados por debajo de 20.000 caracteres.
