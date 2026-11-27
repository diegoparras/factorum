# Novedades

Cada versión publicada tiene su sección. La imagen Docker toma la versión de la primera.

## [0.8.0] - 2026-11-27

### Elegir la IA de una lista

- En Configuración, **IA (visión)**, la IA se elige de una lista: primero el **proveedor** (Ollama en tu servidor, OpenAI, Anthropic, Google Gemini, OpenRouter u otro compatible con OpenAI, o **Sin IA**), y la URL se completa sola. Se puede cambiar solo donde tiene sentido: en Ollama (si corre en otra máquina) y en «Otro». Con OpenAI se elige además la **región** (global, Estados Unidos, Europa y las demás con residencia de datos), que cambia al dominio regional; ojo: la residencia de datos hay que habilitarla en el proyecto de OpenAI.
- La **clave** aparece solo si el proveedor la pide (Ollama no), con el botón para mostrarla, «configurada, termina en ...» si ya hay una y el link a dónde se saca la de cada proveedor.
- El **modelo** se elige de la lista que le pide Factorum al proveedor apenas hay proveedor y clave (la que escribiste o, si está vacía, la guardada), sin guardar nada. Solo quedan los que sirven para leer imágenes: de Ollama, los que tienen visión (los demás aparecen deshabilitados, «no lee imágenes»); de OpenAI, sin los de audio, voz, imágenes, embeddings, moderación, búsqueda y los que no andan con chat (pro, codex); de Gemini, los gemini; de OpenRouter, los que reciben imágenes. Si el tuyo no está, **Otro modelo…** y lo escribís.
- **Probados por Factorum**: arriba de la lista, con su resultado en una prueba chica (8 tickets de compra, 4 limpios y 4 fotos movidas, el 27/11/2026): gpt-6.1-terra (OpenAI) 100 % y ~7 s por imagen; gpt-6-luna (OpenAI) 95 % y ~4 s; qwen3-vl:4b-instruct (Ollama) 79 % y ~73 s en 4 núcleos sin GPU; gemma4:e4b (Ollama) 61 % en la mitad de la prueba y ~3 min 45 s. Si un recomendado no está en tu Ollama, te dice el `ollama pull` para bajarlo.
- Errores claros: «La clave no es válida para OpenAI», Ollama apagado (y cómo prenderlo), sin conexión al proveedor, lista vacía.
- **Probar** usa lo que está en pantalla aunque no lo hayas guardado: le manda a la IA un ticket de prueba (inventado, con un CUIT y un total conocidos) y te muestra qué leyó y cuánto tardó. Si es externa y nadie aceptó el aviso para ese host, primero el aviso; si lo aceptás para probar, al guardar no te lo vuelve a pedir.
- El sello y el aviso dicen qué proveedor elegiste: OpenRouter aparece por su nombre (y el aviso explica que le pasa cada pedido al proveedor del modelo), y un Ollama en una dirección pública, como «Ollama en <host>».
- La clave guardada nunca viaja a otro host: si cambiás de proveedor sin escribir una clave nueva (o elegís Ollama), se borra. Entre regiones de OpenAI sirve la misma.
- Conectar explica cómo elegir entre una IA local y una externa, con los recomendados.
- Las configuraciones de antes siguen andando: el proveedor se deduce de la URL. `FACTORUM_IA_PROVEEDOR` lo fija desde el servidor.

### Aviso de la IA externa

