# API de Factorum

Base: `http://factorum:8095` (puerto por defecto `FACTORUM_PUERTO=8095`).

## Autenticación y roles

Cada pedido se hace con **una clave de API** o con **la sesión del navegador**:

- **Clave de API:** `X-API-Key: <clave>` o `Authorization: Bearer <clave>`. Pueden ser:
  - `FACTORUM_API_KEY` (variable de entorno): rol `usuario`. Es la que usa Arcanum como `FACTORUM_TOKEN`. Con `FACTORUM_API_KEY_SUITE_TOPE` puede abrir sesiones de la suite.
  - `FACTORUM_API_KEY_LECTURA` (variable de entorno): rol `lectura`.
  - Una clave creada en **Claves de API** (`fct_...`): con su rol, sus CUITs, su vencimiento y, si se marcó, la suite. Nunca puede más que la persona que la creó: si a ella se le baja el rol o se la desactiva, la clave queda recortada o deja de andar.
- **Sesión:** la cookie `factorum_sesion` (HttpOnly, SameSite=Lax, Secure con URL pública https), que vence a las 12 horas sin uso. Los pedidos con cookie que modifican (`POST`, `PUT`, `PATCH`, `DELETE`) tienen que traer la cabecera `X-Factorum: 1` y un `Origin` del mismo sitio (o de la URL pública); si no, `403`. Con clave de API no hace falta.

| Rol | Puede |
|---|---|
| `lectura` | `GET` y las consultas que no modifican (`POST /api/verificar` sin guardar, MCP de lectura) |
| `usuario` | Además cargar lotes, corregir, constatar y aprobar (el `operador` de antes) |
| `admin` | Además usuarios de rol hasta admin, claves de API, los clientes de los canales y la auditoría |
| `superadmin` | Todo, incluida la configuración y los superadmin |

Sin clave ni sesión, o con una clave inválida, vencida o revocada, la respuesta es `401`. Con un rol que no alcanza, `403`. Con la contraseña temporal sin cambiar, todo da `403 { "debe_cambiar": true }` salvo `GET /api/sesion` y `POST /api/sesion/clave`. `/healthz`, `/api/novedades` y `GET /api/arranque` no piden nada.

### Alcance por CUIT de cliente

Una persona o una clave puede tener `cuits`: la lista de CUITs de cliente que puede ver (`null` = todos). Se aplica a todo:

- Listados de comprobantes y lotes, resumen, panel, exportación y rubros traen solo lo de esos clientes.
- Un comprobante, lote, documento o página de otro cliente da `404`; lo mismo `PATCH` y `constatar`.
- `POST /api/lotes` con un `cliente_cuit` ajeno da `403`; sin `cliente_cuit`, si tiene un solo CUIT se usa ese, y si tiene varios, `400`.
- `POST /api/verificar` con un `cuit_consultante` ajeno da `403`.
- MCP: las mismas reglas en todas las herramientas.
- `/metrics` es de toda la instalación: con restricción, `403`.

Todas las respuestas son JSON con `ok: true|false`. Ante un error, la respuesta lleva `error` (un mensaje en castellano) y el código HTTP que corresponde.

Los importes van como números con dos decimales (`121000.00`) y las fechas como `AAAA-MM-DD`.

## Salud

`GET /healthz` devuelve:

```json
{ "ok": true, "version": "0.3.0", "base": true, "ocr": true, "ia": { "habilitada": true, "modelo": "qwen2.5vl:7b", "local": true }, "arcanum": true,
  "almacen": "r2", "cifrado": true, "nombre": "Factorum", "ia_por_defecto": true, "constatar_por_defecto": true, "arranque": false }
```

