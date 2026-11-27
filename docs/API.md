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

## Usuarios (admin, con sesión)

- `GET /api/usuarios` → `{ usuarios: [ { id, email, nombre, rol, cuits, tiene_clave, debe_cambiar, activo, origen, creado, ultimo_ingreso } ] }`.
- `POST /api/usuarios` `{ email, nombre, rol, cuits }` → `201 { usuario, clave_temporal }`. La contraseña temporal se devuelve **una sola vez** y obliga a cambiarla al entrar.
- `PATCH /api/usuarios/{id}` `{ nombre?, rol?, cuits?, activo? }`. Desactivar o cambiar rol o CUITs cierra sus sesiones.
- `POST /api/usuarios/{id}/restablecer` → `{ usuario, clave_temporal }` (una sola vez); cierra sus sesiones.

Reglas: nadie da un rol mayor al suyo ni CUITs que no tiene; un admin no toca superadmins; siempre queda un superadmin activo (`409`); nadie se desactiva a sí mismo. `cuits` acepta una lista o un texto separado por comas; vacío o `null` = todos.

## Claves de API (admin, con sesión)

- `GET /api/claves` → `{ claves: [ { id, nombre, prefijo, rol, cuits, usuario_id, dueno, creado, vence, ultimo_uso, revocada, suite, suite_tope } ] }`. Nunca la clave.
- `POST /api/claves` `{ nombre, rol, cuits, vence: "AAAA-MM-DD"|"", suite, suite_tope }` → `201 { clave, datos }`. La clave (`fct_...`) se devuelve **una sola vez**; se guarda su SHA-256. Vence al terminar ese día (hora argentina). El tope de la suite es `usuario` por defecto; `admin` o `superadmin` solo los pone un superadmin.
- `DELETE /api/claves/{id}` → la revoca (queda en la lista como revocada).

## Configuración (con sesión)

- `GET /api/configuracion` (admin) → `{ campos, secciones, canales, url_publica, webhook_entrada }`. Un superadmin ve todas las secciones (`general`, `arcanum`, `telegram`, `whatsapp`, `webhook`, `ia`, `r2`, `copias`, `clientes`); un admin, solo `clientes`. Cada campo: `{ clave, seccion, etiqueta, tipo, env, ayuda, ejemplo, valor, configurado, ultimos, bloqueado, origen, actualizado_por, actualizado }`. Un secreto nunca trae `valor`: solo `configurado` y, si es largo, `ultimos` (los últimos 4 caracteres).
- `PUT /api/configuracion` `{ cambios: { "telegram.token": "...", "telegram.permitidos": ["1","2"], "general.url_publica": null } }` → la vista nueva más `cambiadas`. `null` borra lo guardado (vuelve al entorno o al valor de fábrica); un secreto `""` no se toca. Un campo fijado por una variable de entorno da `409`; un valor inválido, `400`. Al guardar se reconfiguran sin reiniciar Arcanum, el modelo de visión, el almacén y los canales.
- `POST /api/configuracion/probar/{seccion}` (superadmin) `{ cambios }` → `{ exito, mensaje }`. Prueba con lo que está en pantalla, sin guardar: `general` (la URL pública responde), `arcanum` (`/api/health` y `/api/tenants`), `telegram` (`getMe`), `webhook` (un aviso firmado a la URL de salida), `ia` (`/models` y que esté el modelo), `r2` (pone y borra un objeto).

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
| `copias.activo`, `copias.hora`, `copias.diarias`, `copias.mensuales` | `FACTORUM_COPIAS`, `FACTORUM_COPIAS_HORA`, `FACTORUM_COPIAS_DIARIAS`, `FACTORUM_COPIAS_MENSUALES` | sí/no (por defecto sí), hora `HH:MM` de Buenos Aires (03:30), entero 1 a 366 (14), entero 0 a 120 (12) |
| `clientes.canales` | `FACTORUM_CANAL_CLIENTES` | remitente → CUIT (`{ "5491155550000": "20222222223" }` o `"5491155550000 = 20-22222222-3"` por renglón) |

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

Acciones: `sesion.entrar`, `sesion.fallida`, `sesion.salir`, `usuario.crear`, `usuario.editar`, `usuario.desactivar`, `usuario.reactivar`, `usuario.restablecer`, `usuario.cambiar_clave`, `clave.crear`, `clave.revocar`, `config.cambiar` (qué claves, nunca los valores), `config.whatsapp_desvincular`, `lote.crear`, `comprobante.corregir`, `comprobante.aprobar`, `exportar`, `suite.pase_emitido`, `suite.pase_usado`, `suite.pase_rechazado`, `suite.abrir`, `suite.abrir_fallido`, `r2.migrar_inicio`, `r2.migrar_pausa`, `r2.migrar_seguir`, `r2.migrar_fin`, `r2.liberar_inicio`, `r2.liberar_fin`, `copia.pedir`, `copia.ok`, `copia.error`, `copia.descargar`, `copia.restaurar`. Nunca lleva contraseñas, claves, tokens ni pases.

## Suite Escriba: el pase entre apps

El contrato completo está en [suite-pase.md](suite-pase.md).

- `POST /api/suite/pase` (solo clave de API habilitada para la suite; nunca una sesión) `{ app: "arcanum", email, nombre, rol, cuits }` → `{ ok, url, vence }`. `url` es `<URL pública>/suite/entrar?pase=<token>`; el pase sirve una vez y vence a los 60 segundos. Errores: `403 "Esta clave no está habilitada para abrir sesiones de la suite"`, `409 "Falta la URL pública de Factorum"`, `403` si la persona está desactivada, si no tiene cuenta y la creación automática está apagada, o si no queda rol o CUIT en común.
- `GET /suite/entrar?pase=...` → abre la sesión y redirige a `/`. Con un pase vencido o usado, redirige a `/?aviso=pase`.
- `GET /api/suite/apps` (sesión) → `{ ok, apps: [ { app: "arcanum", nombre: "Arcanum", disponible } ] }`. `disponible` = respondió su healthz en los últimos 60 segundos.
- `POST /api/suite/abrir/arcanum` (sesión) → `{ ok, url }`: pide el pase a Arcanum con la clave guardada en Configuración. Los errores de Arcanum vuelven con su mensaje.