- Si la IA (visión) que configurás es **externa** (OpenAI, Anthropic, Google Gemini u otra), antes de guardarla Factorum te muestra **qué pasa con los datos**: a dónde viajan, cuánto tiempo se guardan, si entrenan con ellos y qué queda en Factorum. Los datos salen de las páginas oficiales de cada proveedor (leídas el 27/11/2026), con sus links. Para guardarla hay que tocar **Entiendo y quiero usarla**: queda registrado quién lo aceptó, cuándo y para qué host, también en la auditoría.
- **Sin esa aceptación, Factorum no la usa**: ni en la pantalla, ni en los chats, ni por mail, ni por MCP. Si la IA viene por variables de entorno, Configuración te avisa que está sin usar y te deja aceptarla ahí. Si cambiás de proveedor o de host, o si cambia el texto del aviso, se pide de nuevo.
- **Cada persona** ve el mismo aviso la primera vez que marca **Usar IA si hace falta** en Verificar o Cargar (o que manda algo con la casilla marcada): **Entendido, usar IA** o **Seguir sin IA**. Se recuerda en su cuenta, no en el navegador.
- Al lado de la casilla, un sello dice qué IA se usa («IA externa: OpenAI» o «Local: las imágenes no salen de tu servidor») y el link **Qué pasa con los datos**. También en Configuración, IA y en Conectar.
- Con **Google Gemini**, un aviso en rojo: si la clave es de un proyecto sin facturación, los comprobantes de tus clientes pueden usarse para mejorar productos de Google.
- Con **OpenAI**, Factorum le pide que no guarde la respuesta (`store=false`). Si usás su residencia de datos (eu.api.openai.com y los demás dominios regionales), el aviso dice la región.
- Una IA en una dirección pública ya no pasa por local por tener «ollama» en el nombre (como ollama.com): Factorum mira el host de verdad. Siguen siendo locales los nombres sin punto (ollama, lmstudio), los *.internal y las IPs privadas.

### API

- `POST /api/ia/modelos` (solo un superadmin con sesión) con `{proveedor, url, clave}`: la lista de modelos del proveedor, filtrada y con los recomendados. La clave no vuelve ni queda en el log.
- `POST /api/configuracion/probar/ia` manda el ticket de prueba y devuelve `leido` y `segundos`; con una IA externa sin aceptar responde 409 (`codigo: aviso_ia`) hasta que venga `aceptar_aviso_ia`. Configuración trae el campo `ia.proveedor` y la lista de proveedores y recomendados (`ia`).
- `GET /api/ia/aviso` (el proveedor, el host, si está aceptado, si ya lo viste y el contenido del aviso), `POST /api/ia/aviso/visto` y, en `PUT /api/configuracion`, `aceptar_aviso_ia`. Verificar o cargar con `ia=true` desde la sesión de alguien que todavía no vio el aviso responde 409 (`codigo: aviso_ia`). Las claves de API no son personas: les alcanza con la aceptación del superadmin.

### Arreglos

- La IA (visión) anda con los modelos nuevos de OpenAI, como gpt-6-luna, que solo aceptan la temperatura por defecto. Antes Factorum les pedía temperatura 0 y respondían error siempre; ahora, si el modelo la rechaza, Factorum repite el pedido sin ella y lo recuerda para los siguientes.

## [0.7.0] - 2026-11-27

### Borrar lo que se cargó mal

- **Borrar un lote entero** (admin o superadmin): en Lotes, o arriba de sus comprobantes, el botón **Borrar**. Para confirmar hay que escribir el nombre del lote. Si todavía se está procesando, lo que falta se cancela.
- **Borrar comprobantes sueltos** (usuario o más, de tus clientes): en Comprobantes hay casillas para elegir varios (y «elegir todos los de esta página») y una barra con **Borrar (N)**. También hay un botón **Borrar** en la revisión de cada comprobante. Si a un archivo no le queda ningún comprobante, se va con ellos.
- **Cambiar el cliente de un lote**: si lo cargaste al cliente equivocado, **Cambiar cliente** lo pasa al CUIT correcto con todos sus comprobantes, sin borrar nada. Lo que ya se había constatado con ARCA usando el CUIT del cliente viejo se vuelve a constatar con el nuevo.

### Papelera

- Lo que se borra va a la **Papelera** (menú Datos): qué es, cuándo y quién lo borró, de qué lote y cliente, y cuántos días faltan para que se borre para siempre. Cada uno ve lo de sus clientes.
- **Restaurar** lo deja tal cual estaba: estados, correcciones, aprobaciones y constatación. Lo que había quedado sin procesar se termina de procesar.
- Mientras está en la papelera no aparece en ningún lado: Comprobantes, Revisar, Resumen, Inicio, exportaciones, MCP, los chats, los avisos por mail ni la detección de duplicados.
- **Volver a subir** un archivo que está en la papelera funciona: ya no dice «repetido» y se procesa como nuevo. Si después querés restaurar el viejo, Factorum te avisa que ya hay uno igual cargado: borrá ese primero.
- A los **30 días** (se cambia en Configuración, **Papelera**, de 1 a 365) se borra para siempre, con los archivos del disco y de R2 que no use ningún otro lote. Un superadmin puede **Vaciar la papelera** o **Borrar ya** algo, escribiendo «borrar».
- Las copias de seguridad llevan lo que está en la papelera, y todo (borrar, restaurar, vaciar, cambiar el cliente y el borrado automático) queda en la auditoría.

