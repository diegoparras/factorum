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
| `con_errores` | Hay errores locales, aunque ARCA lo confirme |
| `no_verificable` | No hay Arcanum, faltan datos o ARCA no respondió |

### 5. Papel de trabajo

Cada dato guarda su origen: `qr`, `texto`, `ocr`, `vision`, `manual` o `arcanum`. Cada corrección queda registrada con el valor de antes, el de después, quién y cuándo. Una persona aprueba.

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
cp .env.example .env   # completá FACTORUM_DB_PASSWORD, FACTORUM_API_KEY y FACTORUM_MASTER_KEY
docker compose up -d
```

Entrá a **http://localhost:8095** con la clave de API.

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

Todo es por variables de entorno. Ver [.env.example](.env.example).

| Variable | Para qué |
|---|---|
| `FACTORUM_DATABASE_URL` | Postgres (obligatoria para `serve`) |
| `FACTORUM_API_KEY` / `FACTORUM_API_KEY_LECTURA` | Claves de operador y de solo lectura |
| `FACTORUM_MASTER_KEY` | Cifra todo lo que se guarda (AES-256-GCM). Obligatoria con R2 |
| `FACTORUM_R2_*` | R2 o cualquier S3. Sin esto, guarda en disco (`/data`) |
| `LLM_API_BASE_URL`, `LLM_MODEL`, `LLM_API_KEY` | Modelo de visión compatible con OpenAI (Ollama, LM Studio, vLLM...) |
| `ARCANUM_URL`, `ARCANUM_API_KEY`, `ARCANUM_CUIT_CONSULTANTE` | Constatación, apócrifos y padrón |
| `FACTORUM_TELEGRAM_TOKEN`, `FACTORUM_TELEGRAM_PERMITIDOS` | Bot de Telegram |
| `FACTORUM_WHATSAPP`, `FACTORUM_WHATSAPP_PERMITIDOS` | WhatsApp como dispositivo vinculado |
| `FACTORUM_WEBHOOK_TOKEN`, `FACTORUM_WEBHOOK_SALIDA_URL` | Webhook genérico (secretaria, Evolution API, n8n) |
| `FACTORUM_WORKERS` | Documentos en paralelo (por defecto, los núcleos) |

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
| **WhatsApp** | whatsmeow, como dispositivo vinculado. Se vincula desde la pestaña Canales con el QR o con un código. No es oficial: el número se puede bloquear |
| **Webhook** | Tu bot existente (la secretaria con Baileys, Evolution API, n8n) manda `POST /api/canales/webhook` y recibe la respuesta. Los avisos posteriores van firmados con HMAC a `FACTORUM_WEBHOOK_SALIDA_URL` |

## MCP

`POST /mcp` es Streamable HTTP, con la misma clave que la API. Factorum no necesita un LLM, pero uno se le puede conectar así:

| Herramienta | Qué hace |
|---|---|
| `verificar_factura` | Veredicto de legalidad, sin guardar nada |
| `procesar_factura` | Carga y procesa una factura (solo con clave de operador) |
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
- Los logs nunca llevan contenido de facturas ni claves.

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
| `internal/api` | REST |
| `internal/mcpserver` | MCP |
| `internal/canal/*` | Telegram, WhatsApp y webhook |
| `web/` | UI sin build, con el sistema de diseño de la suite |

## Licencia

Apache-2.0. Factorum no está afiliado ni respaldado por ARCA.