`arranque: true` quiere decir que todavía no hay usuarios (ver abajo).

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
Lote = { "id": 12, "nombre": "Noviembre", "cliente_cuit": "20222222223", "origen": "web|telegram|whatsapp|webhook|correo|mcp|arcanum",
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
| `offset` | desde dónde |

Un valor de `clase` o `rubro` que no se entiende da `400`.

`GET /api/comprobantes/{id}` devuelve `{ ok, comprobante: Cbte, correcciones: Correccion[] }`.

`PATCH /api/comprobantes/{id}` recibe:

```json
{ "campos": { "razon_social_emisor": "X", "iva_21": "21000.00" }, "aprobar": true, "usuario": "diego" }
```

Quién corrige: con sesión, el email de la persona (se ignora `usuario`); con una clave, `usuario` si viene (la app que llama dice en nombre de quién) o la clave.

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

Cuando una persona corrige el rubro con `PATCH`, Factorum lo guarda para ese CUIT, lo pasa a los comprobantes ya cargados de ese emisor y lo usa en los que vengan. Solo se respetan los comprobantes a los que una persona les corrigió el rubro a ellos en particular; los que lo tomaron del emisor siguen la última corrección.

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

`GET /api/panel` (lectura) devuelve lo que muestra Inicio: documentos y comprobantes de hoy y del mes, cantidades por estado, apócrifos, posibles adulteraciones, cuántos se leyeron con IA, los últimos 14 días (hora de Buenos Aires) y los últimos 5 lotes.

`GET /api/novedades` (sin clave) devuelve las versiones del CHANGELOG, de la más nueva a la más vieja: `{ "ok": true, "novedades": [{ "version", "fecha", "cuerpo" }] }`.

## Canales

WhatsApp, Telegram y el webhook se configuran en **Configuración** (o por variables de entorno). Vincular y desvincular WhatsApp es de superadmin, con sesión.

`GET /api/canales` devuelve `{ ok, canales: [ { canal, habilitado, conectado, detalle, qr } ] }`.

`GET /api/canales/whatsapp/qr.png` devuelve el QR de vinculación como imagen. Si no hay uno pendiente, devuelve `404`.

`POST /api/canales/webhook` es el webhook de entrada genérico, para la secretaria (Baileys), Evolution API, n8n, etc.:

- **Autenticación:** `X-Factorum-Token: <FACTORUM_WEBHOOK_TOKEN>`.
- **Cuerpo:** `{ "chat": "id", "remitente": "id", "nombre_remitente": "x", "texto": "...", "archivos": [ { "nombre": "f.pdf", "mime": "application/pdf", "contenido_base64": "..." } ] }`.
- **Respuesta:** `{ ok, respuesta: "texto para contestar" }`.

Las respuestas que vienen después (cuando termina el lote) se mandan con un `POST` a `FACTORUM_WEBHOOK_SALIDA_URL`:

- **Cuerpo:** `{ chat, texto }`.
- **Cabecera:** `X-Factorum-Firma`, un HMAC-SHA256 del cuerpo con `FACTORUM_WEBHOOK_TOKEN`.

## Correo

### Entrada

Solo atiende la forma elegida en Configuración (`correo.forma`); las demás contestan `404`. Todas terminan en lo mismo: un mail con adjuntos útiles genera un lote de origen `correo`. Del mail se toman el remitente, los destinatarios, el asunto, la fecha, el `Message-ID`, `References` y los adjuntos (PDF, JPG, PNG, WEBP, ZIP y lo que haya dentro de un `.eml` adjunto); el texto no se lee. Un `Message-ID` que ya llegó no hace nada.

`POST /api/correo/entrante/cloudflare` (Email Worker de Cloudflare)

- Cuerpo: el mail crudo (RFC 822), `Content-Type: message/rfc822`, hasta el tope por mail (`correo.max_mb`, 25 MB).
- `X-Factorum-Para`: el destinatario del sobre (`message.to`).
- `X-Factorum-Firma: t=<unix>,v1=<hex>`, con `v1 = HMAC-SHA256(secreto, "<t>.<para>." + mail)`. Con la hora corrida más de 5 minutos, `401`; un pedido repetido, `409`.
- Respuestas: `200 { ok, estado: "procesado|retenido|ignorado|repetido", motivo, lote }`; `401` firma mal o vencida; `413` muy grande; `422` ilegible; `503` falla pasajera. Con un error, el Worker rechaza el mail (`message.setReject`) y quien lo mandó recibe el rebote.

`POST /api/correo/entrante/{mailgun|postmark|sendgrid}`

| Proveedor | Autenticación | Formato |
|---|---|---|
| `mailgun` | La firma de Mailgun: `signature = HMAC-SHA256(signing key, timestamp + token)`, con `timestamp` de menos de 5 minutos y `token` que no se repite (`406` si se repite) | `multipart/form-data` de una ruta «Forward» (`from`, `recipient`, `subject`, `message-headers`, `attachment-N`) o con `body-mime` |
| `postmark` | El token de la URL: `https://factorum:<token>@host/...` (autenticación básica) o `?token=` | El JSON de Inbound (`FromFull`, `OriginalRecipient`, `Headers`, `Attachments` en base64; o `RawEmail`) |
| `sendgrid` | Igual que Postmark | Inbound Parse: con «POST the raw, full MIME message», el campo `email`; si no, `from`, `to`, `headers`, `envelope`, `attachment-info` y `attachmentN` |

Sin autenticación válida, `401`. Un mail que no se puede leer contesta `200 { estado: "ignorado" }` (si no, el proveedor reintenta por días); una falla pasajera, `503` (el proveedor reintenta).

IMAP y Gmail no tienen endpoint: el servidor revisa la casilla o la etiqueta cada tanto (`imap.intervalo`, `gmail.intervalo`).

Reglas:

- **Permitidos** (`correo.permitidos`: emails o `@dominio`). Lo de los demás queda retenido. Sin lista, todo.
- **Cliente**: un destinatario `algo+CUIT@dominio` con CUIT válido; si no, el remitente (email exacto o `@dominio`) en `clientes.canales`; si no, sin cliente.
- **Respuesta** (`correo.contestar` y correo saliente configurado): cuando termina el lote (y se constata), un mail al remitente en el mismo hilo (`In-Reply-To` y `References`). Solo si el remitente está permitido en ese momento: a un retenido nunca.

### Correo retenido (admin, con sesión)

- `GET /api/correo/retenido?limite&offset` → `{ retenidos: [ { id, message_id, forma, remitente, nombre, destinatario, asunto, fecha, tamano, adjuntos: [ { nombre, mime, tamano } ], descartados, cliente_cuit, estado, motivo, creado } ], total, permitidos, permitidos_fijados }`. Con alcance por CUIT, solo los retenidos de esos clientes (los que no tienen cliente no se ven).
- `POST /api/correo/retenido/{id}/aceptar` `{ permitir }` → `{ lote, permitido?, aviso? }`: lo procesa y, con `permitir`, suma al remitente a `correo.permitidos`. Si ya se resolvió, `409`.
- `POST /api/correo/retenido/{id}/descartar` → borra los adjuntos y lo deja descartado.
- Los retenidos se borran a los 30 días.

### Gmail con permiso de Google (superadmin, con sesión)

- `POST /api/correo/google/conectar` → `{ url }`: la pantalla de consentimiento de Google (Authorization Code con PKCE, permiso `gmail.modify`, redirect `<URL pública>/api/correo/google/callback`). Hace falta guardar antes el client ID y el secret. El pedido vale 10 minutos.
- `GET /api/correo/google/callback` → canjea el código, guarda el refresh token cifrado y la cuenta, y vuelve a `/?google=ok#configuracion` (o `?google=error&detalle=...`). Solo con la sesión del mismo superadmin que lo pidió.
- `POST /api/correo/google/desconectar` → revoca el permiso en Google y lo borra.

## MCP

`POST /mcp` es Streamable HTTP, sin estado. Solo con clave de API (no acepta la cookie del navegador). Cada clave ve sus clientes y, con rol lectura, no tiene `procesar_factura`.

| Herramienta | Para qué | Permiso |
|---|---|---|
| `verificar_factura` | `{ nombre, contenido_base64, cuit_consultante? }`: el veredicto de `/api/verificar` (también para tickets) | lectura |
| `procesar_factura` | `{ nombre, contenido_base64, cliente_cuit? }`: la procesa y la guarda | usuario |
| `buscar_comprobantes` | Mismos filtros que `GET /api/comprobantes`, `clase` y `rubro` incluidos. Cada resultado trae `es_tique` y, si los tiene, `hora` y `rubro` | lectura |
| `comprobante` | `{ id }` | lectura |
| `pendientes_revision` | `{ lote?, cliente? }`: lo que está en `revisar` o `error` | lectura |
| `resumen` | Mismos filtros que `/api/resumen` | lectura |
| `estado_lote` | `{ id }` | lectura |

Las respuestas son datos JSON, recortados por debajo de 20.000 caracteres.

## Arranque y sesión

- `GET /api/arranque` → `{ ok, necesita, nombre }`. `necesita: true` si todavía no hay usuarios.
- `POST /api/arranque` `{ codigo, email, nombre, clave }` → crea el primer superadmin y abre la sesión. El código de un solo uso se escribe en el log al arrancar (`codigo_arranque`) y lo muestra `factorum salud`. Si ya hay usuarios, `409`.
- `POST /api/sesion` `{ email, clave }` → abre la sesión (cookie). Email sin distinguir mayúsculas. `401` si no coinciden; `403` si la cuenta está desactivada; `429` si hubo demasiados intentos (cinco libres por IP y por email; después, una espera que se duplica hasta 15 minutos).
- `GET /api/sesion` (sesión o clave) → `{ ok, identidad: { tipo, usuario_id, email, nombre, rol, cuits, debe_cambiar, origen, clave }, permisos: { cargar, corregir, usuarios, claves, auditoria, clientes, configuracion }, instalacion }`.
- `DELETE /api/sesion` → cierra la sesión.
- `POST /api/sesion/clave` `{ actual, nueva }` → cambia la contraseña propia (10 caracteres o más) y cierra las otras sesiones.
- `GET /api/arranque` trae además `olvide: true` si hay correo saliente y URL pública (el login muestra «Olvidé mi contraseña»), y `GET /api/sesion`, `correo_saliente`.
- `POST /api/sesion/olvide` `{ email }` → siempre `200 { mensaje }`, exista o no la cuenta (el mail sale aparte). Manda a `<URL pública>/#restablecer?t=<token>` un link de un solo uso que vence en 30 minutos. Tope: 3 pedidos por email y 10 por IP por hora (`429` el de la IP; el del email no se nota). Sin correo saliente, `409`.
- `POST /api/sesion/link` `{ token }` → `{ tipo: "restablecer|invitacion", email, nombre }` sin usarlo; vencido o usado, `410`.
- `POST /api/sesion/restablecer` `{ token, clave }` → pone la contraseña, cierra todas las sesiones de esa persona, invalida sus otros links y abre una sesión nueva. Vencido o usado, `410`. El token se guarda por su SHA-256 (32 bytes al azar).

## Usuarios (admin, con sesión)

- `GET /api/usuarios` → `{ usuarios: [ { id, email, nombre, rol, cuits, tiene_clave, debe_cambiar, activo, origen, creado, ultimo_ingreso } ] }`.
- `POST /api/usuarios` `{ email, nombre, rol, cuits }` → `201 { usuario, clave_temporal }`. La contraseña temporal se devuelve **una sola vez** y obliga a cambiarla al entrar.
- `PATCH /api/usuarios/{id}` `{ nombre?, rol?, cuits?, activo? }`. Desactivar o cambiar rol o CUITs cierra sus sesiones.
- `POST /api/usuarios/{id}/restablecer` → `{ usuario, clave_temporal }` (una sola vez); cierra sus sesiones.
- `POST /api/usuarios` con `invitar: true` → `201 { usuario, invitado }`: sin contraseña temporal; le llega un mail con un link para elegir la suya (vence en 72 horas). Si el mail no sale, la persona queda creada y viene `aviso`. Sin correo saliente o sin URL pública, `409`.
- `POST /api/usuarios/{id}/link` → `{ tipo }`: le manda por mail la invitación otra vez (si todavía no eligió contraseña) o un link para restablecerla (30 minutos).

Reglas: nadie da un rol mayor al suyo ni CUITs que no tiene; un admin no toca superadmins; siempre queda un superadmin activo (`409`); nadie se desactiva a sí mismo. `cuits` acepta una lista o un texto separado por comas; vacío o `null` = todos.

## Claves de API (admin, con sesión)

- `GET /api/claves` → `{ claves: [ { id, nombre, prefijo, rol, cuits, usuario_id, dueno, creado, vence, ultimo_uso, revocada, suite, suite_tope } ] }`. Nunca la clave.
- `POST /api/claves` `{ nombre, rol, cuits, vence: "AAAA-MM-DD"|"", suite, suite_tope }` → `201 { clave, datos }`. La clave (`fct_...`) se devuelve **una sola vez**; se guarda su SHA-256. Vence al terminar ese día (hora argentina). El tope de la suite es `usuario` por defecto; `admin` o `superadmin` solo los pone un superadmin.
- `DELETE /api/claves/{id}` → la revoca (queda en la lista como revocada).

## Configuración (con sesión)

- `GET /api/configuracion` (admin) → `{ campos, secciones, canales, url_publica, webhook_entrada }`. Un superadmin ve todas las secciones (`general`, `arcanum`, `telegram`, `whatsapp`, `webhook`, `correo_entrada`, `correo_salida`, `avisos`, `ia`, `r2`, `clientes`) y además `correo: { urls, estado, smtp, google: { conectado, cuenta, fijado } }` (las URLs para cada servicio, sin tokens); un admin, solo `clientes`. Los campos de tipo `opcion` traen `opciones: [ { valor, etiqueta } ]` y los `entero`, `min` y `max`. Cada campo: `{ clave, seccion, etiqueta, tipo, env, ayuda, ejemplo, valor, configurado, ultimos, bloqueado, origen, actualizado_por, actualizado }`. Un secreto nunca trae `valor`: solo `configurado` y, si es largo, `ultimos` (los últimos 4 caracteres).
- `PUT /api/configuracion` `{ cambios: { "telegram.token": "...", "telegram.permitidos": ["1","2"], "general.url_publica": null } }` → la vista nueva más `cambiadas`. `null` borra lo guardado (vuelve al entorno o al valor de fábrica); un secreto `""` no se toca. Un campo fijado por una variable de entorno da `409`; un valor inválido, `400`. Al guardar se reconfiguran sin reiniciar Arcanum, el modelo de visión, el almacén y los canales.
- `POST /api/configuracion/probar/{seccion}` (superadmin) `{ cambios }` → `{ exito, mensaje }`. Prueba con lo que está en pantalla, sin guardar: `general` (la URL pública responde), `arcanum` (`/api/health` y `/api/tenants`), `telegram` (`getMe`), `webhook` (un aviso firmado a la URL de salida), `ia` (`/models` y que esté el modelo), `r2` (pone y borra un objeto), `correo_entrada` (IMAP: entra y cuenta los mensajes de la carpeta; Gmail: la cuenta y la etiqueta; Cloudflare y proveedores: que la URL pública responda y el secreto esté) y `correo_salida` (con `destino` en el cuerpo, manda un mail de prueba; sin él, solo entra).

| Clave | Variable | Tipo |
|---|---|---|
| `general.nombre`, `general.url_publica` | `FACTORUM_NOMBRE`, `FACTORUM_URL_PUBLICA` | texto, URL |
| `general.ia_por_defecto`, `general.constatar_por_defecto` | `FACTORUM_IA_POR_DEFECTO`, `FACTORUM_CONSTATAR_POR_DEFECTO` | sí/no |
| `arcanum.url`, `arcanum.url_publica`, `arcanum.api_key`, `arcanum.cuit_consultante`, `arcanum.suite_crear_usuarios` | `ARCANUM_URL`, `ARCANUM_URL_PUBLICA`, `ARCANUM_API_KEY`, `ARCANUM_CUIT_CONSULTANTE`, `FACTORUM_SUITE_CREAR_USUARIOS` | URL, URL, secreto, CUIT, sí/no |
| `telegram.token`, `telegram.permitidos` | `FACTORUM_TELEGRAM_TOKEN`, `FACTORUM_TELEGRAM_PERMITIDOS` | secreto, lista |
| `whatsapp.activo`, `whatsapp.permitidos` | `FACTORUM_WHATSAPP`, `FACTORUM_WHATSAPP_PERMITIDOS` | sí/no, lista |
| `webhook.token`, `webhook.salida_url` | `FACTORUM_WEBHOOK_TOKEN`, `FACTORUM_WEBHOOK_SALIDA_URL` | secreto, URL |
| `ia.url`, `ia.modelo`, `ia.api_key` | `LLM_API_BASE_URL`, `LLM_MODEL`, `LLM_API_KEY` | URL, texto, secreto |
| `r2.endpoint`, `r2.bucket`, `r2.access_key`, `r2.secret_key`, `r2.prefijo` | `FACTORUM_R2_*` | URL, texto, secreto, secreto, texto |
| `clientes.canales` | `FACTORUM_CANAL_CLIENTES` | remitente → CUIT (`{ "5491155550000": "20222222223" }` o `"5491155550000 = 20-22222222-3"` por renglón). El remitente puede ser un email o `@dominio` |
| `correo.forma`, `correo.permitidos`, `correo.max_mb`, `correo.contestar` | `FACTORUM_CORREO_FORMA`, `FACTORUM_CORREO_PERMITIDOS`, `FACTORUM_CORREO_MAX_MB`, `FACTORUM_CORREO_CONTESTAR` | opción (`imap`, `cloudflare`, `proveedor`, `gmail`, `apagado`), emails o `@dominio`, entero (1 a 100), sí/no |
| `imap.host`, `imap.puerto`, `imap.tls`, `imap.usuario`, `imap.clave`, `imap.carpeta`, `imap.intervalo`, `imap.procesado`, `imap.carpeta_procesado` | `FACTORUM_IMAP_*` | texto, entero, opción (`implicito`, `starttls`, `ninguno`), texto, secreto, texto, entero (15 a 3600), opción (`leido`, `mover`), texto |
| `cloudflare.secreto` | `FACTORUM_CLOUDFLARE_SECRETO` | secreto |
| `correo.proveedor`, `mailgun.signing_key`, `correo.token` | `FACTORUM_CORREO_PROVEEDOR`, `FACTORUM_MAILGUN_SIGNING_KEY`, `FACTORUM_CORREO_TOKEN` | opción (`mailgun`, `postmark`, `sendgrid`), secreto, secreto |
| `google.client_id`, `google.client_secret`, `gmail.etiqueta`, `gmail.procesado`, `gmail.intervalo` | `FACTORUM_GOOGLE_CLIENT_ID`, `FACTORUM_GOOGLE_CLIENT_SECRET`, `FACTORUM_GMAIL_*` | secreto, secreto, texto, opción (`quitar`, `leido`), entero. El refresh token lo guarda «Conectar con Google» y no se cambia por `PUT` (`400`) |
| `smtp.host`, `smtp.puerto`, `smtp.tls`, `smtp.usuario`, `smtp.clave`, `smtp.remitente`, `smtp.responder_a` | `FACTORUM_SMTP_*` | texto, entero, opción, texto, secreto, texto (`Nombre <email>`), texto |
| `avisos.lote`, `avisos.lote_a`, `avisos.alertas`, `avisos.alertas_a`, `avisos.semanal`, `avisos.semanal_a` | `FACTORUM_AVISO_*` | sí/no y emails |

### Avisos por mail

Salen por la cola, una vez por aviso y destinatario (aunque la cola reintente):

- **Lote terminado** (`avisos.lote`): a quien lo cargó desde la pantalla; si vino por chat, por la API o por mail, a `avisos.lote_a`.
- **Apócrifos y adulterados** (`avisos.alertas`): a los admins y superadmins activos que ven ese cliente, y a `avisos.alertas_a`.
- **Resumen semanal** (`avisos.semanal`): los lunes desde las 8 (hora de Buenos Aires), lo cargado la semana anterior y lo pendiente de revisar, por cliente. A `avisos.semanal_a` o, sin lista, a los admins.
- Una dirección que es de una persona de Factorum recibe solo lo de sus clientes (y nada si está desactivada); una que no es de nadie recibe lo que se le manda (la puso un superadmin).

## Auditoría (admin, con sesión)

`GET /api/auditoria?desde=AAAA-MM-DD&hasta=AAAA-MM-DD&persona=texto&accion=sesion.&limite=50&offset=0` → `{ eventos: [ { id, creado, actor, actor_tipo, usuario_id, accion, objeto, detalle, ip } ], total, acciones }`. Las fechas van en hora argentina; `accion` terminada en punto filtra por grupo.

Acciones: `sesion.entrar`, `sesion.fallida`, `sesion.salir`, `usuario.crear`, `usuario.editar`, `usuario.desactivar`, `usuario.reactivar`, `usuario.restablecer`, `usuario.cambiar_clave`, `clave.crear`, `clave.revocar`, `config.cambiar` (qué claves, nunca los valores), `config.whatsapp_desvincular`, `lote.crear`, `comprobante.corregir`, `comprobante.aprobar`, `exportar`, `suite.pase_emitido`, `suite.pase_usado`, `suite.pase_rechazado`, `suite.abrir`, `suite.abrir_fallido`, `correo.recibido`, `correo.retenido`, `correo.ignorado` (sin adjuntos útiles, muy grande o ilegible), `correo.aceptar`, `correo.descartar`, `config.google_conectar`, `config.google_desconectar`, `usuario.pedir_restablecer`, `usuario.restablecer_por_mail`, `usuario.aceptar_invitacion`, `usuario.link_por_mail`. Del correo, quién lo mandó, la forma y cuántos adjuntos; nunca el texto ni el asunto. Nunca lleva contraseñas, claves, tokens ni pases.

## Suite Escriba: el pase entre apps

El contrato completo está en [suite-pase.md](suite-pase.md).

- `POST /api/suite/pase` (solo clave de API habilitada para la suite; nunca una sesión) `{ app: "arcanum", email, nombre, rol, cuits }` → `{ ok, url, vence }`. `url` es `<URL pública>/suite/entrar?pase=<token>`; el pase sirve una vez y vence a los 60 segundos. Errores: `403 "Esta clave no está habilitada para abrir sesiones de la suite"`, `409 "Falta la URL pública de Factorum"`, `403` si la persona está desactivada, si no tiene cuenta y la creación automática está apagada, o si no queda rol o CUIT en común.
- `GET /suite/entrar?pase=...` → abre la sesión y redirige a `/`. Con un pase vencido o usado, redirige a `/?aviso=pase`.
- `GET /api/suite/apps` (sesión) → `{ ok, apps: [ { app: "arcanum", nombre: "Arcanum", disponible } ] }`. `disponible` = respondió su healthz en los últimos 60 segundos.
- `POST /api/suite/abrir/arcanum` (sesión) → `{ ok, url }`: pide el pase a Arcanum con la clave guardada en Configuración. Los errores de Arcanum vuelven con su mensaje.