### API

- `DELETE /api/lotes/{id}`, `PATCH /api/lotes/{id}` con `cliente_cuit`, `POST /api/comprobantes/borrar`, `GET /api/papelera`, `POST /api/papelera/restaurar` y, solo con la sesión de un superadmin, `DELETE /api/papelera` y `POST /api/papelera/borrar`. MCP no puede borrar nada.

## [0.6.0] - 2026-11-27

### Conciliación con ARCA

- Pantalla nueva **Conciliación** (menú Datos, para todos los roles): elegís el cliente y el período y Factorum compara las facturas de compra que cargaste con lo que ARCA dice que ese cliente **recibió** (Mis Comprobantes, que baja Arcanum). Atajos para el mes pasado, este mes y los últimos 3 meses. Si ves un solo cliente, ya viene elegido.
- Cuatro grupos, cada uno con su tarjeta de totales y su pestaña con la tabla (ordenable tocando el encabezado):
  - **Coinciden**: el mismo comprobante en los dos lados, con la misma fecha y el mismo total (y neto e IVA en las A cuando ARCA los trae), con un centavo de tolerancia.
  - **Con diferencias**: está en los dos pero algo no cierra; te dice qué campo y cuánto. Si coinciden el emisor y el número pero no el punto de venta o el tipo, aparece como **casi igual**: suele ser un error de lectura. Es una sugerencia, nunca una coincidencia.
  - **Falta cargar**: está en ARCA y no en Factorum. **Copiar la lista** arma el texto para mandárselo al cliente, y **Excel para el cliente** baja solo esos.
  - **No figura en ARCA**: lo cargaste y ARCA no lo tiene. Las causas legítimas van aparte (tiques de controlador fiscal, moneda extranjera o exterior, a nombre de otro CUIT o de consumidor final, fechas que Arcanum todavía no bajó, notas de crédito y débito); para el resto, **Constatar** (si tu rol lo permite) y, si ya se constató, el resultado. Un emisor apócrifo queda marcado en rojo.
- Cada comprobante que está en Factorum se abre en **Revisar**; al cerrar volvés a la conciliación.
- Arriba, el estado de Mis Comprobantes en Arcanum: la última sincronización, qué fechas tiene bajadas y si la última corrida falló. Si el período no está completo, un aviso dice qué días faltan y que lo que cae ahí puede aparecer como «Falta cargar» o «No figura en ARCA» sin serlo.
- Los comprobantes cargados dos veces (o repetidos en ARCA) se muestran como tales y no se cuentan dos veces.
- Totales por grupo con las notas de crédito restando y la moneda extranjera aparte.
- **Excel** con un resumen y una hoja por grupo, y **CSV**. Cada exportación queda en la auditoría.
- Si Factorum no está conectado con Arcanum, si Arcanum todavía no bajó Mis Comprobantes de ese cliente o si la clave de Arcanum no lo incluye, la pantalla te dice qué pasa y qué hacer.
- Por API, `GET /api/conciliacion`, `GET /api/conciliacion.xlsx` y `.csv`; por MCP, la herramienta `conciliar`. Lo que trae Arcanum se guarda unos minutos (**Actualizar** lo vuelve a pedir).
- Necesita **Arcanum 2.3.0** o superior, con Mis Comprobantes sincronizado para ese cliente y una clave que lo incluya (alcanza con rol lectura).

## [0.5.0] - 2026-11-27

### Entrar con Lockatus

