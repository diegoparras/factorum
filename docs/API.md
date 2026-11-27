# API de Factorum

Base: `http://factorum:8095` (puerto por defecto `FACTORUM_PUERTO=8095`).

## Autenticación y roles

Cada pedido se hace con **una clave de API** o con **la sesión del navegador**:

- **Clave de API:** `X-API-Key: <clave>` o `Authorization: Bearer <clave>`. Pueden ser:
  - `FACTORUM_API_KEY` (variable de entorno): rol `usuario`. Es la que usa Arcanum como `FACTORUM_TOKEN`. Con `FACTORUM_API_KEY_SUITE_TOPE` puede abrir sesiones de la suite.
  - `FACTORUM_API_KEY_LECTURA` (variable de entorno): rol `lectura`.
  - Una clave creada en **Claves de API** (`fct_...`): con su rol, sus CUITs, su vencimiento y, si se marcó, la suite. Nunca puede más que la persona que la creó: si a ella se le baja el rol o se la desactiva, la clave queda recortada o deja de andar.
- **Sesión:** la cookie `factorum_sesion` (HttpOnly, SameSite=Lax, Secure con URL pública https), que vence a las 12 horas sin uso. Se abre con email y contraseña, con Lockatus (ver [Lockatus](#lockatus-inicio-de-sesión-único)) o con el pase de la suite; es la misma en los tres casos (cambia el `origen`: `local`, `lockatus` o `suite:arcanum`). Los pedidos con cookie que modifican (`POST`, `PUT`, `PATCH`, `DELETE`) tienen que traer la cabecera `X-Factorum: 1` y un `Origin` del mismo sitio (o de la URL pública); si no, `403`. Con clave de API no hace falta.

| Rol | Puede |
|---|---|
| `lectura` | `GET` y las consultas que no modifican (`POST /api/verificar` sin guardar, MCP de lectura) |
| `usuario` | Además cargar lotes, corregir, constatar y aprobar (el `operador` de antes); borrar comprobantes, cambiar el cliente de un lote, ver la papelera y restaurar; exportar al Libro IVA Digital |
| `admin` | Además borrar lotes enteros, usuarios de rol hasta admin, claves de API, los clientes de los canales y la auditoría |
| `superadmin` | Todo, incluida la configuración, los superadmin y, con sesión, el borrado definitivo a mano (vaciar la papelera) |

Sin clave ni sesión, o con una clave inválida, vencida o revocada, la respuesta es `401`. Con un rol que no alcanza, `403`. Con la contraseña temporal sin cambiar, todo da `403 { "debe_cambiar": true }` salvo `GET /api/sesion` y `POST /api/sesion/clave`. `/healthz`, `/api/novedades` y `GET /api/arranque` no piden nada.

### Alcance por CUIT de cliente

Una persona o una clave puede tener `cuits`: la lista de CUITs de cliente que puede ver (`null` = todos). Una persona puede tener además `cuits: []`, **ninguno todavía** (quien entra por primera vez con Lockatus con rol `usuario` o `lectura`): todo le da `403 "No tenés ningún cliente asignado"` salvo `GET /api/sesion` (con `permisos.sin_clientes: true`), `DELETE /api/sesion` y `POST /api/sesion/clave`. Se aplica a todo:

- Listados de comprobantes y lotes, resumen, panel, exportación y rubros traen solo lo de esos clientes.
- Un comprobante, lote, documento o página de otro cliente da `404`; lo mismo `PATCH` y `constatar`.
- `POST /api/lotes` con un `cliente_cuit` ajeno da `403`; sin `cliente_cuit`, si tiene un solo CUIT se usa ese, y si tiene varios, `400`.
- `POST /api/verificar` con un `cuit_consultante` ajeno da `403`.
- Borrar, restaurar y cambiar el cliente: solo lo de esos clientes (lo demás da `404`); el cliente nuevo de un lote también tiene que ser uno de esos (`403`).
- MCP: las mismas reglas en todas las herramientas.
- `/metrics` es de toda la instalación: con restricción, `403`.

Todas las respuestas son JSON con `ok: true|false`. Ante un error, la respuesta lleva `error` (un mensaje en castellano) y el código HTTP que corresponde.

Los importes van como números con dos decimales (`121000.00`) y las fechas como `AAAA-MM-DD`.

## Salud

`GET /healthz` devuelve:

```json
{ "ok": true, "version": "0.3.0", "base": true, "ocr": true, "ia": { "habilitada": true, "modelo": "qwen3-vl:4b-instruct", "local": true }, "arcanum": true,
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
| `ia` | `true` o `false`. Con una IA externa, desde una sesión de una persona que todavía no vio el aviso, responde `409` con `codigo: "aviso_ia"` y `aviso_ia` (ver [Aviso de la IA](#aviso-de-la-ia-externa)). |
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
         "comprobantes": { "ok": 80, "revisar": 5, "error": 2, "aprobado": 0 },
         "constatacion_pendiente": false, "constatado": true }
```

Los números cuentan solo lo vivo: lo que está en la [papelera](#papelera) no se cuenta. `constatacion_pendiente`: el lote se constata y le falta algún comprobante; `constatado`: alguno ya se constató.

`DELETE /api/lotes/{id}` (admin o más) manda el lote entero a la papelera, con sus documentos y comprobantes. Devuelve `{ ok, borrado: { documentos, comprobantes, trabajos_cancelados }, dias }`. Si todavía se está procesando, en la misma transacción se cancelan sus trabajos de la cola (lo que espera y lo que está corriendo); si un documento termina de leerse justo después, su resultado no se guarda. Al restaurarlo, lo que quedó sin procesar vuelve a la cola. Un lote que no existe, ya está en la papelera o es de otro cliente: `404`.

`PATCH /api/lotes/{id}` (usuario o más) `{ "cliente_cuit": "30-71111111-1" }` cambia el cliente del lote y de todos sus comprobantes (también los que están en la papelera). No borra nada. El CUIT tiene que ser válido (dígito verificador; si no, `400`) y estar entre los clientes de quien pide (`403`). Lo que ya se constató con el CUIT consultante del cliente viejo pierde esa constatación, queda con el aviso `reconstatar` y se vuelve a constatar con el nuevo: el lote entero si se constata al terminar (si todavía se está procesando, lo hace el cierre del lote) o solo esos comprobantes si se habían constatado de a uno. Sin Arcanum, quedan sin constatar con el aviso. Devuelve `{ ok, lote, cambio: { antes, despues, comprobantes, reconstatar, constatacion_encolada } }`.

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

`POST /api/comprobantes/borrar` (usuario o más) `{ "ids": [12, 13] }` manda esos comprobantes a la papelera (hasta 5.000 por pedido). Si a un documento no le queda ningún comprobante vivo, se va con ellos. Si alguno no existe, ya está en la papelera o es de otro cliente, no se borra ninguno: `404` con los ids. Devuelve `{ ok, borrado: { documentos, comprobantes }, dias }`.

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

## Conciliación con ARCA

Cruza lo cargado de un cliente con lo que ARCA dice que ese CUIT **recibió** (Mis Comprobantes, que baja Arcanum). Necesita Arcanum 2.3.0 o superior conectado, con una clave que tenga ese CUIT (alcanza con rol lectura en Arcanum).

`GET /api/conciliacion?cliente=CUIT&desde=AAAA-MM-DD&hasta=AAAA-MM-DD[&refrescar=1]` (lectura o más). El cliente tiene que estar en el alcance de quien pide (si no, `403`); con un solo cliente asignado se puede omitir. El período va por fecha de emisión y es de un año como mucho (`400` si no).

- Factorum le pide a Arcanum lo recibido con **31 días de margen** a cada lado, para emparejar un comprobante cuya fecha se leyó mal; lo que queda afuera del período en los dos lados no aparece.
- Lo que trae Arcanum se guarda **5 minutos** por CUIT y rango (`de_cache: true`); `refrescar=1` lo vuelve a pedir.

```json
{ "ok": true, "estado": "lista", "mensaje": "Arcanum no bajó todo el período: ...",
  "cliente": "20222222223", "desde": "2026-11-01", "hasta": "2026-11-30",
  "cobertura": { "sincronizado": true, "rangos": [{ "desde": "2026-10-15", "hasta": "2026-11-22" }],
    "ultimaSincronizacion": { "corrida": 2, "terminada": "2026-11-23T09:18:00.000Z", "desde": "...", "hasta": "..." },
    "ultimaCorrida": { "estado": "error", "error": "..." }, "enCurso": null, "credencial": true, "sincronizarDiaria": true },
  "sin_sincronizar": [{ "desde": "2026-11-23", "hasta": "2026-11-30" }], "cubre_todo": false,
  "en_factorum": 13, "en_arca": 8, "truncado": false, "sin_fecha": 0, "consultado": "...", "de_cache": false,
  "resultado": {
    "grupos": { "coinciden": [...], "diferencias": [...], "falta_cargar": [...], "no_figura": [...] },
    "totales": { "coinciden": { "cantidad": 1, "pesos": 121000.00, "pesos_factorum": 121000.00, "pesos_arca": 121000.00,
      "cantidad_moneda_extranjera": 0, "moneda_extranjera": {} }, "no_figura": { ..., "por_causa": { "tique": 3 }, "legitimas": 7, "alarmas": 1 } },
    "duplicados": { "factorum": 2, "arca": 0 }, "incompletos": 0 } }
```

`estado` es `lista` (hay `resultado`) o, con un `mensaje` para la persona y sin `resultado`: `sin_arcanum` (Factorum no está conectado), `sin_sincronizar` (Arcanum nunca bajó Mis Comprobantes de ese CUIT), `sin_permiso` (la clave de Arcanum no tiene ese CUIT), `arcanum_viejo` (anterior a 2.3.0), `arcanum_caido` o `arcanum_error`.

**Cómo empareja.** La clave es el CUIT del emisor (solo dígitos), el tipo como código de ARCA, el punto de venta y el número como enteros. Los importes se comparan en centavos, con **un centavo de tolerancia** por importe; las notas de crédito se comparan en valor absoluto y en los totales restan.

| Grupo | Qué es |
|---|---|
| `coinciden` | Está en los dos con la misma fecha, moneda y total; y el mismo neto e IVA en las letras A y M cuando ARCA los trae (en B y C el IVA va incluido y no se compara) |
| `diferencias` | Está en los dos pero algo no cierra: `diferencias: [{ campo, factorum, arca, diferencia \| dias }]` con `campo` `total`, `neto_gravado`, `iva`, `fecha` o `moneda` (con otra moneda no se comparan importes). `casi_igual: true` es una **sugerencia**: mismo emisor y número con otro punto de venta o tipo de la misma clase, y el mismo total o la misma fecha (típico de un error de lectura). Nunca cuenta como coincidencia |
| `falta_cargar` | Está en ARCA y no en Factorum. `arca.emisor_apocrifo` sale de la constatación de recibidos de Arcanum |
| `no_figura` | Está en Factorum y no en ARCA, con `causa`: `tique`, `exterior` (moneda extranjera, factura E o CUIT de país), `otro_receptor` (a nombre de otro CUIT o a consumidor final), `no_sincronizado` (fecha en un tramo que Arcanum no bajó), `nota` (crédito o débito), `incompleto` (sin CUIT, tipo o número) o `sin_explicacion`. `legitima` marca las causas que explican la ausencia; `constatar` sugiere constatarlo y `alarma`, que ya se constató y el emisor es apócrifo o ARCA no lo reconoce |

Cada ítem trae `factorum` (con `id`, para abrirlo) y `arca` cuando están, `nota` con la explicación y, si el mismo comprobante está más de una vez, `duplicados_factorum` (los otros ids) y `duplicados_arca`. En `totales`, `pesos` usa el importe de ARCA cuando está; la moneda extranjera va aparte, por moneda.

`GET /api/conciliacion/clientes` (lectura): los clientes que ve quien pide, con su nombre como receptor y cuántos comprobantes tienen (con clientes asignados, están todos aunque no tengan nada cargado).

`GET /api/conciliacion.xlsx` y `GET /api/conciliacion.csv`, con los mismos parámetros y, opcional, `grupo=coinciden|diferencias|falta_cargar|no_figura` para bajar solo uno. El Excel trae una hoja **Resumen** (cliente, período, sincronización de Arcanum, avisos y totales) y una hoja por grupo; el CSV es una sola tabla con el grupo en la primera columna. Si el estado no es `lista`, `409` con el mensaje. Cada exportación queda en la auditoría (`conciliacion.exportar`).

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

## Papelera

Borrar no borra nada de verdad: marca (`borrado_en`, `borrado_por`) el lote o los comprobantes y sus documentos. Todo lo que lista, cuenta, resume o exporta lo ignora: `GET /api/lotes`, `/api/comprobantes` (y la bandeja de Revisar), `/api/resumen`, las exportaciones, `/api/panel`, `/metrics`, `/api/rubros`, MCP, las respuestas de los chats, los avisos y el resumen semanal por mail, la detección de duplicados y la de archivos repetidos. Un comprobante, lote o documento de la papelera da `404` (también al corregirlo o constatarlo).

**Volver a subir.** Un archivo cuyo SHA-256 está solo en la papelera no es «repetido»: se procesa de nuevo como nuevo. El viejo sigue en la papelera hasta que vence o se vacía.

- `GET /api/papelera?limite=100&offset=0` (usuario o más, lo de sus clientes) → `{ ok, items, total, dias }`. Cada item:

```json
{ "tipo": "lote|comprobante", "id": 12, "lote_id": 12, "lote_nombre": "Noviembre", "lote_borrado": false, "cliente_cuit": "20222222223",
  "borrado_en": "2026-11-27T12:00:00Z", "borrado_por": "ana@estudio.com.ar", "vence": "2026-12-27T12:00:00Z", "dias_restantes": 30,
  "documentos": 3, "comprobantes": 4,
  "archivo": "a.pdf", "cbte_tipo": 1, "punto_venta": 2, "numero": 1543, "fecha": "2026-11-02", "emisor": "ACME SA", "cuit_emisor": "30711111111", "total": "121000.00", "estado": "ok" }
```

  Un lote trae `documentos` y `comprobantes` (lo que se fue con él); un comprobante borrado suelto, sus datos y, si su lote también está en la papelera, `lote_borrado: true`. Lo que se fue con su lote no aparece suelto: vuelve con el lote.
- `POST /api/papelera/restaurar` (usuario o más) `{ "items": [ { "tipo": "lote", "id": 12 }, { "tipo": "comprobante", "id": 40 } ] }` → `{ ok, restaurados, rechazados: [ { tipo, id, error } ] }`. Vuelve todo como estaba: estados, correcciones, aprobaciones y constatación. Un comprobante cuyo documento está en la papelera vuelve con su documento. Se rechaza (cada uno por separado) si su lote también está en la papelera («restaurá primero el lote») o si, mientras tanto, se volvió a subir el mismo archivo y se procesó: no puede haber dos documentos vivos con el mismo archivo («ya hay uno igual cargado después; borrá ese primero»). Si no se restauró nada: `409` (o `404`) con `error` y `rechazados`.
- `DELETE /api/papelera` (superadmin, **solo con sesión**) `{ "confirmacion": "borrar" }` → `{ ok, borrado: { lotes, documentos, comprobantes, archivos } }`: vacía la papelera (lo de sus clientes).
- `POST /api/papelera/borrar` (superadmin, **solo con sesión**) `{ "items": [...], "confirmacion": "borrar" }`: Borrar ya, de a uno o varios.

Una clave de API no puede vaciar ni borrar para siempre (`403`): un error o una integración desbocada se arreglan restaurando.

**Borrado definitivo.** Cada hora, un trabajo de la cola (`purgar_papelera`, cola `mantenimiento`) borra lo que lleva en la papelera más de `papelera.dias` (por defecto 30): las filas (lotes con todo lo suyo, comprobantes con sus correcciones, documentos que se quedaron sin comprobantes y lotes que se quedaron sin documentos) y después los archivos del almacén (el original y el resultado, en el disco y en R2) que ya no usa ningún documento, vivo o en la papelera. Los archivos se direccionan por su SHA-256: el mismo PDF subido en dos lotes es un solo archivo y se borra recién cuando no queda ninguno. La carga y el borrado comparten un candado de Postgres por archivo: no se puede borrar un archivo que se está volviendo a subir. Mientras corre el pase a R2 o la liberación del disco, los archivos esperan a la próxima pasada. Queda en la auditoría como `papelera.purgar`.

## Libro IVA Digital

Las compras de un cliente en un mes con el formato de importación del Libro IVA Digital de ARCA (RG 4597): los archivos `LIBRO_IVA_DIGITAL_COMPRAS_CBTE` (325 posiciones) y `LIBRO_IVA_DIGITAL_COMPRAS_ALICUOTAS` (84). El diseño de registro, sus fuentes y las decisiones en los casos dudosos están en [libro-iva-digital.md](libro-iva-digital.md). Rol **usuario** o más, con el alcance por CUIT (otro cliente, `403`; con un solo cliente asignado se puede omitir).

`GET /api/libro-iva?cliente=CUIT&periodo=AAAA-MM[&solo_aprobados=1][&conciliar=0][&registros=1]`: el resumen y los problemas, sin los archivos. El mes va por fecha de emisión. Lo que está en la papelera no cuenta.

- `solo_aprobados=1`: solo lo que está bien o aprobado y sin advertencias fuertes. Sin él, va todo lo que no bloquea.
- Con Arcanum, concilia el mes con Mis Comprobantes (lo guardado de la conciliación sirve) para **avisar** lo que no figura en ARCA o tiene otros datos; nunca saca nada del archivo. `conciliar=0` no lo pide.
- `registros=1` trae además lo que va al archivo, registro por registro.

```json
{ "ok": true, "cliente": "20222222223", "nombre": "GOMEZ MARIA LAURA", "periodo": "2026-11", "desde": "2026-11-01", "hasta": "2026-11-30",
  "solo_aprobados": false, "leidos": 18, "excluidos": 9, "cantidad_todos": 9, "cantidad_listos": 4,
  "resumen": { "comprobantes": 9, "registros_alicuotas": 4, "notas_credito": 1, "sin_credito_fiscal": 5,
    "por_alicuota": [{ "codigo": "0005", "tasa": "21", "registros": 3, "neto_gravado": 130000.00, "iva": 27300.00 }],
    "neto_gravado": 130000.00, "no_gravado": 0.00, "exento": 0.00, "iva": 27300.00, "credito_fiscal": 27300.00, "otros_tributos": 0.00, "total": 395557.50,
    "moneda_extranjera": [{ "moneda": "DOL", "cantidad": 1, "iva": 210.00, "credito_fiscal": 210.00, "total": 1210.00 }], "por_tipo": { "Factura A": 2 } },
  "problemas": [{ "codigo": "importes_no_cierran", "nivel": "bloquea", "titulo": "Los importes no cierran", "ayuda": "...",
    "items": [{ "id": 7, "tipo": 1, "nombre_tipo": "Factura A", "punto_venta": 5, "numero": 91, "fecha": "2026-11-15", "cuit_emisor": "30711111111",
      "emisor": "DISTRIBUIDORA EJEMPLO S.R.L.", "total": 112100.00, "moneda": "PES", "estado": "error", "detalle": "La suma da 121.000,00 y el total es 112.100,00 (diferencia -8.900,00).", "excluido": true }] }],
  "conciliacion": { "estado": "lista" }, "importar": "Al importar, elegí «Los importes están expresados en la Moneda Original del Comprobante»: ..." }
```

En `resumen`, los importes son en pesos y las notas de crédito restan; la moneda extranjera va aparte, en su moneda. `problemas` viene agrupado por código y ordenado por `nivel`: `bloquea` (no va nunca), `fuerte` (emisor apócrifo, ARCA no lo reconoce, una A de otro comprador: va solo sin `solo_aprobados`), `advertencia` (va) e `info`. `excluido` dice si ese comprobante queda afuera con las opciones pedidas. `conciliacion.estado` es `lista`, `no_pedida` o el estado de la conciliación que no se pudo hacer (`sin_arcanum`, `sin_sincronizar`, `sin_permiso`...), con su `mensaje`. Período mal escrito o cliente que no es de 11 dígitos, `400`.

`GET /api/libro-iva/archivos.zip` con los mismos `cliente`, `periodo` y `solo_aprobados`: un ZIP con `LIBRO_IVA_DIGITAL_COMPRAS_CBTE.txt`, `LIBRO_IVA_DIGITAL_COMPRAS_ALICUOTAS.txt` (ISO 8859-1, CRLF al final de cada registro) y `resumen.csv` (para la persona: totales por alícuota, lo que va y los problemas; `;` y coma decimal). No concilia. Sin nada para exportar, `409` con el mensaje. Cada ZIP queda en la auditoría (`libro_iva.exportar`, con el cliente, el período, la opción y cuántos fueron). Al importar en ARCA, elegí «Los importes están expresados en la Moneda Original del Comprobante».

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
| `conciliar` | `{ cliente?, desde, hasta, grupo?, limite? }`: la conciliación con ARCA; totales completos y hasta `limite` comprobantes por grupo (20 por defecto, 100 como mucho) | lectura |
| `libro_iva_resumen` | `{ cliente?, periodo, solo_aprobados?, limite? }`: el resumen del Libro IVA Digital de compras del mes y los problemas por grupo (hasta `limite` comprobantes por grupo); nunca los archivos | usuario |
| `estado_lote` | `{ id }` | lectura |

Las respuestas son datos JSON, recortados por debajo de 20.000 caracteres. Lo que está en la papelera no aparece en ninguna herramienta, y por MCP no se puede borrar nada.

## Arranque y sesión

- `GET /api/arranque` → `{ ok, necesita, nombre }`. `necesita: true` si todavía no hay usuarios.
- `POST /api/arranque` `{ codigo, email, nombre, clave }` → crea el primer superadmin y abre la sesión. El código de un solo uso se escribe en el log al arrancar (`codigo_arranque`) y lo muestra `factorum salud`. Si ya hay usuarios, `409`.
- `POST /api/sesion` `{ email, clave }` → abre la sesión (cookie). Email sin distinguir mayúsculas. `401` si no coinciden; `403` si la cuenta está desactivada; `429` si hubo demasiados intentos (cinco libres por IP y por email; después, una espera que se duplica hasta 15 minutos).
- `GET /api/sesion` (sesión o clave) → `{ ok, identidad: { tipo, usuario_id, email, nombre, rol, cuits, debe_cambiar, origen, clave }, permisos: { cargar, corregir, usuarios, claves, auditoria, clientes, configuracion, sin_clientes }, instalacion, lockatus, solo_lockatus }` y, con sesión, `cuenta: { origen, tiene_clave }` (de dónde salió la cuenta: `local`, `arcanum` o `lockatus`).
- `DELETE /api/sesion` → cierra la sesión.
- `POST /api/sesion/clave` `{ actual, nueva }` → cambia la contraseña propia (10 caracteres o más) y cierra las otras sesiones. Una cuenta de Lockatus no tiene contraseña acá: `409`.
- `GET /api/arranque` trae además `olvide: true` si hay correo saliente y URL pública (el login muestra «Olvidé mi contraseña»), `lockatus: true` si se puede entrar con Lockatus y `solo_lockatus: true` si solo se puede entrar así; y `GET /api/sesion`, `correo_saliente`.
- Con «Solo se entra con Lockatus» activo (y sin `FACTORUM_LOGIN_LOCAL=1`), `POST /api/sesion`, `POST /api/sesion/olvide`, `POST /api/sesion/link` y `POST /api/sesion/restablecer` dan `403 "El ingreso con email y contraseña está apagado: entrá con Lockatus."`. `POST /api/arranque` (el primer superadmin) no cambia.
- `POST /api/sesion/olvide` `{ email }` → siempre `200 { mensaje }`, exista o no la cuenta (el mail sale aparte; a una cuenta de Lockatus no le sale nada). Manda a `<URL pública>/#restablecer?t=<token>` un link de un solo uso que vence en 30 minutos. Tope: 3 pedidos por email y 10 por IP por hora (`429` el de la IP; el del email no se nota). Sin correo saliente, `409`.
- `POST /api/sesion/link` `{ token }` → `{ tipo: "restablecer|invitacion", email, nombre }` sin usarlo; vencido o usado, `410`.
- `POST /api/sesion/restablecer` `{ token, clave }` → pone la contraseña, cierra todas las sesiones de esa persona, invalida sus otros links y abre una sesión nueva. Vencido o usado, `410`. El token se guarda por su SHA-256 (32 bytes al azar).

## Lockatus: inicio de sesión único

Lockatus (u otro proveedor OpenID Connect) se configura en Configuración, sección `lockatus` (ver la tabla de abajo). Con el emisor, el client ID, el client secret y la URL pública, el login ofrece «Entrar con Lockatus».

- `GET /api/auth/oidc/login` → `302` a la autorización de Lockatus: Authorization Code con PKCE (`S256`), `state` y `nonce`, con los scopes configurados y `redirect_uri=<URL pública>/api/auth/oidc/callback`. El `state`, el `nonce` y el verificador quedan en el servidor (10 minutos, un solo uso) y el navegador lleva el `state` en la cookie `factorum_oidc` (HttpOnly, SameSite=Lax, `Path=/api/auth/oidc/`, 10 minutos). Tope: 30 inicios por IP cada 10 minutos.
- `GET /api/auth/oidc/callback?code&state` → canjea el código (con el `code_verifier` y el client secret, por `client_secret_basic` o `client_secret_post` según el discovery), verifica el `id_token` y, si todo está bien, abre la sesión (`origen: "lockatus"`) y redirige a `/`. Nunca a otra dirección.
- Si algo falla, redirige a `/?lockatus=<motivo>`, que la pantalla traduce: `estado` (el `state` no existe, venció, ya se usó o la cookie no coincide), `token` (el canje o el `id_token` no pasaron), `email` (sin email verificado), `desactivado`, `sin_cuenta` (no existe y la creación automática está apagada), `cancelado` (Lockatus devolvió `error`), `caido` (Lockatus no responde), `no_config`, `demasiados` (cinco fallos libres por IP; después, una espera que se duplica) y `error`.

Verificación del `id_token`: firma `RS256` o `ES256` con las claves del `jwks_uri` del discovery (se guardan y se vuelven a pedir cuando aparece un `kid` nuevo); `iss` igual al `issuer` del discovery (que tiene que ser el emisor configurado); `aud` con el client ID (y `azp` igual al client ID si hay varias audiencias o viene `azp`); `exp` presente y no vencido, `nbf` e `iat` no en el futuro, con un minuto de tolerancia; y `nonce` igual al del pedido.

Quién entra:

1. Se toma el `email` del `id_token` si `email_verified` no es `false`. Sin email verificado, no entra.
2. Si hay una persona con ese email (sin distinguir mayúsculas), entra con **su** rol y **sus** CUITs de Factorum: lo que diga Lockatus no la escala ni la baja. Si está desactivada, no entra.
3. Si no hay, y `lockatus.crear_usuarios` está prendido, se crea con `origen: "lockatus"`, sin contraseña, con el rol del claim (`lockatus.claim_rol`, por defecto `role`; un texto con una o varias palabras o una lista; gana el mayor: `superadmin`, `admin`, `usuario` u `operador`, `lectura` o `lector`) o, si no trae uno conocido, el rol por defecto (`lectura`). `admin` y `superadmin` nacen con todos los clientes; `usuario` y `lectura`, con ninguno (`cuits: []`) hasta que un admin se los asigne.

Una cuenta con `origen: "lockatus"` no tiene contraseña: el login local le contesta igual que a un email que no existe (y tarda lo mismo), no recibe «Olvidé mi contraseña» y `POST /api/usuarios/{id}/restablecer` y `/link` dan `409`. Quien tiene una contraseña temporal sin cambiar y entra con Lockatus no queda trabado.

Salida de emergencia: con `FACTORUM_LOGIN_LOCAL=1` en el entorno (no se puede poner desde la pantalla), el login con contraseña vuelve aunque «Solo se entra con Lockatus» esté activo. Las claves de API y el pase de la suite no dependen de nada de esto.

## Usuarios (admin, con sesión)

- `GET /api/usuarios` → `{ usuarios: [ { id, email, nombre, rol, cuits, tiene_clave, debe_cambiar, activo, origen, creado, ultimo_ingreso } ] }`.
- `POST /api/usuarios` `{ email, nombre, rol, cuits }` → `201 { usuario, clave_temporal }`. La contraseña temporal se devuelve **una sola vez** y obliga a cambiarla al entrar.
- `PATCH /api/usuarios/{id}` `{ nombre?, rol?, cuits?, sin_clientes?, activo? }`. `sin_clientes: true` le saca todos los clientes (`cuits: []`, no ve nada). Desactivar o cambiar rol o CUITs cierra sus sesiones.
- `POST /api/usuarios` con `lockatus: true` (o cualquier alta con «Solo se entra con Lockatus») → `201 { usuario, lockatus: true }`: sin contraseña ni invitación; entra con Lockatus con ese email, con el rol y los CUITs que se le dieron. Sin Lockatus configurado, `409`. `sin_clientes: true` también vale en el alta.
- `POST /api/usuarios/{id}/restablecer` → `{ usuario, clave_temporal }` (una sola vez); cierra sus sesiones.
- `POST /api/usuarios` con `invitar: true` → `201 { usuario, invitado }`: sin contraseña temporal; le llega un mail con un link para elegir la suya (vence en 72 horas). Si el mail no sale, la persona queda creada y viene `aviso`. Sin correo saliente o sin URL pública, `409`.
- `POST /api/usuarios/{id}/link` → `{ tipo }`: le manda por mail la invitación otra vez (si todavía no eligió contraseña) o un link para restablecerla (30 minutos).

Reglas: nadie da un rol mayor al suyo ni CUITs que no tiene; un admin no toca superadmins; siempre queda un superadmin activo (`409`); nadie se desactiva a sí mismo. `cuits` acepta una lista o un texto separado por comas; vacío o `null` = todos (para «ninguno», `sin_clientes: true`). En la lista, `cuits: []` es «ninguno todavía» y `origen` dice si la cuenta es `local`, `arcanum` o `lockatus`.

## Claves de API (admin, con sesión)

- `GET /api/claves` → `{ claves: [ { id, nombre, prefijo, rol, cuits, usuario_id, dueno, creado, vence, ultimo_uso, revocada, suite, suite_tope } ] }`. Nunca la clave.
- `POST /api/claves` `{ nombre, rol, cuits, vence: "AAAA-MM-DD"|"", suite, suite_tope }` → `201 { clave, datos }`. La clave (`fct_...`) se devuelve **una sola vez**; se guarda su SHA-256. Vence al terminar ese día (hora argentina). El tope de la suite es `usuario` por defecto; `admin` o `superadmin` solo los pone un superadmin.
- `DELETE /api/claves/{id}` → la revoca (queda en la lista como revocada).

## Aviso de la IA externa

Una IA (visión) externa no se usa hasta que un superadmin acepta su aviso para ese host y esa versión del texto, y cada persona lo ve la primera vez que la va a usar. La local no tiene aviso.

- `GET /api/ia/aviso` (lectura o más) → `{ configurada, proveedor, nombre, host, region, region_nombre, local, externa, aceptado, en_uso, version, persona, visto, puede_aceptar, aceptacion, aviso }`. `proveedor` es `local`, `openai`, `anthropic`, `google`, `openrouter` o `desconocido` (sale del host); `eleccion` y `eleccion_nombre`, el proveedor elegido en Configuración; `modelo`, el modelo; `recomendados` y `prueba`, los modelos que probó Factorum; `region`, el código de la región de OpenAI si el host es regional (`eu`, `us`...). `aceptado`: hay una aceptación vigente para este host y esta versión (la local siempre). `en_uso`: está configurada y es local o aceptada. `persona` y `visto`: si quien pide es una persona y si ya vio este aviso. `aceptacion` (admin o más, si es externa y aceptada): `{ host, version, por, cuando }`. `aviso` es el contenido para pintar:

```json
{
  "version": 1, "fecha": "2026-11-27", "titulo": "OpenAI", "sello": "IA externa: OpenAI", "store_false": true,
  "intro": "Cuando el QR, el texto del PDF y el OCR no alcanzan...",
  "alerta": { "nivel": "mal", "titulo": "...", "texto": "..." },
  "tarjetas": [ { "id": "donde", "titulo": "A dónde viajan", "destacado": "Fuera de tu servidor", "tono": "aviso", "puntos": ["..."], "notas": ["..."] } ],
  "fuentes": [ { "titulo": "OpenAI: Your data", "url": "https://developers.openai.com/api/docs/guides/your-data", "fecha": "2026-11-27" } ],
  "pie": "Datos tomados de las páginas oficiales de cada proveedor el 27 de noviembre de 2026. Pueden cambiar..."
}
```

  Las tarjetas son cuatro, en orden: `donde`, `tiempo`, `entrenan` y `factorum`. Los textos pueden traer `**negrita**`. `alerta` solo viene con Google Gemini (plan gratuito) y con un proveedor desconocido.
- `POST /api/ia/modelos` (superadmin, con sesión) `{ proveedor, url, clave }` → `{ exito, proveedor, url, clave_guardada, local, sello, recomendados, modelos | motivo, mensaje }`. Le pide la lista de modelos al proveedor con lo que está en pantalla, sin guardar nada. `proveedor`: `ollama`, `openai`, `anthropic`, `gemini`, `openrouter` u `otro`; `url` vacía es la de siempre del proveedor (en OpenAI, también los dominios regionales; en Ollama y `otro`, cualquiera). Si `clave` viene vacía usa la guardada, solo si es para el mismo host (o una región de OpenAI): una clave nunca viaja a otro host. A Ollama no se le manda clave y OpenRouter lista sin clave. Cada modelo: `{ id, nombre, grupo, detalle, imagenes, nota, recomendado }`; `imagenes: false` (solo Ollama, que lo dice en `capabilities`) es uno que no lee imágenes. De OpenAI se sacan los de audio, voz, imágenes, embeddings, moderación, búsqueda y los que no andan con `/chat/completions` (pro, codex). `recomendado` (y `recomendados`, los del proveedor): `{ proveedor, modelo, campos, total, segundos, porcentaje, tiempo, corto, texto, nota }`, de la prueba de Factorum. Si falla, `exito: false` con `motivo` (`sin_clave`, `clave`, `red`, `vacia` o `respuesta`) y `mensaje`. La clave nunca vuelve ni se loguea.
- `POST /api/ia/aviso/visto` (persona, con sesión) `{ host, version }` → la misma vista con `visto: true`. Si el host o la versión no son los de la IA en uso, `409` con el aviso nuevo. Queda en la auditoría (`ia.aviso_visto`).
- La aceptación del superadmin va con el guardado de la configuración: `PUT /api/configuracion` con `aceptar_aviso_ia: { host, version }`. Guardar una IA externa nueva (o de otro host) sin aceptar responde `409` con `codigo: "aviso_ia"` y `aviso_ia` (la vista de la IA que se quería guardar) y no guarda nada. Para aceptar la que fija el entorno, `{ "cambios": {}, "aceptar_aviso_ia": { ... } }`. Solo un superadmin con sesión (`403` si no). Queda en la auditoría (`ia.aviso_aceptado`).
- Verificar o crear un lote con `ia=true` desde la sesión de una persona que no vio el aviso responde `409` (`codigo: "aviso_ia"`). Las claves de API, los chats y MCP no son personas: les alcanza con la aceptación del superadmin. Con una IA externa sin aceptar, nadie la usa.

## Configuración (con sesión)

- `GET /api/configuracion` (admin) → `{ campos, secciones, canales, url_publica, webhook_entrada }`. Un superadmin ve todas las secciones (`general`, `arcanum`, `lockatus`, `telegram`, `whatsapp`, `webhook`, `correo_entrada`, `correo_salida`, `avisos`, `ia`, `r2`, `copias`, `papelera`, `clientes`) y además `correo: { urls, estado, smtp, google: { conectado, cuenta, fijado } }` (las URLs para cada servicio, sin tokens) y `lockatus: { redirect, listo, solo, login_local, sesion_lockatus }` (la Redirect URI para registrar en Lockatus, si se puede entrar, si solo se entra así, si está la salida de emergencia y si la sesión de quien mira se abrió con Lockatus) e `ia_aviso` (como `GET /api/ia/aviso`) e `ia: { proveedores, recomendados, prueba, proveedor, region }` (los proveedores con su URL, si piden clave y dónde se saca; el proveedor elegido o, si no se eligió nunca, el que sale de la URL); un admin, solo `clientes`. Al guardar, el proveedor y la URL tienen que ir juntos (`400` si no) y, si cambia el host sin una clave nueva o se elige Ollama, la clave guardada se borra. Los campos de tipo `opcion` traen `opciones: [ { valor, etiqueta } ]` y los `entero`, `min` y `max`. Cada campo: `{ clave, seccion, etiqueta, tipo, env, ayuda, ejemplo, valor, configurado, ultimos, bloqueado, origen, actualizado_por, actualizado }`. Un secreto nunca trae `valor`: solo `configurado` y, si es largo, `ultimos` (los últimos 4 caracteres).
- `PUT /api/configuracion` `{ cambios: { "telegram.token": "...", "telegram.permitidos": ["1","2"], "general.url_publica": null } }` → la vista nueva más `cambiadas`. `null` borra lo guardado (vuelve al entorno o al valor de fábrica); un secreto `""` no se toca. Un campo fijado por una variable de entorno da `409`; un valor inválido, `400`. Al guardar se reconfiguran sin reiniciar Arcanum, Lockatus, el modelo de visión, el almacén y los canales. Prender `lockatus.solo` desde una sesión que no se abrió con Lockatus da `409` (para no quedar afuera). Guardar una IA externa sin `aceptar_aviso_ia` da `409` (ver [Aviso de la IA](#aviso-de-la-ia-externa)).
- `POST /api/configuracion/probar/{seccion}` (superadmin) `{ cambios }` → `{ exito, mensaje }`. Prueba con lo que está en pantalla, sin guardar: `general` (la URL pública responde), `arcanum` (`/api/health` y `/api/tenants`), `lockatus` (lee el discovery del emisor, controla que tenga los endpoints de autorización, token y claves, PKCE S256 y RS256 o ES256, y lee el JWKS), `telegram` (`getMe`), `webhook` (un aviso firmado a la URL de salida), `ia` (le manda el ticket de prueba al modelo y devuelve además `segundos`, `modelo` y `leido: [ { campo, valor, esperado, bien } ]`; si es una IA externa sin el aviso aceptado para ese host, `409` con `codigo: aviso_ia` y el aviso, hasta que el pedido traiga `aceptar_aviso_ia: { host, version }`, que no deja nada aceptado y queda en la auditoría como `ia.aviso_aceptado_prueba`), `r2` (pone y borra un objeto), `correo_entrada` (IMAP: entra y cuenta los mensajes de la carpeta; Gmail: la cuenta y la etiqueta; Cloudflare y proveedores: que la URL pública responda y el secreto esté) y `correo_salida` (con `destino` en el cuerpo, manda un mail de prueba; sin él, solo entra).

| Clave | Variable | Tipo |
|---|---|---|
| `general.nombre`, `general.url_publica` | `FACTORUM_NOMBRE`, `FACTORUM_URL_PUBLICA` | texto, URL |
| `general.ia_por_defecto`, `general.constatar_por_defecto` | `FACTORUM_IA_POR_DEFECTO`, `FACTORUM_CONSTATAR_POR_DEFECTO` | sí/no |
| `arcanum.url`, `arcanum.url_publica`, `arcanum.api_key`, `arcanum.cuit_consultante`, `arcanum.suite_crear_usuarios` | `ARCANUM_URL`, `ARCANUM_URL_PUBLICA`, `ARCANUM_API_KEY`, `ARCANUM_CUIT_CONSULTANTE`, `FACTORUM_SUITE_CREAR_USUARIOS` | URL, URL, secreto, CUIT, sí/no |
| `lockatus.emisor`, `lockatus.client_id`, `lockatus.client_secret`, `lockatus.scope`, `lockatus.claim_rol`, `lockatus.rol_por_defecto`, `lockatus.crear_usuarios`, `lockatus.solo` | `FACTORUM_OIDC_ISSUER`, `FACTORUM_OIDC_CLIENT_ID`, `FACTORUM_OIDC_CLIENT_SECRET`, `FACTORUM_OIDC_SCOPE`, `FACTORUM_OIDC_ROLE_CLAIM`, `FACTORUM_OIDC_DEFAULT_ROLE`, `FACTORUM_OIDC_CREAR_USUARIOS`, `FACTORUM_OIDC_SOLO` | URL, texto, secreto, texto (`openid profile email`), texto (`role`; con puntos, anidado), opción (`lectura`, `usuario`, `admin`, `superadmin`; por defecto `lectura`), sí/no (sí), sí/no (no) |
| `telegram.token`, `telegram.permitidos` | `FACTORUM_TELEGRAM_TOKEN`, `FACTORUM_TELEGRAM_PERMITIDOS` | secreto, lista |
| `whatsapp.activo`, `whatsapp.permitidos` | `FACTORUM_WHATSAPP`, `FACTORUM_WHATSAPP_PERMITIDOS` | sí/no, lista |
| `webhook.token`, `webhook.salida_url` | `FACTORUM_WEBHOOK_TOKEN`, `FACTORUM_WEBHOOK_SALIDA_URL` | secreto, URL |
| `ia.url`, `ia.modelo`, `ia.api_key` | `LLM_API_BASE_URL`, `LLM_MODEL`, `LLM_API_KEY` | URL, texto, secreto |
| `r2.endpoint`, `r2.bucket`, `r2.access_key`, `r2.secret_key`, `r2.prefijo` | `FACTORUM_R2_*` | URL, texto, secreto, secreto, texto |
| `copias.activo`, `copias.hora`, `copias.diarias`, `copias.mensuales` | `FACTORUM_COPIAS`, `FACTORUM_COPIAS_HORA`, `FACTORUM_COPIAS_DIARIAS`, `FACTORUM_COPIAS_MENSUALES` | sí/no (por defecto sí), hora `HH:MM` de Buenos Aires (03:30), entero 1 a 366 (14), entero 0 a 120 (12) |
| `papelera.dias` | `FACTORUM_PAPELERA_DIAS` | entero 1 a 365 (30): días que queda lo borrado antes del borrado definitivo |
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

## Pasar lo del disco a R2 (superadmin, con sesión)

Con R2 activo, lo nuevo va a R2 y lo que quedó en el disco se sigue leyendo de ahí. El pase sube cada objeto tal cual (ya está comprimido y cifrado), lo verifica (tamaño y SHA-256 de lo que quedó en R2) y nunca borra nada del disco por sí solo. Mientras dura y después, cada objeto se busca primero en R2 y, si no está, en el disco.

- `GET /api/r2/pase` → `{ pase }`:
  `{ estado, por, iniciada, terminada, actualizado, mensaje, destino, total, hechos, subidos, pendientes, errores, liberados, para_liberar, estimado_seg, trabajando, r2_activo, puede_liberar, ultimos_errores }`.
  Cada cuenta es `{ objetos, bytes }`. `estado`: `""` (nunca se empezó), `corriendo`, `pausada`, `terminada`, `liberando`, `liberada` o `error` (con el motivo en `mensaje`). `estimado_seg` es el tiempo que falta al ritmo actual. `ultimos_errores`: `[ { clave, error, intentos } ]`.
- `POST /api/r2/pase` `{ accion }` → `{ pase }`:
  - `iniciar`: empieza (o vuelve a revisar el disco: lo verificado no se sube de nuevo).
  - `pausar`: termina los objetos que está pasando y se detiene.
  - `seguir`: sigue lo pausado o frenado y reintenta lo que falló.
  - `liberar` con `{ confirmacion: "liberar" }`: borra del disco solo lo verificado, volviendo a mirar en R2 que siga ahí. Solo con el pase terminado.

  Sin R2 activo o en un estado que no corresponde, `409` con el motivo. Es un trabajo en segundo plano (cola `mantenimiento` de River): sigue solo si se reinicia el servidor. Cada acción queda en la auditoría (`r2.migrar_inicio`, `r2.migrar_pausa`, `r2.migrar_seguir`, `r2.migrar_fin`, `r2.liberar_inicio`, `r2.liberar_fin`).

Por consola: `factorum r2 migrar` (Ctrl+C pausa; volver a correrlo sigue), `factorum r2 migrar --liberar` y `factorum r2 estado`.

## Copias de seguridad de la base (superadmin, con sesión)

- `GET /api/copias` → `{ copias, activas, hora, diarias, mensuales, destino, proxima }`. Cada copia: `{ id, creado, terminado, estado (haciendo, ok, error), manual, programada, por, destino (r2 o disco), clave, tamano, filas, tablas, sha256, version, error, duracion_ms, disponible }`.
- `POST /api/copias` → `202`: hace una copia ahora, en segundo plano.
- `GET /api/copias/{id}/archivo` → el archivo cifrado (`application/octet-stream`, con `X-Factorum-SHA256`). Queda en la auditoría.
- `GET /api/panel`, para un superadmin, suma `copia_fallida: { cuando, error }` si la última copia falló.

La copia es lógica: el contenido de cada tabla (`COPY`) dentro de una transacción `REPEATABLE READ`, en un tar con un `manifiesto.json` (versión de Factorum y del esquema, columnas, filas, secuencias y SHA-256 de cada tabla), comprimido con zstd y cifrado con `FACTORUM_MASTER_KEY` (AES-256-GCM en tramos de 1 MiB). Va a `copias/factorum-AAAAMMDD-HHMMSS.copia` en R2 (bajo el prefijo) o en `FACTORUM_DATOS/copias`. Restaurar es solo por consola: `factorum restaurar <archivo o clave> [--confirmar]` (ver el README).

Para avisar por otro medio cuando una copia falla, `copias.Servicio.AlTerminar(func(ctx, copias.Copia))` se llama después de cada copia, bien o mal.

## Auditoría (admin, con sesión)

`GET /api/auditoria?desde=AAAA-MM-DD&hasta=AAAA-MM-DD&persona=texto&accion=sesion.&limite=50&offset=0` → `{ eventos: [ { id, creado, actor, actor_tipo, usuario_id, accion, objeto, detalle, ip } ], total, acciones }`. Las fechas van en hora argentina; `accion` terminada en punto filtra por grupo.

Acciones: `sesion.entrar`, `sesion.fallida`, `sesion.salir`, `sesion.entrar_lockatus` (con `cuenta_creada` y `rol`), `sesion.rechazo_lockatus` (con el `motivo`: state inválido, sin email verificado, desactivada, sin cuenta, la firma, el emisor, la audiencia, el vencimiento o el nonce del `id_token`...), `usuario.crear`, `usuario.editar`, `usuario.desactivar`, `usuario.reactivar`, `usuario.restablecer`, `usuario.cambiar_clave`, `clave.crear`, `clave.revocar`, `config.cambiar` (qué claves, nunca los valores), `config.probar_ia` (proveedor, host, modelo y si leyó bien), `ia.aviso_aceptado_prueba`, `config.whatsapp_desvincular`, `lote.crear`, `comprobante.corregir`, `comprobante.aprobar`, `exportar`, `suite.pase_emitido`, `suite.pase_usado`, `suite.pase_rechazado`, `suite.abrir`, `suite.abrir_fallido`, `correo.recibido`, `correo.retenido`, `correo.ignorado` (sin adjuntos útiles, muy grande o ilegible), `correo.aceptar`, `correo.descartar`, `config.google_conectar`, `config.google_desconectar`, `usuario.pedir_restablecer`, `usuario.restablecer_por_mail`, `usuario.aceptar_invitacion`, `usuario.link_por_mail`, `r2.migrar_inicio`, `r2.migrar_pausa`, `r2.migrar_seguir`, `r2.migrar_fin`, `r2.liberar_inicio`, `r2.liberar_fin`, `copia.pedir`, `copia.ok`, `copia.error`, `copia.descargar`, `copia.restaurar`, `lote.borrar` (con nombre, cliente, documentos, comprobantes y si estaba en proceso), `comprobante.borrar` (con los ids), `lote.cambiar_cliente` (antes y después), `papelera.restaurar`, `papelera.vaciar`, `papelera.borrar_ya` y `papelera.purgar` (el automático, actor Factorum). Del correo, quién lo mandó, la forma y cuántos adjuntos; nunca el texto ni el asunto. Nunca lleva contraseñas, claves, tokens ni pases.

## Suite Escriba: el pase entre apps

El contrato completo está en [suite-pase.md](suite-pase.md).

- `POST /api/suite/pase` (solo clave de API habilitada para la suite; nunca una sesión) `{ app: "arcanum", email, nombre, rol, cuits }` → `{ ok, url, vence }`. `url` es `<URL pública>/suite/entrar?pase=<token>`; el pase sirve una vez y vence a los 60 segundos. Errores: `403 "Esta clave no está habilitada para abrir sesiones de la suite"`, `409 "Falta la URL pública de Factorum"`, `403` si la persona está desactivada, si no tiene cuenta y la creación automática está apagada, o si no queda rol o CUIT en común.
- `GET /suite/entrar?pase=...` → abre la sesión y redirige a `/`. Con un pase vencido o usado, redirige a `/?aviso=pase`.
- `GET /api/suite/apps` (sesión) → `{ ok, apps: [ { app: "arcanum", nombre: "Arcanum", disponible } ] }`. `disponible` = respondió su healthz en los últimos 60 segundos.
- `POST /api/suite/abrir/arcanum` (sesión) → `{ ok, url }`: pide el pase a Arcanum con la clave guardada en Configuración. Los errores de Arcanum vuelven con su mensaje.
