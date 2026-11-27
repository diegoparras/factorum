<div align="center">

# Factorum

**Lee cientos o miles de facturas de ARCA por minuto, sin depender de una IA.**

Self-hosted · Sin LLM obligatorio · Cada dato dice de dónde salió · Parte de la suite **Escriba**

</div>

---

## El problema

El cliente manda los PDF sueltos, por mail o por WhatsApp. Hay que sacar fecha, tipo, número, CUIT, razón social, neto, IVA por alícuota, otros tributos y total, y controlar que todo cierre.

Pasárselos a un chat con IA tiene tres problemas:

- Los datos del cliente salen de tu compu.
- El mismo pedido no da siempre lo mismo.
- Si un inspector pregunta de dónde salió un importe, no hay respuesta.

Esto ya lo explica la presentación [Facturas e IA](https://github.com/diegoparras/contadorfacturas) (tres caminos: Claude, IA local y código).

## La solución

Factorum hace el "camino C" de esa presentación (código, no IA) a escala. A la IA la deja como último recurso, opcional y local.

```
 Entradas                       Factorum (Go)                          Salidas
┌───────────────┐    ┌──────────────────────────────────┐    ┌──────────────────────┐
│ Web (PDF/ZIP) │    │ Cola en Postgres (River)         │    │ Revisión en pantalla │
│ Correo        │    │                                  │    │ Respuesta por mail   │
│ WhatsApp      │───▶│   1. QR de ARCA (dato oficial)   │───▶│ Excel / CSV          │
│ Telegram      │    │   2. Texto del PDF               │    │ Resumen por alícuota │
│ Webhook       │    │   3. OCR (Tesseract)             │    │ API REST             │
│ MCP / Arcanum │    │   4. Visión local (Ollama), opc. │    │ MCP                  │
└───────────────┘    │   Controles + WSCDC (Arcanum)    │    │ Avisos por el chat   │
                     └──────────────────────────────────┘    └──────────────────────┘
```

### 1. El QR manda

Toda factura electrónica trae un QR (RG 4892) con CUIT, tipo, punto de venta, número, fecha, total y CAE. Factorum lo lee de la imagen embebida en el PDF, sin rasterizar, y si hace falta de la página entera o de una foto.

Si el texto del PDF dice otra cosa que el QR, avisa: el PDF puede estar **adulterado**.

### 2. La cascada se corta cuando alcanza

Cada paso corre solo si el anterior no completó los datos:

1. QR
2. Texto
3. OCR
4. Modelo de visión

Una factura de "Comprobantes en línea" se resuelve con los dos primeros en unos 15 ms. El modelo de visión es opcional:

- Si está prendido, solo completa campos vacíos, nunca pisa al QR, y todo lo que aporta queda para que lo revise una persona.
- Si no es local, Factorum lo avisa al arrancar.

### 3. Controles de contador

- Neto + IVA + otros = total.
- IVA contra el neto (con una sola alícuota).
- Dígito verificador del CUIT.
- Letra contra tipo.
- CAE presente y no vencido a la emisión.
- Fechas.
- Moneda extranjera.
- Duplicados: mismo emisor, tipo, punto de venta y número.
- Archivos repetidos: mismo SHA-256.

### 4. ¿Es legal?

Con [Arcanum](https://github.com/diegoparras/arcanum), cada comprobante se constata con **WSCDC** y el emisor se busca en la **base de apócrifos**.

| Veredicto | Qué significa |
|---|---|
| `valida` | ARCA confirma el comprobante con esos datos |
| `invalida` | ARCA lo rechaza |
| `apocrifa` | El emisor figura en la base de apócrifos |
| `tique_controlado` | Un ticket de controlador fiscal: el CUIT es válido y el emisor no es apócrifo |
| `con_errores` | Hay errores locales, aunque ARCA lo confirme |
| `no_verificable` | No hay Arcanum, faltan datos o ARCA no respondió |

### 5. Papel de trabajo

Cada dato guarda su origen: `qr`, `texto`, `ocr`, `vision`, `manual` o `arcanum`. Cada corrección queda registrada con el valor de antes, el de después, quién y cuándo. Una persona aprueba.

## Tickets de compra

Además de facturas, Factorum lee los **tickets de controlador fiscal**: el tique a consumidor final y el tique factura A, B o C del supermercado, la estación de servicio, la farmacia o el bar. Casi siempre llegan como foto del celular.

- **Qué lee:** comercio y razón social, CUIT, condición frente al IVA, tipo (tique o tique factura y su letra), punto de venta y número, fecha (también con el año en dos dígitos) y hora, neto, IVA por alícuota, IVA contenido, subtotal y total. El pago, el vuelto y el "recibimos" no se toman como total.
- **Fotos:** antes del OCR, la foto pasa a grises, se agranda si es angosta, se empareja la luz, se estira el contraste y, si hace falta, se binariza con Otsu. Tesseract la lee con dos segmentaciones de página y gana la lectura con más datos. El parser tolera el ruido típico (O por 0, espacios dentro de los números, "$" mal leído, "T O T A L") y nunca completa un dato que no está escrito.
- **¿Es legal?** Los tiques **no se constatan en ARCA**: WSCDC no los cubre. Factorum controla el dígito verificador del CUIT y busca al emisor en la **base de apócrifos**. Si todo da bien, el veredicto es `tique_controlado`. No se pide CAE.
- **Rubros:** cada ticket lleva un rubro de gasto: `supermercado`, `combustible`, `farmacia`, `gastronomia`, `transporte`, `servicios` u `otros`. Sale de las palabras clave del comercio (YPF, SHELL, AXION o "estación de servicio" es combustible), de la actividad principal del emisor en el padrón de ARCA (le gana a las palabras clave, y también les pone rubro a las facturas) o de la corrección de una persona, que se recuerda por CUIT y le gana a todo.

Con el filtro `clase=tickets` se ven solo los tickets; el resumen trae cuántos hay, cuánto suman y el total por rubro.

## Velocidad

Medido en 4 núcleos, sin IA:

| Qué | Tiempo |
|---|---|
| Un PDF de ARCA (QR + texto + controles) | 15 ms |
| 1.000 facturas distintas en un ZIP, de punta a punta (subida, cola, Postgres, almacén cifrado) | 27 s |
| Excel de 1.000 filas | 0,2 s |

El cuello de botella posible es el modelo de visión (segundos por página), y por eso es el último recurso.

## Instalación

Requisitos: Docker.

```bash
cp .env.example .env   # completá FACTORUM_DB_PASSWORD y FACTORUM_MASTER_KEY
docker compose up -d
```

### Primera vez: el primer superadmin

Sin usuarios, Factorum genera un **código de un solo uso** y lo escribe en el log:

```bash
docker compose logs factorum | grep codigo_arranque
# o, en el contenedor:
docker compose exec factorum factorum salud
```

Entrá a **http://localhost:8095**: la pantalla muestra «Crear el primer superadmin» y pide el código, tu nombre, tu email y una contraseña (10 caracteres o más). Después, desde **Configuración**, completá la URL pública, Arcanum, los canales y el resto; desde **Usuarios**, sumá a las demás personas.

El código vale hasta que se crea el primer superadmin; si reiniciás antes, se genera otro.

Con un modelo de visión local:

```bash
docker compose --profile ia up -d
docker compose exec ollama ollama pull qwen2.5vl:7b
# en .env: LLM_API_BASE_URL=http://ollama:11434/v1 y LLM_MODEL=qwen2.5vl:7b
```

Como **módulo de Arcanum**, usá el `docker-compose.factorum.yml` del repo de Arcanum (ver su DESPLIEGUE.md). Arcanum suma la pestaña "Facturas recibidas", las herramientas MCP y la constatación.

### Sin servidor: por consola

```bash
factorum procesar -xlsx compras.xlsx carpeta-con-pdfs/ facturas.zip
```

No necesita base de datos. Usa todos los núcleos.

## Configuración

Hacen falta solo tres variables de entorno; todo lo demás se configura desde la pantalla **Configuración** (superadmin), con un botón **Probar** por sección y el paso a paso de cada una. Al guardar, se aplica sin reiniciar (Arcanum, el modelo de visión, el almacén, Telegram, WhatsApp, el webhook y el correo).

| Variable | Para qué |
|---|---|
| `FACTORUM_DATABASE_URL` | Postgres (obligatoria) |
| `FACTORUM_MASTER_KEY` | Obligatoria. Cifra los archivos y la configuración secreta (AES-256-GCM). Guardala fuera del servidor |
| `FACTORUM_PUERTO` | Puerto (por defecto 8095) |

Si una variable de configuración está puesta, **gana**: en la pantalla el campo aparece bloqueado con «fijado por el servidor (variable X)». Las que se pueden poner son estas (ver [.env.example](.env.example)):

| Variable | Para qué |
|---|---|
| `FACTORUM_URL_PUBLICA`, `FACTORUM_NOMBRE` | La dirección que ve el navegador (cookies seguras y suite) y el nombre de la instalación |
| `FACTORUM_API_KEY` / `FACTORUM_API_KEY_LECTURA` | Claves del entorno: de rol usuario (la que usa Arcanum) y de lectura. Las demás claves se crean en **Claves de API** |
| `FACTORUM_API_KEY_SUITE_TOPE` | Habilita `FACTORUM_API_KEY` para abrir sesiones desde Arcanum, con ese rol tope (`lectura`, `usuario`, `admin` o `superadmin`) |
| `FACTORUM_SUITE_CREAR_USUARIOS` | Crear la cuenta de quien llega desde otra app de la suite (por defecto, sí) |
| `FACTORUM_IA_POR_DEFECTO`, `FACTORUM_CONSTATAR_POR_DEFECTO` | Lo que viene marcado al cargar |
| `FACTORUM_R2_*` | R2 o cualquier S3. Sin esto, guarda en disco (`/data`) |
| `LLM_API_BASE_URL`, `LLM_MODEL`, `LLM_API_KEY` | Modelo de visión compatible con OpenAI (Ollama, LM Studio, vLLM...) |
| `ARCANUM_URL`, `ARCANUM_URL_PUBLICA`, `ARCANUM_API_KEY`, `ARCANUM_CUIT_CONSULTANTE` | Constatación, apócrifos, padrón y el pase de la suite |
| `FACTORUM_TELEGRAM_TOKEN`, `FACTORUM_TELEGRAM_PERMITIDOS` | Bot de Telegram |
| `FACTORUM_WHATSAPP`, `FACTORUM_WHATSAPP_PERMITIDOS` | WhatsApp como dispositivo vinculado |
| `FACTORUM_WEBHOOK_TOKEN`, `FACTORUM_WEBHOOK_SALIDA_URL` | Webhook genérico (secretaria, Evolution API, n8n) |
| `FACTORUM_CANAL_CLIENTES` | Qué cliente es cada remitente de chat o de mail (`5491155550000=20222222223,@cliente.com.ar=30711111111`) |
| `FACTORUM_CORREO_*`, `FACTORUM_IMAP_*`, `FACTORUM_CLOUDFLARE_SECRETO`, `FACTORUM_MAILGUN_SIGNING_KEY`, `FACTORUM_GOOGLE_*`, `FACTORUM_GMAIL_*` | Correo entrante (ver [.env.example](.env.example)) |
| `FACTORUM_SMTP_*` | Correo saliente |
| `FACTORUM_AVISO_*` | Avisos por mail |
| `FACTORUM_WORKERS` | Documentos en paralelo (por defecto, los núcleos) |

## Usuarios, roles y claves

| Rol | Puede |
|---|---|
| `lectura` | Mirar: listados, resumen, exportar y verificar sin guardar |
| `usuario` | Además cargar, revisar, corregir, constatar y aprobar |
| `admin` | Además personas (hasta admin), claves de API, los clientes de los canales y la auditoría |
| `superadmin` | Todo, incluida la configuración y los otros superadmin |

- **Alcance por cliente.** A cada persona y a cada clave se le pueden asignar CUITs de cliente: solo ve y carga lo de esos clientes (en la pantalla, la API y MCP). Vacío = todos.
- **Contraseñas** con argon2id, de 10 caracteres o más. Al crear a alguien o restablecerle la contraseña, Factorum genera una temporal que se muestra una sola vez y que obliga a cambiarla al entrar. Después de cinco intentos fallidos hay que esperar, cada vez más.
- **Con correo saliente**: invitación por mail al crear a alguien (un link para elegir la contraseña, vence a las 72 horas) y «Olvidé mi contraseña» en el login (vence a los 30 minutos, contesta igual exista o no la cuenta, tiene tope por email e IP y, al usarlo, cierra las demás sesiones). Los links se guardan por su SHA-256.
- **Sesión** por cookie (HttpOnly, SameSite=Lax y Secure con URL pública https); vence a las 12 horas sin uso.
- **Claves de API** (`fct_...`) desde la pantalla, con rol, clientes, vencimiento y la marca «Puede abrir sesiones de la suite». Se muestran una sola vez; Factorum guarda solo su SHA-256. La pantalla ya no entra con una clave de API: las claves son para la API, MCP y las otras apps.
- **Auditoría** (admin): entradas, salidas, intentos fallidos, personas, claves, configuración (qué cambió, nunca el valor), correcciones, aprobaciones, exportaciones y pases de la suite.

### Suite Escriba: de Arcanum a Factorum y vuelta

Con las dos apps conectadas, se pasa de una a la otra sin volver a loguearse (contrato en [docs/suite-pase.md](docs/suite-pase.md)):

- **De Factorum a Arcanum**: en Configuración, Arcanum, cargá la URL interna, la pública y una clave de Arcanum habilitada para la suite. Aparece el ícono de grilla arriba a la derecha con «Arcanum».
- **De Arcanum a Factorum**: Arcanum usa `FACTORUM_TOKEN`. Puede ser una clave creada en Claves de API con «Puede abrir sesiones de la suite», o `FACTORUM_API_KEY` con `FACTORUM_API_KEY_SUITE_TOPE`. Factorum necesita su URL pública.
- La persona entra con **su** rol de Factorum si ya tiene cuenta (no se escala), topeado por el rol tope de la clave; si no tiene cuenta, se crea con el rol que trae (también topeado).

## Canales de mensajería

Mandás el PDF o la foto por chat y Factorum contesta cuando termina el lote:

```
Listo: 3 comprobante(s). 2 bien, 1 para revisar, 0 con errores.

- Factura A 00002-00001543, DISTRIBUIDORA EJEMPLO S.R.L., $ 121.000,00: bien
- Factura B 00003-00020077, DISTRIBUIDORA EJEMPLO S.R.L., $ 60.500,00: revisar (...)
```

- Con el texto **"verificar"** junto a la factura, contesta en el momento si es legal.
- **"estado"** cuenta cómo va lo último que mandaste.
- Solo responde a remitentes habilitados.

| Canal | Cómo |
|---|---|
| **Telegram** | Bot API oficial, sin riesgo de bloqueo |
| **WhatsApp** | whatsmeow, como dispositivo vinculado. Se vincula desde Configuración, WhatsApp, con el QR o con un código. No es oficial: el número se puede bloquear |
| **Webhook** | Tu bot existente (la secretaria con Baileys, Evolution API, n8n) manda `POST /api/canales/webhook` y recibe la respuesta. Los avisos posteriores van firmados con HMAC a `FACTORUM_WEBHOOK_SALIDA_URL` |

## Correo

Las facturas también llegan por mail. Configuración, **Correo entrante**, tiene cuatro formas (una activa a la vez, se cambia en caliente):

| Forma | Cómo | Para quién |
|---|---|---|
| **IMAP** (por defecto) | Factorum revisa los no leídos de una carpeta cada 60 s y los marca como leídos o los mueve a «Factorum» | Una casilla de Gmail (contraseña de aplicación), del dominio o de Microsoft 365 |
| **Cloudflare Email Routing** | Un Email Worker firma cada mail (HMAC-SHA256 con la hora, ventana de 5 minutos) y lo manda a `POST /api/correo/entrante/cloudflare`. El código del Worker viene en la pantalla | Dominios en Cloudflare: llega al instante |
| **Proveedor** | Mailgun (con su firma), Postmark o SendGrid (con un token en la URL) mandan cada mail a `POST /api/correo/entrante/{proveedor}` | Si ya usás uno |
| **Gmail con permiso de Google** | OAuth (PKCE, permiso `gmail.modify`): Factorum lee una etiqueta («Facturas») con la Gmail API y se la saca al procesar | Una cuenta de Gmail sin contraseña de aplicación |

Reglas para todas:

- **El mail es dato, nunca instrucción.** Solo se toman los adjuntos (PDF, JPG, PNG, ZIP y los PDF dentro de un `.eml` adjunto); el texto ni se lee ni se guarda. Los logos de las firmas no cuentan. Tope por mail (25 MB) y por adjunto (`FACTORUM_MAX_MB`).
- **Remitentes permitidos** (emails o `@dominio`): lo de los demás queda en **Correo retenido** (quién, asunto, fecha y los adjuntos, cifrados), donde un admin lo acepta o lo descarta. Se borra a los 30 días. Sin lista, todo queda retenido.
- **Cliente**: `facturas+CUIT@dominio` (CUIT con dígito verificador) o el remitente en Clientes y canales (email o `@dominio`).
- **Sin duplicados** por `Message-ID` (y los archivos, por su SHA-256).
- **Respuesta** al remitente permitido cuando termina el lote, en el mismo hilo (`In-Reply-To`), con el resumen y el link. Nunca a un retenido.

**Correo saliente** (SMTP con STARTTLS o TLS implícito) para las respuestas, los avisos (lote terminado, apócrifos o adulterados a los admins, resumen de los lunes a las 8) y los links de las contraseñas. Cada persona recibe solo lo de sus clientes.

## MCP

`POST /mcp` es Streamable HTTP, con una clave de API (cada clave ve solo sus clientes). La pantalla **Conectar** trae los bloques listos para Claude Code y Claude Desktop. Para un asistente, conviene una clave de rol lectura. Factorum no necesita un LLM, pero uno se le puede conectar así:

| Herramienta | Qué hace |
|---|---|
| `verificar_factura` | Veredicto de legalidad, sin guardar nada |
| `procesar_factura` | Carga y procesa una factura (solo con clave de rol usuario o más) |
| `buscar_comprobantes` | Busca entre lo cargado |
| `comprobante` | Un comprobante con todos sus datos |
| `pendientes_revision` | Lo que tiene que mirar una persona |
| `resumen` | Totales por alícuota |
| `estado_lote` | Progreso de un lote |

## API

Ver [docs/API.md](docs/API.md).

## Privacidad

- Por defecto, **ninguna IA ve los comprobantes**.
- Los archivos se guardan comprimidos y **cifrados antes de salir del proceso**: ni el disco ni el bucket de R2 los ven en claro.
- Sin la master key no se abren. Guardala fuera del servidor.
- Los logs nunca llevan contenido de facturas, contraseñas, claves, tokens ni pases.
- La configuración secreta (claves de Arcanum, de R2, del modelo, el token de Telegram y del webhook, las contraseñas de IMAP y SMTP y el permiso de Google) se guarda cifrada con la master key y nunca vuelve completa por la API.
- Del correo solo se guardan el remitente, el asunto y los adjuntos; nunca el texto del mail. Los adjuntos retenidos van cifrados.

## Desarrollo

Requisitos: Go 1.26 y poppler-utils.

```bash
go test ./...
FACTORUM_TEST_DB=postgres://... go test ./internal/api/   # prueba de punta a punta con Postgres
ARCANUM_DIR=../arcanum node scripts/fixtures.js testdata/pdf   # regenerar las facturas de prueba
```

Las facturas de prueba se generan con el modelo "ARCA" de Arcanum, que copia "Comprobantes en línea". Los CUIT y CAE son de fantasía.

### Estructura

| Paquete | Qué tiene |
|---|---|
| `internal/arca` | CUIT, tipos de comprobante, QR de la RG 4892 |
| `internal/qrscan` | Lector de QR (gozxing con normalización de grilla, más zbar de respaldo) |
| `internal/pdfx` | poppler |
| `internal/extractor` | Parser de texto, Tesseract, visión |
| `internal/pipeline` | La cascada |
| `internal/validar` | Los controles |
| `internal/store` | Postgres |
| `internal/app` | Orquestación, cola y canales |
| `internal/almacen` | Disco o R2, con zstd y AES-GCM |
| `internal/arcanum` | Constatación y padrón |
| `internal/auth` | Roles, identidad y alcance por CUIT, argon2id, tokens y límite de intentos |
| `internal/ajustes` | La configuración desde la pantalla, con el entorno que gana y la recarga en caliente |
| `internal/api` | REST, sesiones, usuarios, claves, auditoría y el pase de la suite |
| `internal/mcpserver` | MCP |
| `internal/canal/*` | Telegram, WhatsApp y webhook |
| `internal/correo` | Leer mails (solo adjuntos), IMAP, Gmail, Cloudflare y proveedores, SMTP y las plantillas; `correotest` tiene SMTP e IMAP de prueba |
| `web/` | UI sin build, con el sistema de diseño de la suite |

## Licencia

Apache-2.0. Factorum no está afiliado ni respaldado por ARCA.