- Factorum se puede federar con **Lockatus**, el inicio de sesión único de la suite Escriba: el login muestra **«Entrar con Lockatus»** y entrás con la misma cuenta que usás en Arcanum. Se configura en Configuración, **Lockatus (inicio de sesión único)**, con el paso a paso para registrar a Factorum en Lockatus, la dirección de vuelta lista para copiar y un botón **Probar**. Al guardar se aplica sin reiniciar.
- Te reconoce por tu **email verificado** en Lockatus. Si ya tenías cuenta en Factorum, entrás con tu rol y tus clientes de siempre: lo que diga Lockatus no te sube ni te baja el rol.
- Quien entra por primera vez se crea solo, con el rol que manda Lockatus (o **lectura**, si no manda uno conocido). Admin y superadmin ven todos los clientes; usuario y lectura empiezan **sin clientes** y en Inicio ven «Pedile a un admin que te asigne clientes» hasta que un admin se los asigna en Usuarios. Se puede apagar la creación automática para que solo entren los que ya tienen cuenta.
- Si algo sale mal (email sin verificar, cuenta desactivada, sin cuenta, el inicio de sesión venció), el login te dice qué pasó.
- **«Solo se entra con Lockatus»**: el login con email y contraseña desaparece y el servidor lo rechaza. Las claves de API, MCP y el pase desde Arcanum siguen andando. Se activa desde una sesión abierta con Lockatus, para no quedar afuera, y si Lockatus se cae, `FACTORUM_LOGIN_LOCAL=1` en el servidor devuelve el login con contraseña (paso a paso en el README).
- Cada entrada con Lockatus y cada rechazo, con el motivo, quedan en la auditoría.

### Usuarios

- La columna Origen muestra **Lockatus** para quien entró por ahí. Esas cuentas no tienen contraseña en Factorum: no tienen «Nueva contraseña», invitación ni «Olvidé mi contraseña», y el menú no ofrece «Cambiar mi contraseña».
- Al crear o editar a alguien, «Qué clientes ve» se elige entre **Todos**, **Ninguno todavía** y **Solo estos CUITs** (antes, dejar el campo vacío daba acceso a todos sin querer). En Clientes, «Ninguno todavía» aparece resaltado.
- Con Lockatus configurado, un admin puede dar de alta a alguien que **entra con Lockatus**, con su rol y sus clientes desde el primer día.

## [0.4.0] - 2026-11-27

### Correo

- Las facturas pueden llegar por mail. En Configuración, Correo entrante, elegís una de cuatro formas: una casilla por IMAP (Gmail, Microsoft 365 o la del dominio), Cloudflare Email Routing con un Worker (el código viene listo para copiar), un proveedor (Mailgun, Postmark o SendGrid) o Gmail con permiso de Google (botón «Conectar con Google»). Cada una con su paso a paso y un botón Probar.
- Cada mail con PDFs, fotos o un ZIP adjuntos genera un lote, igual que un mensaje de Telegram. Solo se toman los adjuntos: el texto del mail no se lee ni se guarda. Un mail repetido no se carga dos veces.
- Remitentes permitidos (un email o un @dominio): lo de los demás queda en la pantalla nueva **Correo retenido**, donde un admin lo acepta (y, si quiere, suma al remitente) o lo descarta. Los retenidos se borran solos a los 30 días. Sin lista, todo queda retenido.
- Para cargar a un cliente, que manden a facturas+CUIT@tudominio, o asociá el email o el @dominio del remitente en Clientes y canales.
- Cuando termina el lote, Factorum le contesta al remitente en el mismo hilo con el resumen: cuántos comprobantes, cuántos bien, para revisar y con errores, los apócrifos y el link. Nunca a un retenido.
- Correo saliente por SMTP (Gmail, Microsoft 365, Resend, Postmark, Mailgun, Brevo...), con un mail de prueba.
- Avisos por mail, cada uno con su interruptor y sus destinatarios: lote terminado, emisor apócrifo o PDF posiblemente adulterado (a los admins) y un resumen los lunes a las 8. Cada persona recibe solo lo de sus clientes.
- «Olvidé mi contraseña» en el login: llega un link que vale una vez y vence en 30 minutos, y al usarlo se cierran las demás sesiones.
- En Usuarios, al crear a alguien, «Mandarle una invitación por mail» para que elija su contraseña (vence en 72 horas), en lugar de la temporal. También «Reenviar invitación» y «Link por mail».

### R2 y copias de seguridad

