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

## Conciliación con ARCA

Lo que el cliente te mandó contra lo que ARCA dice que recibió. En **Conciliación** (menú Datos) elegís el cliente y el período (mes pasado, este mes, últimos 3 meses o las fechas que quieras) y Factorum le pide a [Arcanum](https://github.com/diegoparras/arcanum) los comprobantes **recibidos** de Mis Comprobantes para cruzarlos con lo cargado:

- **Coinciden:** mismo emisor, tipo, punto de venta, número, fecha y total (y neto e IVA en las facturas A cuando ARCA los trae), con un centavo de tolerancia.
- **Con diferencias:** te dice qué no cierra y cuánto. Si el emisor y el número coinciden pero no el punto de venta o el tipo, lo marca como **casi igual**: suele ser un error de lectura y lo corregís en Revisar.
- **Falta cargar:** está en ARCA y no en Factorum. Copiás la lista o bajás un Excel para pedírsela al cliente.
- **No figura en ARCA:** separa las causas legítimas (tiques de controlador fiscal, moneda extranjera o exterior, a nombre de otro CUIT o consumidor final, período que Arcanum todavía no bajó, notas de crédito y débito) del resto, que conviene constatar; si ya se constató y el emisor es apócrifo, lo marca en rojo.

Arriba ves cuándo sincronizó Arcanum por última vez y qué fechas tiene bajadas, con un aviso si el período no está completo. Totales por grupo (las notas de crédito restan; la moneda extranjera va aparte), Excel con una hoja por grupo, CSV, la API (`GET /api/conciliacion`) y la herramienta MCP `conciliar`. Necesita Arcanum 2.3.0 o superior con Mis Comprobantes sincronizado para ese CUIT, y que la clave de Arcanum que usa Factorum incluya al cliente.

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
| `FACTORUM_OIDC_ISSUER`, `FACTORUM_OIDC_CLIENT_ID`, `FACTORUM_OIDC_CLIENT_SECRET`, `FACTORUM_OIDC_SCOPE`, `FACTORUM_OIDC_ROLE_CLAIM`, `FACTORUM_OIDC_DEFAULT_ROLE`, `FACTORUM_OIDC_CREAR_USUARIOS`, `FACTORUM_OIDC_SOLO` | Inicio de sesión único con Lockatus (ver [Lockatus](#lockatus-inicio-de-sesión-único)) |
| `FACTORUM_LOGIN_LOCAL` | Salida de emergencia: `1` vuelve a dejar entrar con email y contraseña aunque esté «Solo se entra con Lockatus». Solo por el entorno |
| `FACTORUM_IA_POR_DEFECTO`, `FACTORUM_CONSTATAR_POR_DEFECTO` | Lo que viene marcado al cargar |
| `FACTORUM_R2_*` | R2 o cualquier S3. Sin esto, guarda en disco (`/data`) |
| `FACTORUM_COPIAS`, `FACTORUM_COPIAS_HORA`, `FACTORUM_COPIAS_DIARIAS`, `FACTORUM_COPIAS_MENSUALES` | Copia diaria de la base (por defecto sí, a las 03:30, 14 diarias y 12 mensuales) |
| `FACTORUM_PAPELERA_DIAS` | Días que queda lo borrado en la papelera antes del borrado definitivo (1 a 365, por defecto 30) |
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

## Borrar lo que se cargó mal: la papelera

¿Cargaste un lote al cliente equivocado o subiste lo que no era? Nada se borra de golpe:

- **Un lote entero** (admin o superadmin): en **Lotes** (o arriba de sus comprobantes), «Borrar». Para confirmar hay que escribir el nombre del lote (o «lote 12» si no tiene). Si todavía se está procesando, lo que falta se cancela.
- **Comprobantes sueltos** (usuario o más, de sus clientes): en **Comprobantes**, marcá las casillas (o «elegir todos los de esta página») y tocá «Borrar (N)»; o «Borrar» en la revisión de uno. Si a un archivo no le queda ningún comprobante, se va con ellos.
- **Si solo le erraste al cliente**, no hace falta borrar: «Cambiar cliente» en el lote pasa el lote y sus comprobantes al CUIT correcto (con el dígito verificador controlado). Lo que ya se había constatado con ARCA usando el CUIT del cliente viejo como consultante se vuelve a constatar con el nuevo.

Todo va a la **Papelera** (menú Datos): ahí se ve qué se borró, cuándo, quién, de qué lote y cliente, y cuántos días faltan para que se borre para siempre. **Restaurar** lo deja tal cual estaba, con sus estados, correcciones y constatación (lo que había quedado sin procesar se termina de procesar). Mientras está en la papelera no aparece en Comprobantes, Revisar, Resumen, Inicio, las exportaciones, MCP, los chats ni los avisos por mail.

- **Volver a subir** lo que está en la papelera funciona: no cuenta como repetido y se procesa como nuevo. Después no se puede restaurar el viejo (quedarían dos iguales): primero hay que borrar el nuevo.
- **Para siempre:** a los 30 días (Configuración, **Papelera**, de 1 a 365; o `FACTORUM_PAPELERA_DIAS`) Factorum lo borra de verdad, junto con los archivos del disco y de R2 que no use ningún otro lote (el mismo PDF subido dos veces es un solo archivo y se queda mientras alguien lo use). Lo revisa cada hora; mientras corre el pase a R2, los archivos esperan. Un superadmin puede **Vaciar la papelera** o **Borrar ya** algo, escribiendo «borrar» (con la sesión; una clave de API no puede).
- Todo queda en la auditoría: quién borró, restauró, vació o cambió el cliente, y el borrado automático.

Las copias de seguridad de la base llevan lo que está en la papelera: al restaurar una, vuelve a estar en la papelera con su fecha. Ojo: si la copia es de antes de un borrado definitivo, esas filas vuelven pero sus archivos ya no están.

## Almacenamiento en R2 y copias de seguridad

### R2

Sin R2, los originales y los resultados se guardan en el disco del servidor (`/data/almacen`). Con R2 activo (Configuración, Almacenamiento R2), lo nuevo va al bucket y lo que ya estaba en el disco se sigue leyendo de ahí: un objeto se busca primero en R2 y, si no está, en el disco. Todo sale del proceso comprimido y cifrado.

**Pasar lo del disco a R2.** En Configuración, Almacenamiento R2, el botón «Pasar lo del disco a R2» (superadmin) arranca un trabajo en segundo plano que:

- sube cada objeto tal cual (ya está cifrado: no se vuelve a cifrar) y lo **verifica**: el tamaño y el SHA-256 de lo que quedó en R2 tienen que coincidir con el disco;
- es **reanudable**: si se corta o se reinicia el servidor, sigue desde donde quedó y lo verificado no se vuelve a subir;
- sube de a cuatro, reintenta con espera y muestra el avance (objetos, bytes, errores y tiempo estimado), con **Pausar** y **Seguir**;
- **no borra nada del disco**. Al terminar, «Liberar el disco» (hay que escribir «liberar») borra solo lo verificado, después de volver a mirar que siga en R2.

Por consola, lo mismo con el avance en texto:

```bash
factorum r2 migrar            # Ctrl+C pausa; volver a correrlo sigue
factorum r2 estado
factorum r2 migrar --liberar  # cuando terminó
```

Ojo: después de liberar el disco, esos archivos están solo en R2. Si apagás R2, no se van a poder ver.

### Copias de seguridad de la base

En Configuración, **Copias de seguridad** (superadmin): la copia diaria (por defecto activada, a las 03:30 de Buenos Aires), cuántas guardar (14 diarias y la primera de cada uno de los últimos 12 meses) y la lista con **Descargar** y «Hacer una copia ahora». Si el servidor estaba apagado a la hora de la copia, la hace al volver. Si la última falló, Inicio se lo avisa al superadmin.

- **Dónde:** en R2 (`copias/` dentro del prefijo) si está activo; si no, en `FACTORUM_DATOS/copias`. Conviene R2: una copia en el mismo disco no sirve si se rompe el disco. Bajá una de vez en cuando y guardala en otro lado.
- **Qué es:** una copia lógica y portable hecha por Factorum (no depende de la versión de `pg_dump` ni de Postgres): cada tabla con `COPY`, todo en una transacción `REPEATABLE READ` (una foto consistente), en un tar con un manifiesto (versión de Factorum y del esquema, fecha, tablas, filas y SHA-256 de cada una), comprimida con zstd y **cifrada con `FACTORUM_MASTER_KEY`**. Sin esa clave no se abre: guardala fuera del servidor.
- **Qué no lleva:** la cola de trabajos, el estado del pase a R2 y la lista de copias (son del servidor, no datos). Los archivos de las facturas tampoco: están en R2 o en el disco.

### Cómo restaurar

Restaurar borra lo que hay en la base y carga la copia, por eso se hace solo por consola. Hace falta la misma `FACTORUM_MASTER_KEY`.

```bash
# 1. Verificar (no toca nada): descifra, controla cada tabla contra el manifiesto y muestra qué tiene.
factorum restaurar copias/factorum-20261127-033000.copia       # una clave en R2
factorum restaurar /data/copias/factorum-20261127-033000.copia # o un archivo

# 2. Restaurar. Si la base tiene datos, antes hace una copia de lo que hay (--sin-copia-previa
#    para no hacerla). Vacía y recarga en una sola transacción: si algo falla, queda como estaba.
factorum restaurar copias/factorum-20261127-033000.copia --confirmar

# 3. Reiniciar Factorum, para que tome la configuración restaurada.
```

Funciona en una base vacía (crea las tablas) o sobre la que hay. Las secuencias quedan donde estaban, lo que quedó a medio procesar vuelve a la cola y la restauración queda en la auditoría. Una copia de un Factorum más nuevo no se restaura con uno más viejo.

**En EasyPanel:** abrí el servicio de Factorum, pestaña **Consola**, y corré los comandos de arriba (el contenedor ya tiene `FACTORUM_DATABASE_URL`, `FACTORUM_MASTER_KEY` y la configuración de R2). Si la copia es un archivo descargado, subilo al volumen `/data` (por ejemplo con la pestaña de archivos del volumen) y pasá su ruta. Cuando termine, tocá **Restart** en el servicio. Si querés restaurar en otra base (para probar una copia sin tocar la de producción), creá un Postgres nuevo en EasyPanel y corré el comando con `FACTORUM_DATABASE_URL=postgres://... factorum restaurar ...`.

También hay `factorum copia`, que hace una copia en el momento (por ejemplo, antes de actualizar).

## Usuarios, roles y claves

| Rol | Puede |
|---|---|
| `lectura` | Mirar: listados, resumen, exportar y verificar sin guardar |
| `usuario` | Además cargar, revisar, corregir, constatar y aprobar; borrar comprobantes, cambiar el cliente de un lote y restaurar de la papelera |
| `admin` | Además borrar lotes enteros, personas (hasta admin), claves de API, los clientes de los canales y la auditoría |
| `superadmin` | Todo, incluida la configuración, los otros superadmin y vaciar la papelera |

- **Alcance por cliente.** A cada persona y a cada clave se le pueden asignar CUITs de cliente: solo ve y carga lo de esos clientes (en la pantalla, la API y MCP). Vacío = todos.
- **Contraseñas** con argon2id, de 10 caracteres o más. Al crear a alguien o restablecerle la contraseña, Factorum genera una temporal que se muestra una sola vez y que obliga a cambiarla al entrar. Después de cinco intentos fallidos hay que esperar, cada vez más.
- **Con correo saliente**: invitación por mail al crear a alguien (un link para elegir la contraseña, vence a las 72 horas) y «Olvidé mi contraseña» en el login (vence a los 30 minutos, contesta igual exista o no la cuenta, tiene tope por email e IP y, al usarlo, cierra las demás sesiones). Los links se guardan por su SHA-256.
- **Sesión** por cookie (HttpOnly, SameSite=Lax y Secure con URL pública https); vence a las 12 horas sin uso. La misma con email y contraseña, con Lockatus o con el pase de la suite.
- **Claves de API** (`fct_...`) desde la pantalla, con rol, clientes, vencimiento y la marca «Puede abrir sesiones de la suite». Se muestran una sola vez; Factorum guarda solo su SHA-256. La pantalla ya no entra con una clave de API: las claves son para la API, MCP y las otras apps.
- **Auditoría** (admin): entradas, salidas, intentos fallidos, personas, claves, configuración (qué cambió, nunca el valor), correcciones, aprobaciones, exportaciones y pases de la suite.

### Lockatus: inicio de sesión único

**Lockatus** es el inicio de sesión único de la suite Escriba (un proveedor OpenID Connect). Con Factorum federado, el login muestra **«Entrar con Lockatus»** y cada persona entra con la misma cuenta que usa en Arcanum y en las demás apps. Funciona igual con otro proveedor OIDC.

**1. En Lockatus**, registrá una aplicación para Factorum:

- Tipo **confidencial** (con client secret), flujo **Authorization Code** con **PKCE S256**.
- **Redirect URI**, exacta: `https://<tu Factorum>/api/auth/oidc/callback` (la URL pública más esa ruta, sin barra al final). La pantalla la muestra para copiar.
- **Scopes**: `openid profile email`. Factorum reconoce a cada persona por su **email verificado**: si Lockatus no lo manda, o lo manda con `email_verified: false`, no entra.
- **Rol**: mandá el rol de la persona para Factorum en el claim `role` del `id_token`: `superadmin`, `admin`, `usuario` (u `operador`) o `lectura`. Puede ser una palabra, varias o una lista; gana la de más permisos. Con otro nombre (o anidado, como `realm_access.roles`), ponelo en «Claim del rol».

**2. En Factorum**, Configuración, **Lockatus (inicio de sesión único)** (superadmin): el emisor (la URL de Lockatus, tal como la publica en `/.well-known/openid-configuration`), el client ID y el client secret (queda cifrado). **Probar** lee el discovery y las claves. Al guardar se aplica sin reiniciar. También se puede fijar todo por el entorno (`FACTORUM_OIDC_*`, ver [.env.example](.env.example)). Hace falta la URL pública.

**Quién entra y con qué rol:**

- Si el email ya tiene cuenta en Factorum (sin distinguir mayúsculas), entra con **su rol y sus clientes de Factorum**: lo que diga Lockatus no lo escala ni lo baja. Si está desactivada, no entra.
- Si no tiene cuenta, se crea (origen Lockatus, sin contraseña) con el rol del claim o, si no trae uno conocido, con el **rol por defecto** (lectura, salvo que un superadmin elija otro). Admin y superadmin ven todos los clientes; **usuario y lectura nacen sin clientes**: no ven nada (Inicio les dice «Pedile a un admin que te asigne clientes») hasta que un admin se los asigna en Usuarios. Con «Crear la cuenta de quien entra por primera vez» apagado, quien no tiene cuenta no entra.
- Un admin también puede dar de alta a alguien «para Lockatus» desde Usuarios, con su rol y sus clientes: cuando entre, ya los tiene.
- Una cuenta de Lockatus no tiene contraseña en Factorum: no entra por el login local, no recibe «Olvidé mi contraseña» y en Usuarios no tiene «Nueva contraseña» ni invitación.
- La sesión es la de siempre (12 horas sin uso, se cierra al salir o al desactivar a la persona). Cada entrada y cada rechazo (con el motivo) queda en la auditoría.

**Seguridad:** Authorization Code con PKCE, `state` y `nonce` de un solo uso que quedan en el servidor y se atan al navegador con una cookie de 10 minutos; el `id_token` se verifica entero (firma RS256 o ES256 con las claves del emisor, emisor, audiencia, vencimiento y nonce); después de entrar se va siempre a la portada; hay límite de intentos; y nunca se loguean tokens, códigos, `state`, `nonce` ni el client secret.

**Solo se entra con Lockatus.** Con esa opción, el login con email y contraseña desaparece y **el servidor lo rechaza** (también «Olvidé mi contraseña» y los links por mail); las altas nuevas son para Lockatus. Las claves de API, MCP y el pase de la suite siguen andando, y la pantalla «Crear el primer superadmin» no cambia. Para no quedar afuera, se activa solo desde una sesión abierta con Lockatus.

**Salida de emergencia.** Si Lockatus se cae con «Solo se entra con Lockatus» activo, poné `FACTORUM_LOGIN_LOCAL=1` en el entorno del servidor y reiniciá: vuelve el login con contraseña (para quien la tiene) y la pantalla de Configuración lo avisa. Sacala cuando Lockatus vuelva a andar. En EasyPanel: el servicio de Factorum, **Environment**, agregá la variable y **Deploy**.

Para probarlo sin un Lockatus de verdad: `go run ./internal/oidc/oidctest/lockatus-falso -puerto 9099` levanta uno de mentira (emisor `http://127.0.0.1:9099`, client ID `factorum`, secret `secreto-de-prueba`) con una pantalla para elegir con qué email y rol entrar.

### Suite Escriba: de Arcanum a Factorum y vuelta

Con las dos apps conectadas, se pasa de una a la otra sin volver a loguearse (contrato en [docs/suite-pase.md](docs/suite-pase.md)):

- **De Factorum a Arcanum**: en Configuración, Arcanum, cargá la URL interna, la pública y una clave de Arcanum habilitada para la suite. Aparece el ícono de grilla arriba a la derecha con «Arcanum».
- **De Arcanum a Factorum**: Arcanum usa `FACTORUM_TOKEN`. Puede ser una clave creada en Claves de API con «Puede abrir sesiones de la suite», o `FACTORUM_API_KEY` con `FACTORUM_API_KEY_SUITE_TOPE`. Factorum necesita su URL pública.
- La persona entra con **su** rol de Factorum si ya tiene cuenta (no se escala), topeado por el rol tope de la clave; si no tiene cuenta, se crea con el rol que trae (también topeado).
- Si las dos apps están federadas con Lockatus, el paso ya es directo por el inicio de sesión único; el pase sigue andando igual (también con «Solo se entra con Lockatus»).

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
| `conciliar` | Lo cargado de un cliente contra lo que ARCA dice que recibió |
| `estado_lote` | Progreso de un lote |

## API

Ver [docs/API.md](docs/API.md).

## Privacidad

- Por defecto, **ninguna IA ve los comprobantes**.
- Los archivos y las copias de la base se guardan comprimidos y **cifrados antes de salir del proceso**: ni el disco ni el bucket de R2 los ven en claro.
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
| `internal/almacen` | Disco o R2, con zstd y AES-GCM (también en flujo, para las copias) |
| `internal/mudanza` | El pase de lo del disco a R2: verificado, reanudable y sin borrar nada solo |
| `internal/copias` | Copias de seguridad de la base: hacer, guardar, podar, programar y restaurar |
| `internal/arcanum` | Constatación, padrón y los recibidos de Mis Comprobantes |
| `internal/conciliar` | El cruce entre lo cargado y Mis Comprobantes (puro, en centavos) |
| `internal/auth` | Roles, identidad y alcance por CUIT, argon2id, tokens y límite de intentos |
| `internal/oidc` | Lockatus (OIDC): discovery, Authorization Code con PKCE y la verificación del `id_token`; `oidctest` tiene un Lockatus de mentira |
| `internal/ajustes` | La configuración desde la pantalla, con el entorno que gana y la recarga en caliente |
| `internal/api` | REST, sesiones, Lockatus, usuarios, claves, auditoría y el pase de la suite |
| `internal/mcpserver` | MCP |
| `internal/canal/*` | Telegram, WhatsApp y webhook |
| `internal/correo` | Leer mails (solo adjuntos), IMAP, Gmail, Cloudflare y proveedores, SMTP y las plantillas; `correotest` tiene SMTP e IMAP de prueba |
| `web/` | UI sin build, con el sistema de diseño de la suite |

## Licencia

Apache-2.0. Factorum no está afiliado ni respaldado por ARCA.