- Pasar lo del disco a R2: en Configuración, Almacenamiento R2, un botón sube a R2 lo que quedó guardado en el disco antes de activarlo. Cada archivo se verifica (tamaño y SHA-256 de lo que quedó en R2), se puede pausar y seguir, muestra el avance con el tiempo estimado y, si se reinicia el servidor, sigue desde donde quedó sin volver a subir lo verificado.
- Nunca borra nada del disco por su cuenta: al terminar, «Liberar el disco» (escribiendo «liberar») borra solo lo verificado, después de volver a mirar que siga en R2.
- Mientras tanto y después, los originales y las páginas se siguen viendo: cada archivo se busca primero en R2 y, si no está o llegó roto, en el disco.
- Por consola: `factorum r2 migrar`, `factorum r2 migrar --liberar` y `factorum r2 estado`.
- Copias de seguridad de la base: una por día (por defecto a las 03:30 de Buenos Aires), en R2 si está activo o en el disco. Se guardan las últimas 14 y la primera de cada uno de los últimos 12 meses. Se configuran en Configuración, Copias de seguridad, con la lista, «Descargar» y «Hacer una copia ahora».
- La copia la hace Factorum (no depende de la versión de pg_dump): una foto consistente de todas las tablas, con un manifiesto que dice qué tiene, comprimida y cifrada con la clave maestra.
- Restaurar, por consola: `factorum restaurar <copia>` verifica la copia y, con `--confirmar`, hace una copia de lo que hay y restaura todo en una transacción. Sirve en una base vacía. Paso a paso en el README, también para EasyPanel.
- Si la última copia falló, Inicio se lo avisa al superadmin y, con los avisos de alertas prendidos y el correo saliente configurado, también le llega un mail. Cada copia, descarga, restauración y paso del pase a R2 queda en la auditoría.

## [0.3.0] - 2026-11-27

### Usuarios y roles

- Se entra con email y contraseña. La primera vez, Factorum pide crear el primer superadmin con un código de un solo uso que aparece en el log del servidor (o con `factorum salud`).
- Cuatro roles: lectura (solo mira), usuario (carga, revisa, corrige y aprueba), admin (además personas, claves de API, clientes y auditoría) y superadmin (todo, también la configuración).
- A cada persona se le pueden asignar los CUITs de los clientes que atiende: solo ve, carga y exporta lo de esos clientes, en la pantalla, la API y MCP.
- Pantalla Usuarios: alta con una contraseña temporal que se muestra una sola vez y obliga a cambiarla al entrar, cambio de rol y de clientes, desactivar, reactivar y restablecer la contraseña.
- Menú de usuario arriba a la derecha con tu nombre, tu rol, «Cambiar mi contraseña» y «Cerrar sesión». La sesión vence a las 12 horas sin uso y, después de cinco intentos fallidos, hay que esperar.
- Lectura no ve los botones de cargar, corregir ni aprobar.

### Configuración desde la pantalla

- Casi todo lo que antes iba en variables de entorno se configura en Configuración: general (nombre y URL pública), Arcanum, Telegram, WhatsApp, webhook, el modelo de visión, el almacenamiento en R2 y qué cliente es cada remitente de chat.
- Cada sección trae «Cómo se configura» con el paso a paso y un botón «Probar» que hace una prueba de verdad y dice qué falló.
- Al guardar se aplica en el momento, sin reiniciar.
- Las claves y tokens se guardan cifrados y nunca se vuelven a mostrar: solo «configurado» y los últimos cuatro caracteres. Lo que fija el servidor con una variable de entorno gana y aparece bloqueado.
- La vinculación de WhatsApp (el QR) pasó de Canales a Configuración.
- Ahora hacen falta solo `FACTORUM_DATABASE_URL`, `FACTORUM_MASTER_KEY` (obligatoria) y, si querés, `FACTORUM_PUERTO`.

### Claves de API y Conectar

- Pantalla Claves de API: cada una con nombre, rol, clientes, vencimiento y último uso; se muestra una sola vez y se puede revocar.
- Pantalla Conectar: cómo usar la API REST y MCP, con los bloques listos para pegar en Claude Code y Claude Desktop.

### Auditoría

- Quién entró (y quién lo intentó), quién salió, altas y cambios de personas y claves, cambios de configuración (qué cambió, nunca el valor), correcciones, aprobaciones, exportaciones y pases entre apps. Con filtros por fecha, persona y acción.

### Suite Escriba

- Con Arcanum conectado, el ícono de grilla de arriba abre Arcanum sin volver a loguearte, y desde Arcanum se entra a Factorum igual. Cada uno entra con su rol en la app de destino, nunca con uno mayor.

## [0.2.0] - 2026-11-26

### Tickets de compra

- Lee tickets de controlador fiscal (tique y tique factura A, B, C y M) de supermercados, estaciones de servicio, farmacias y bares, en PDF o en foto. Saca comercio, CUIT, tipo, punto de venta y número, fecha y hora, neto, IVA por alícuota y total, y nunca inventa lo que no está escrito.
- Las fotos se preparan antes del OCR (grises, tamaño, luz pareja, contraste y Otsu) y se leen con dos segmentaciones de página: gana la que encuentra más datos. Tesseract corre con un hilo por proceso, que es cuatro veces más rápido.
- Nuevos tipos de comprobante: los tiques de ARCA (81, 82, 83 y del 109 al 120).
- Los tiques no se constatan en ARCA: se controla el CUIT y la base de apócrifos. Nuevo veredicto `tique_controlado`. No se pide CAE.
- Rubro de gasto (supermercado, combustible, farmacia, gastronomía, transporte, servicios u otros) en tres capas: palabras clave, actividad del padrón de ARCA y la corrección de una persona, que se recuerda por CUIT del emisor. Las facturas también toman el rubro del padrón.
- Filtros `clase` (facturas o tickets) y `rubro` en la API, MCP y la exportación. El Excel y el CSV suman las columnas Hora y Rubro, y el resumen trae los tickets y el total por rubro.
- En pantalla: marca «Ticket», hora y rubro en Comprobantes, Revisar y Verificar; filtros por clase y rubro; Resumen con barras por rubro (tocá una para ver esos comprobantes); el rubro se cambia desde la revisión y se recuerda para el CUIT; los tickets del mes en Inicio.

### Diseño nuevo

- Identidad propia dentro de la suite Escriba: logo (una hoja con el cuadrado petróleo y la F blanca), color petróleo y las mismas fuentes que Arcanum (Inter y JetBrains Mono, servidas por Factorum).
- Inicio: lo de hoy y del mes, apócrifos, posibles adulteraciones, cuánto se leyó con IA, los últimos 14 días en barras y los últimos lotes. Si todavía no hay nada, los pasos para empezar.
- Revisar: una bandeja con lo que tiene errores y lo que hay que mirar, con contador en el menú. Con el teclado: A aprueba y pasa al siguiente, J siguiente, K anterior, Esc cierra.
- Menú agrupado (Trabajo, Datos, Configuración) y la sección actual arriba, al lado del logo.
- Novedades: al entrar después de una actualización, Factorum muestra qué cambió. También desde el menú, con Acerca de (versión y estado de cada componente).
- Pantallas vacías que explican qué hacer, veredicto con sello e ícono y estados con punto de color.
- Carga de archivos con botón propio (sin el «Choose File» del navegador): muestra los elegidos con su tamaño y el total.

## [0.1.0] - 2026-11-25

Primera versión de Factorum.

- Lee comprobantes de ARCA sin IA: el QR (RG 4892) es la fuente oficial, el texto del PDF completa razones sociales, neto e IVA por alícuota.
- Cascada: QR, texto, OCR (Tesseract) y, solo si hace falta, un modelo de visión local (Ollama) o cualquier API compatible con OpenAI. Cada dato dice de dónde salió.
- Controles: sumas, IVA contra el neto, CUIT, letra contra tipo, CAE, fechas, moneda extranjera, duplicados y PDFs cuyo texto no coincide con el QR (posible adulteración).
- Verificar si una factura es legal: constatación WSCDC y base de apócrifos vía Arcanum.
- Lotes de miles de facturas (PDF, fotos, ZIP) en una cola sobre Postgres: mil facturas en menos de 30 segundos en 4 núcleos.
- Revisión en pantalla con el original al lado, correcciones registradas (papel de trabajo) y aprobación.
- Exportación a Excel y CSV con resumen por alícuota.
- Archivos guardados en disco o R2, comprimidos (zstd) y cifrados (AES-256-GCM).
- MCP para asistentes, Telegram, WhatsApp y webhook genérico.
- Módulo de Arcanum.
