# Libro IVA Digital: compras, ventas e importaciones

Factorum arma los archivos de importación del Libro IVA Digital de ARCA (RG 4597 y modificatorias) para un cliente (CUIT) y un mes: las **compras** (lo que leyó Factorum), las **ventas** (los emitidos de Mis Comprobantes que trae Arcanum) y las **importaciones de bienes** (los despachos de importación). Cada libro son dos archivos: el de comprobantes y el de alícuotas. Este documento dice qué diseño de registro se usó, de dónde salió, cómo se completa cada campo con lo que hay, cómo funcionan los períodos (lo presentado y lo que llega tarde) y qué no se pudo confirmar.

El código está en `internal/libroiva` (puro, sin base ni red), con golden files en `internal/libroiva/testdata` (compras, ventas e importaciones). El lector de despachos está en `internal/despacho`.

## Fuentes oficiales

Consultadas el **27/11/2026** en el sitio de ARCA:

| Documento | URL | Versión |
|---|---|---|
| Anexo I: Diseños de registros | https://www.arca.gob.ar/iva/documentos/libro-iva-digital-diseno-registros.pdf (redirige desde `afip.gob.ar/libro-iva-digital/documentos/libro-iva-digital-diseno-registros.pdf`) | PDF creado el 14/01/2025 |
| Especificaciones (R.G. N° 4597) | https://www.arca.gob.ar/iva/documentos/Libro-IVA-Digital-Especificaciones.pdf | «Revisión 30/07/2025» |
| Tablas del sistema | https://www.arca.gob.ar/libro-iva-digital/documentos/Libro-IVA-Digital-Tablas-del-Sistema.pdf (redirige a `/iva/documentos/`) | PDF creado el 26/02/2025 |
| Diseño de registro para sujetos exentos | https://www.arca.gob.ar/iva/documentos/libro-iva-digital-diseno-de-registro-SUJETOS-EXENTOS.pdf | modificado el 26/12/2025 |
| Manual del usuario | https://www.arca.gob.ar/iva/documentos/libro-de-iva-digita-manual-del-usuario.pdf | PDF creado el 14/01/2025 |
| Página «Módulo Registración Electrónica de Operaciones» (aspectos a tener en cuenta para la importación) | https://www.arca.gob.ar/iva/iva-simple/especificaciones-especiales.asp | vigente al 27/11/2026 |

Para ventas y períodos (0.10.0), además, el mismo 27/11/2026:

| Documento | URL | Para qué |
|---|---|---|
| Ley de IVA, texto ordenado 1997 (Decreto 280/97), artículo 12 | https://servicios.infoleg.gob.ar/infolegInternet/anexos/40000-44999/42701/texact.htm | Período en que se computa el crédito fiscal |
| Resolución General ARCA 5700/2025 (Boletín Oficial del 27/05/2025) | https://www.boletinoficial.gob.ar/detalleAviso/primera/326006/20250527 | Importe desde el que se identifica al consumidor final ($ 10.000.000) |
| Tablas de países («CUIT país») | https://www.arca.gob.ar/sire/downloadfile.asp?name=paises | **No se pudo bajar** (la descarga respondió un texto de 55 bytes, no la tabla) |

Desde noviembre de 2025 la declaración jurada de IVA se presenta por **IVA Simple**; la registración de las operaciones (el ex Libro IVA Digital) es su «Módulo Registración Electrónica de Operaciones», y la página de ARCA remite a los mismos diseños de registro de arriba para importar compras.

## Qué archivos van

| Archivo | Diseño | Longitud | Lo arma Factorum |
|---|---|---|---|
| `LIBRO_IVA_DIGITAL_COMPRAS_CBTE` | «Compras e Importación de Bienes - Cabecera» | 325 | Sí, un registro por comprobante de compra |
| `LIBRO_IVA_DIGITAL_COMPRAS_ALICUOTAS` | «Compras - Alícuotas» | 84 | Sí, un registro por alícuota de cada A o M |
| `LIBRO_IVA_DIGITAL_VENTAS_CBTE` | «Ventas - Cabecera» | 266 | Sí (0.10.0), un registro por comprobante emitido |
| `LIBRO_IVA_DIGITAL_VENTAS_ALICUOTAS` | «Ventas - Alícuotas» | 62 | Sí (0.10.0), un registro por alícuota |
| cabecera de importaciones | «Compras e Importación de Bienes - Cabecera» | 325 | Sí (0.10.0), un registro por despacho |
| `LIBRO_IVA_DIGITAL_IMPORTACION_BIENES_ALICUOTA` | «Importaciones de Bienes - Alícuotas» | 50 | Sí (0.10.0), un registro por alícuota de cada despacho |
| `LIBRO_IVA_DIGITAL_IMPORTACION_SERVICIOS_CREDITO_FISCAL` | «Importaciones de Servicios» | 211 | **No** |
| `LIBRO_IVA_DIGITAL_CBTES_VENTAS_ANULADOS` | «Comprobantes Anulados» | 44 | **No** (ver «Ventas») |

Las Especificaciones dicen que, si en «Datos iniciales» se tildó «Importación Definitiva de Bienes», se genera **un archivo de importación para cada uno, además de uno para las compras ordinarias**: por eso los despachos van en su propio ZIP (la pestaña Importaciones), con la cabecera de compras y las alícuotas de importaciones. La importación de servicios no: necesita el pago del impuesto (fecha, monto e identificación de la constancia), que no está en el comprobante.

**Nombres dentro de cada ZIP.** La «denominación del archivo» de cada diseño más `.txt`: `LIBRO_IVA_DIGITAL_COMPRAS_CBTE.txt` y `_ALICUOTAS.txt` (compras), `LIBRO_IVA_DIGITAL_VENTAS_CBTE.txt` y `_ALICUOTAS.txt` (ventas). En importaciones, la cabecera tiene el mismo diseño (y la misma denominación) que la de compras: se llama `LIBRO_IVA_DIGITAL_IMPORTACION_BIENES_CBTE.txt` para que no pise a la de compras si se descomprimen los dos ZIP en la misma carpeta, y las alícuotas, `LIBRO_IVA_DIGITAL_IMPORTACION_BIENES_ALICUOTA.txt`. El manual dice que en «Importar archivos» el sistema pide los dos archivos (uno de comprobantes y otro de alícuotas); no encontré que exija un nombre, así que los nombres son una convención (ver «No confirmado»). Además va `resumen.csv`, que es para la persona y no se importa.

**El archivo de alícuotas de importaciones tiene tres nombres en las fuentes:** el Anexo I (el diseño de registro, con las posiciones y la longitud) lo llama `LIBRO_IVA_DIGITAL_IMPORTACION_BIENES_ALICUOTA`; el índice de las Especificaciones, `LIBRO_IVA_DIGITAL_COMPRAS_IMPORTACIONES`, y su texto, `LIBRO_IVA_DIGITAL_IMPORTACIONES`. Los tres describen el mismo registro (despacho, neto, alícuota, impuesto). Factorum usa el del **Anexo I**, que es el documento que define el registro; como el nombre no lo exige el aplicativo, no cambia lo que se importa.

## Reglas generales (Especificaciones, «Consideraciones generales»)

- Importes: 15 posiciones, 13 enteros y 2 decimales, **sin coma ni punto**, ceros a la izquierda. «Se consignarán en valores positivos». Si un campo resultara negativo, **el signo «-» va en la primera posición** de los 13 enteros (`-00000000012345`).
- Lo numérico se completa con **ceros a la izquierda**; lo alfanumérico, con **blancos a la derecha**. Un campo opcional sin dato va todo en blancos o ceros según su tipo.
- Archivos «con formato **ANSI**», con los juegos de caracteres **ISO 8859-1** o **Windows-1252**.
- El orden de los registros de alícuotas tiene que seguir el de los comprobantes («si el archivo cabeceras informa los comprobantes 8, 3 y 567, el de alícuotas mantiene ese orden»).
- B y C: «cantidad de alícuotas» = 0 y **no se informan registros de alícuotas**.
- Tamaño: hasta 10 MB por `.txt` (o un `.zip` de hasta 10 MB con el `.txt` de hasta 70 MB). Son unos 30.000 comprobantes por archivo.
- No se puede registrar el mismo comprobante en dos períodos.

## Diseño: LIBRO_IVA_DIGITAL_COMPRAS_CBTE (325)

Tipo de dato del diseño: 1 alfabético, 2 numérico, 3 alfanumérico.

| N° | Campo | Desde | Hasta | Long. | Tipo | Relleno | Qué pone Factorum |
|---|---|---|---|---|---|---|---|
| 1 | Fecha de comprobante o de oficialización | 1 | 8 | 8 | 2 | - | Fecha de emisión, `AAAAMMDD` |
| 2 | Tipo de comprobante | 9 | 11 | 3 | 2 | ceros izq. | Código de ARCA (`001`, `006`, `081`...) |
| 3 | Punto de venta | 12 | 16 | 5 | 2 | ceros izq. | El del comprobante |
| 4 | Número de comprobante | 17 | 36 | 20 | 2 | ceros izq. | El del comprobante |
| 5 | Despacho de importación | 37 | 52 | 16 | 3 | - | 16 ceros («Este campo se completará con ceros», Especificaciones); en importaciones, el número de despacho |
| 6 | Código de documento del vendedor | 53 | 54 | 2 | 2 | - | `80` (CUIT), obligatorio en compras |
| 7 | Número de identificación del vendedor | 55 | 74 | 20 | 3 | ceros izq. | CUIT del emisor |
| 8 | Apellido y nombre o denominación del vendedor | 75 | 104 | 30 | 3 | blancos der. | Razón social, recortada a 30 (ver «Texto») |
| 9 | Importe total de la operación | 105 | 119 | 15 | 2 | ceros izq. | Total del comprobante |
| 10 | Conceptos que no integran el precio neto gravado | 120 | 134 | 15 | 2 | ceros izq. | No gravado (solo A y M) |
| 11 | Importe de operaciones exentas | 135 | 149 | 15 | 2 | ceros izq. | Exento (solo A y M) |
| 12 | Percepciones o pagos a cuenta de IVA | 150 | 164 | 15 | 2 | ceros izq. | Percepción de IVA separada en Revisar (si no, 0: ver «Otros tributos») |
| 13 | Percepciones o pagos a cuenta de otros impuestos nacionales | 165 | 179 | 15 | 2 | ceros izq. | Percepción de Ganancias y otros nacionales separada |
| 14 | Percepciones de Ingresos Brutos | 180 | 194 | 15 | 2 | ceros izq. | Percepción de IIBB separada |
| 15 | Percepciones de impuestos municipales | 195 | 209 | 15 | 2 | ceros izq. | Percepción municipal separada |
| 16 | Impuestos internos | 210 | 224 | 15 | 2 | ceros izq. | Impuestos internos separados |
| 17 | Código de moneda | 225 | 227 | 3 | 3 | - | `PES`, `DOL`, `060`... (ver «Moneda») |
| 18 | Tipo de cambio | 228 | 237 | 10 | 2 | ceros izq. | 4 enteros y 6 decimales; `0001000000` en pesos («deberá completarse aún cuando el campo 17 sea igual a 'PES'») |
| 19 | Cantidad de alícuotas de IVA | 238 | 238 | 1 | 2 | - | Registros de alícuotas; 0 en B, C y tiques sin letra A o M |
| 20 | Código de operación | 239 | 239 | 1 | 1 | - | `E`, `N` o blanco (ver abajo) |
| 21 | Crédito Fiscal Computable | 240 | 254 | 15 | 2 | ceros izq. | IVA total en A y M; 0 en el resto |
| 22 | Otros Tributos | 255 | 269 | 15 | 2 | ceros izq. | Lo que no se separó de los otros tributos |
| 23 | CUIT emisor/corredor | 270 | 280 | 11 | 2 | - | 11 ceros (solo va en 033, 058, 059, 060 y 063) |
| 24 | Denominación del emisor/corredor | 281 | 310 | 30 | 3 | - | 30 blancos |
| 25 | IVA comisión | 311 | 325 | 15 | 2 | - | ceros |

El diseño «Compras por TurIVA» (340, con el reintegro del Decreto 1043/2016) no se usa: es para el régimen de turistas extranjeros.

## Diseño: LIBRO_IVA_DIGITAL_COMPRAS_ALICUOTAS (84)

| N° | Campo | Desde | Hasta | Long. | Tipo | Qué pone Factorum |
|---|---|---|---|---|---|---|
| 1 | Tipo de comprobante | 1 | 3 | 3 | 2 | El del registro de comprobantes |
| 2 | Punto de venta | 4 | 8 | 5 | 2 | Ídem |
| 3 | Número de comprobante | 9 | 28 | 20 | 2 | Ídem |
| 4 | Código de documento del vendedor | 29 | 30 | 2 | 2 | `80` |
| 5 | Número de identificación del vendedor | 31 | 50 | 20 | 3 | CUIT del emisor, ceros a la izquierda |
| 6 | Importe neto gravado | 51 | 65 | 15 | 2 | Neto de esa alícuota |
| 7 | Alícuota de IVA | 66 | 69 | 4 | 2 | Código de la tabla, con ceros a la izquierda |
| 8 | Impuesto liquidado | 70 | 84 | 15 | 2 | IVA de esa alícuota |

## Tablas usadas

**Alícuotas del IVA:** `0003` 0 %, `0004` 10,5 %, `0005` 21 %, `0006` 27 %, `0008` 5 %, `0009` 2,5 %.

**Códigos de operación:** `A` no alcanzado, blanco o `0` no corresponde, `C` canje, `D` devolución IVA turistas, `E` exentas, `N` no gravado, `T` reintegro Decreto 1043/2016, `X` importación del exterior (en ventas, exportación al exterior), `Z` importación de zona franca (en ventas, exportación a zona franca). Factorum usa `E`, `N`, `X` y blanco.

**Documento:** `80` CUIT (las compras lo exigen salvo bienes usados, no registrables o material reciclado), `86` CUIL, `87` CDI, `94` pasaporte, `96` DNI, `99` «SIN IDENTIFICAR / VENTA GLOBAL DIARIA», entre otros.

**Monedas:** `PES` pesos, `DOL` dólar estadounidense y códigos numéricos (`2` dólar libre, `12` real, `60` euro, etc.). Factorum reconoce los de la tabla, además de los ISO más comunes (`USD`, `EUR`, `BRL`...), y escribe los numéricos con tres dígitos (`060`).

**Tipos de comprobante:** la tabla trae todos los de compras y ventas. Los que Factorum lee: 001 a 013 y 015 (A, B y C), 019 a 021 (E), 051 a 054 (M), **066 (DESPACHO DE IMPORTACION)**, 081, 082, 083 y 109 a 120 (tiques), 201 a 213 (FCE MiPyME).

## Cómo se completa cada caso

### Montos

Factorum guarda los importes en centavos (`int64`), así que van tal cual: 1.234,56 es `000000000123456`. Lo que no entra en 13 enteros y 2 decimales no se exporta (problema «Un dato no entra en el registro»).

### Notas de crédito

Van **en positivo**, con su tipo (`003`, `008`, `013`, `112`...). Las Especificaciones piden importes positivos y el signo «-» solo «si los campos resultaran negativos»; la nota de crédito resta por su tipo. En el resumen de la pantalla y en `resumen.csv` restan. Un comprobante con algún importe negativo en Factorum no se exporta (problema «Importes negativos»): casi siempre es una lectura mal hecha.

### A y M (discriminan IVA): alícuotas y crédito fiscal

- Un registro de alícuotas por cada alícuota con IVA (21, 10,5, 27, 5, 2,5), en el orden del código. El campo 19 dice cuántos.
- **Neto por alícuota.** Factorum guarda un solo neto gravado y el IVA de cada alícuota. Con una alícuota, el neto es el neto gravado. Con varias, el de cada una sale de su IVA (IVA / tasa, al centavo) y la diferencia de redondeo con el neto gravado va a la alícuota de mayor neto, para que los registros sumen exactamente el neto gravado. Si la diferencia es más grande que lo que explica el redondeo (unos centavos por alícuota), el comprobante no se exporta: «El neto no se puede repartir entre las alícuotas». Siempre se avisa «Neto por alícuota calculado».
- **Sin IVA** (todo exento o no gravado): un registro con alícuota `0003`, neto 0 e impuesto 0 (las Especificaciones admiten neto 0 «si la operación no se encuentra gravada por IVA en su totalidad», con la alícuota 0 y el código de operación distinto de blanco). Código de operación `E` si hay exento, si no `N`. El exento va en el campo 11 y el no gravado en el 10.
- **Gravado con exento o no gravado:** la alícuota es la del gravado, el exento o no gravado van en su campo y el código de operación queda en blanco (hay una alícuota distinta de 0).
- **Crédito fiscal computable (campo 21):** el IVA total del comprobante. Es lo que piden las Especificaciones cuando **no hay prorrateo** («idéntico valor al del impuesto liquidado»). Si el cliente prorratea el crédito fiscal, se ajusta en el Libro IVA Digital después de importar.
- Neto gravado sin IVA, o IVA sin neto, en una A o M: no se exporta (casi siempre es una lectura incompleta; si es exento, hay que pasar el importe a «Exento» en Revisar).
- Neto + no gravado + exento + IVA + otros tributos tiene que dar el total («Deberá corresponderse con la suma de todos los importes del documento informado»). Hasta un centavo por componente (mínimo dos) es una advertencia de redondeo; más, no se exporta.

### B, C y tiques que no discriminan IVA

Cantidad de alícuotas `0`, ningún registro de alícuotas, crédito fiscal `0`, código de operación en blanco y solo el total (más otros tributos si los hay); no gravado y exento en 0. Las Especificaciones aclaran que en B y C la suma de los componentes no se controla contra el total. El IVA contenido que imprimen las B por la Ley 27.743 (Régimen de Transparencia Fiscal al Consumidor) no se informa: es informativo y no da crédito fiscal.

### Tiques y tique-factura

Van todos los de controlador fiscal que Factorum lee, con su código de la tabla:

| Código | Comprobante | Cómo va |
|---|---|---|
| 081 | Tique Factura A | Como una A: alícuotas y crédito fiscal |
| 082 | Tique Factura B | Como una B: sin alícuotas, crédito fiscal 0 |
| 083 | Tique (a consumidor final) | Sin alícuotas, crédito fiscal 0 |
| 109, 111 | Tique C, Tique Factura C | Como una C |
| 110, 113, 114 | Tique nota de crédito (sin letra, B, C) | Como B/C, resta |
| 112, 115 | Tique nota de crédito A, nota de débito A | Como una A |
| 116, 117 | Tique nota de débito B, C | Como B/C |
| 118, 119, 120 | Tique Factura, nota de crédito y de débito M | Como una M |

El punto de venta es el número del controlador fiscal y el número, el del tique.

### Moneda extranjera

Va **en la moneda original**, con el código de la moneda (campo 17) y la cotización del comprobante (campo 18, 4 enteros y 6 decimales: 1.180,50 es `1180500000`). Factorum no convierte a pesos. Al importar, el Libro IVA Digital pregunta cómo vienen los importes: hay que elegir **«Los importes están expresados en la Moneda Original del Comprobante»** (con todo en pesos, las dos opciones dan lo mismo). La pantalla lo avisa en rojo cuando hay moneda extranjera. Sin cotización (o con cotización 1 en otra moneda), no se exporta. En el resumen, la moneda extranjera va aparte, en su moneda.

### Percepciones y otros tributos

Lo que lee Factorum trae los otros tributos **sin detalle** (no sabe si son percepción de IVA, de Ingresos Brutos, municipal o impuesto interno). Desde 0.10.0 se pueden **separar en Revisar**: percepción de IVA (campo 12), de Ganancias y otros nacionales (13), de Ingresos Brutos (14), municipal (15) e impuestos internos (16). El detalle es parte de los otros tributos (que siguen siendo el total del comprobante): no puede sumar más. Lo que no se separó va en el campo 22, «Otros Tributos» («el total de tributos no informados en los otros campos»), con la advertencia **«Otros tributos sin detalle»**: conviene separarlo, sobre todo las **percepciones de IVA**, que solo se computan como pago a cuenta en el campo 12.

### Texto (razón social)

Se pasa a ISO 8859-1 sin acentos: las vocales con tilde o diéresis pierden el acento, la Ñ y la ñ quedan (existen en ISO 8859-1 y Windows-1252 con el mismo byte), las comillas y rayas tipográficas pasan a `"` y `-`, y cualquier otro carácter (emojis, otros alfabetos, controles) a un blanco; los blancos repetidos se juntan. Después se recorta a 30 y se completa con blancos a la derecha. Si queda vacía, se avisa.

### Encoding y saltos de línea

Bytes ISO 8859-1 (compatibles con Windows-1252) y **CRLF** al final de cada registro, también el último. Sin BOM.

### Orden

Los comprobantes van por fecha, tipo, punto de venta y número; las alícuotas, en el mismo orden que su comprobante.

## Validación previa

Antes de bajar, la pantalla (y `GET /api/libro-iva`) muestra los problemas agrupados por nivel:

| Nivel | Qué significa | Problemas |
|---|---|---|
| **No va al archivo** | No se exporta nunca | Sin CUIT válido del emisor; sin fecha (o fecha que no se entiende); fuera del período; tipo que no va (desconocido, factura E de exportación, 049 bienes usados, que va en su propia pestaña); sin punto de venta o número; sin total; importes negativos; moneda desconocida; moneda extranjera sin cotización; neto gravado sin IVA o IVA sin neto en A/M; A/M sin neto, no gravado ni exento; importes que no cierran; neto que no se puede repartir entre alícuotas; un dato que no entra; **duplicados** (va una sola copia: la que no tiene problemas que bloqueen, la aprobada, la que está bien, la más vieja; las otras solo figuran como duplicadas) |
| **Advertencia fuerte** | Va solo con «Todos»; no debería computarse | **Emisor apócrifo** (por la constatación o por Mis Comprobantes); ARCA no reconoce el comprobante; una A a nombre de otro CUIT |
| **Advertencia** | Va; conviene mirarlo | Sin aprobar («para revisar» o «con errores»); **no figura en ARCA** según la conciliación (aviso, nunca bloquea); distinto en ARCA; otros tributos sin detalle; B/C/tique a nombre de otro CUIT; diferencia de redondeo; IVA que no da con el neto; subtotal que no da el total (B y C); sin razón social; punto de venta 9998 o mayor |
| **Para tener en cuenta** | Cómo quedó | En moneda extranjera; neto por alícuota calculado; sin crédito fiscal (B, C y tiques) |

En **ventas** se suman: sin detalle por alícuota, falta el código de operación, factura C (no va); documento del comprador raro, consumidor final sin identificar desde $ 10.000.000, el detalle de ARCA y el de Arcanum no coinciden, otros tributos deducidos, exportación (advertencias); alícuota deducida y detalle de la emisión de Arcanum (para tener en cuenta). En **importaciones**: sin número de despacho o número inválido, sin identificación del proveedor del exterior, percepciones que suman más que los otros tributos (no va); número de despacho con otra forma (advertencia). En los tres, «Entró de un mes anterior» es para tener en cuenta.

**Qué va.** «Todos» exporta todo lo que no bloquea, con sus advertencias. «Solo los listos» exporta lo que está **bien o aprobado** y no tiene advertencias fuertes. Los comprobantes sin fecha del cliente aparecen como problema (no se puede saber si son del mes). Lo que está en la papelera no cuenta. La conciliación se pide a Arcanum solo para avisar: si Arcanum no está o no tiene el período, la pantalla lo dice y los archivos se arman igual.

## Ventas

Las ventas salen de los **emitidos de Mis Comprobantes** que baja Arcanum (`GET /api/mis-comprobantes/{cuit}/emitidos`, Arcanum 2.4.0). Factorum no lee facturas emitidas: lo que ARCA tiene registrado como emitido por el cliente es la fuente. Si Arcanum no bajó todo el mes (según sus sincronizaciones de emitidos que terminaron bien), la pestaña lo avisa con los días que faltan: lo emitido esos días no está en el libro.

### Diseño: LIBRO_IVA_DIGITAL_VENTAS_CBTE (266)

| N° | Campo | Desde | Hasta | Long. | Tipo | Qué pone Factorum |
|---|---|---|---|---|---|---|
| 1 | Fecha de comprobante | 1 | 8 | 8 | 2 | Fecha de emisión, `AAAAMMDD` |
| 2 | Tipo de comprobante | 9 | 11 | 3 | 2 | Código de ARCA |
| 3 | Punto de venta | 12 | 16 | 5 | 2 | El del comprobante |
| 4 | Número de comprobante | 17 | 36 | 20 | 2 | Número desde |
| 5 | Número de comprobante hasta | 37 | 56 | 20 | 2 | Número hasta (igual al 4 salvo en los rangos) |
| 6 | Código de documento del comprador | 57 | 58 | 2 | 2 | El de Mis Comprobantes; `99` si es consumidor final sin identificar |
| 7 | Número de identificación del comprador | 59 | 78 | 20 | 3 | Ceros a la izquierda; `0` con el código 99 |
| 8 | Apellido y nombre o denominación del comprador | 79 | 108 | 30 | 3 | La denominación de Mis Comprobantes; «CONSUMIDOR FINAL» o «VENTA GLOBAL DIARIA» |
| 9 | Importe total de la operación | 109 | 123 | 15 | 2 | Total |
| 10 | Conceptos que no integran el precio neto gravado | 124 | 138 | 15 | 2 | No gravado |
| 11 | Percepción a no categorizados | 139 | 153 | 15 | 2 | Tributo 13 de la emisión propia, si lo hay |
| 12 | Importe de operaciones exentas | 154 | 168 | 15 | 2 | Exento |
| 13 | Percepciones o pagos a cuenta de impuestos nacionales | 169 | 183 | 15 | 2 | Tributos 1, 6 y 9 de la emisión propia |
| 14 | Percepciones de Ingresos Brutos | 184 | 198 | 15 | 2 | Tributos 5 y 7 |
| 15 | Percepciones impuestos municipales | 199 | 213 | 15 | 2 | Tributos 3 y 8 |
| 16 | Impuestos internos | 214 | 228 | 15 | 2 | Tributo 4 |
| 17 | Código de moneda | 229 | 231 | 3 | 3 | Como en compras |
| 18 | Tipo de cambio | 232 | 241 | 10 | 2 | Como en compras |
| 19 | Cantidad de alícuotas de IVA | 242 | 242 | 1 | 2 | Registros de alícuotas (al menos 1) |
| 20 | Código de operación | 243 | 243 | 1 | 1 | `E`, `N`, `X` o blanco |
| 21 | Otros Tributos | 244 | 258 | 15 | 2 | Lo que no va en 11 ni 13 a 16 |
| 22 | Fecha de Vencimiento o Pago | 259 | 266 | 8 | 2 | La de la emisión propia, si la hay; si no, `00000000` |

### Diseño: LIBRO_IVA_DIGITAL_VENTAS_ALICUOTAS (62)

| N° | Campo | Desde | Hasta | Long. | Tipo |
|---|---|---|---|---|---|
| 1 | Tipo de comprobante | 1 | 3 | 3 | 2 |
| 2 | Punto de venta | 4 | 8 | 5 | 2 |
| 3 | Número de comprobante | 9 | 28 | 20 | 2 |
| 4 | Importe neto gravado | 29 | 43 | 15 | 2 |
| 5 | Alícuota de IVA | 44 | 47 | 4 | 2 |
| 6 | Impuesto liquidado | 48 | 62 | 15 | 2 |

A diferencia de compras, el registro de alícuotas de ventas **no lleva el documento** de la contraparte.

### Cómo se completa cada caso

- **El comprador (campos 6 a 8).** Las Especificaciones piden la CUIT salvo que el receptor sea consumidor final; a consumidor final que no hace falta identificar, código `99`, número `0` y la leyenda «CONSUMIDOR FINAL»; en las registraciones globales diarias, `99` y «VENTA GLOBAL DIARIA». Factorum usa `99` cuando Mis Comprobantes trae el 99 o un número en cero; si el comprobante es un **rango** (número hasta mayor que el número, como los tiques de controlador fiscal agrupados) va «VENTA GLOBAL DIARIA»; si no, «CONSUMIDOR FINAL» (o el nombre que trae ARCA, si no dice consumidor). Con CUIT, CUIL o DNI van tal cual (un CUIT o CUIL que no es válido, o un DNI de largo raro, se avisa; va como lo tiene ARCA). Una CUIT país (50, 51 o 55) se acepta para operaciones con el exterior.
- **Consumidor final por montos grandes.** Las Especificaciones (revisión 30/07/2025) dicen que desde **$ 20.932** hay que identificarlo. Ese importe está desactualizado: la **RG 5700/2025** (B.O. 27/05/2025) cambió el Anexo II de la RG 1415 y lo llevó a **$ 10.000.000** («Si el importe de la operación es igual o superior a PESOS DIEZ MILLONES ... CUIT, CUIL o CDI o ... DNI»). Factorum avisa (no bloquea) una venta a consumidor final sin identificar de $ 10.000.000 o más (en moneda extranjera, por su cotización), salvo los rangos de venta global diaria.
- **Alícuotas.** En ventas, también las B y los tiques llevan alícuotas: «aún tratándose de compradores consumidores finales, sujetos exentos y monotributistas igualmente deberá consignarse el IVA contenido» (Especificaciones, ventas alícuotas, campo 6). El detalle sale, en este orden:
  1. de **Mis Comprobantes**, si Arcanum lo bajó y cierra con sus totales;
  2. de la **emisión propia de Arcanum** (lo que mandó por WSFEv1 y ARCA aprobó), si es el mismo comprobante y el total coincide; se avisa como «Detalle de la emisión de Arcanum»;
  3. si no hay ninguno: con neto e IVA en cero, una alícuota 0 % (`0003`) con su código de operación; si el IVA es exactamente una alícuota del neto (al centavo, con un margen chico por el redondeo de cada ítem), esa alícuota («Alícuota deducida»); si no, **no va** («Sin detalle por alícuota»).

  Si Mis Comprobantes y la emisión propia tienen alícuotas distintas, va la de Mis Comprobantes (es lo que ARCA registró) con la advertencia. La prioridad es de Arcanum 2.4.0 y está en su `openapi.yaml`.
- **Percepciones.** Mis Comprobantes trae un solo importe de otros tributos (y en lo bajado antes de Arcanum 2.4.0, ninguno). Si el comprobante es de la emisión propia de Arcanum, sus tributos (con el código de la tabla de tributos de WSFEv1) se reparten en los campos 11 y 13 a 16 como dice la tabla de arriba; los impuestos provinciales (2) y otros (99) quedan en el 21. Si no se conocen los otros tributos, son lo que falta para llegar al total («Otros tributos deducidos»). El campo de **percepción de IVA** no existe en el registro de ventas: va en el 13 («percepciones o pagos a cuenta de impuestos nacionales»).
- **Código de operación.** Solo cuando todas las alícuotas son 0 %: `X` en una factura E (exportación), `E` si hay exento, `N` si hay no gravado. Un neto al 0 % sin exento ni no gravado no va («Falta el código de operación»): no hay con qué elegirlo.
- **Exportaciones (E).** Van con alícuota 0 % y código `X` («Exportación al Exterior» en la tabla de ventas). Si Mis Comprobantes trae solo el total, va solo el total. Si es a zona franca, el código es `Z` y hay que cambiarlo en el aplicativo.
- **Moneda.** Igual que en compras: en la moneda original con su cotización.
- **Notas de crédito.** En positivo, con su tipo; en el resumen restan.
- **Facturas C.** No van: las emite un monotributista o un exento, y el libro de ventas de IVA es de responsables inscriptos (los exentos tienen su propio diseño de registro, que Factorum no arma).
- **Total (campo 9).** «Deberá ser igual a la suma de todos los importes que componen el comprobante»: neto + no gravado + exento + IVA + otros tributos. Hasta un centavo por componente es redondeo; más, no va.
- **Comprobantes anulados.** El aplicativo los registra en la solapa «Comprobantes Anulados» (diseño de 44 posiciones). Mis Comprobantes, como lo baja Arcanum, no marca comprobantes anulados (un comprobante electrónico no se anula: se cancela con una nota de crédito), así que Factorum no arma ese archivo.
- **Duplicados.** Mismo tipo, punto de venta y número: va uno solo.

## Importaciones de bienes

Cada **despacho de importación** (tipo 066 de la tabla de comprobantes: «DESPACHO DE IMPORTACION») es un comprobante de Factorum: se marca en Revisar («Es un despacho de importación») o se sube el PDF del despacho. «En el caso de importación de bienes, no se registrará la Factura del exterior, sino el Despacho de Importación.»

**Cabecera (diseño de compras, 325).** Campo 1, la **fecha de oficialización** («En caso que el comprobante a registrar sea un despacho de importación, se completará con la fecha de oficialización»); 2, `066`; 3 y 4 en **cero** («este campo podrá ser cero si el campo Tipo de Comprobante se informa con un tipo de documento aduanero»); 5, el **número de despacho**; 6 y 7, código `80` y la identificación del vendedor (el proveedor del exterior, ver «No confirmado»); 8, su nombre; 9 a 22 como una compra A, con las percepciones de la liquidación en sus campos (el IVA adicional es percepción de IVA, campo 12; la de Ganancias, campo 13; la de Ingresos Brutos, campo 14); el crédito fiscal es el IVA. Sin IVA, alícuota 0 % con código de operación `X` (importación del exterior).

**Alícuotas (`LIBRO_IVA_DIGITAL_IMPORTACION_BIENES_ALICUOTA`, 50):**

| N° | Campo | Desde | Hasta | Long. | Tipo | Qué pone Factorum |
|---|---|---|---|---|---|---|
| 1 | Despacho de importación | 1 | 16 | 16 | 3 | El del registro de cabecera |
| 2 | Importe neto gravado | 17 | 31 | 15 | 2 | La base imponible (el «valor de imposición aduanera») de esa alícuota |
| 3 | Alícuota de IVA | 32 | 35 | 4 | 2 | Código de la tabla |
| 4 | Impuesto liquidado | 36 | 50 | 15 | 2 | El IVA de esa alícuota |

**Número de despacho.** El diseño solo dice «16 posiciones, alfanumérico». Factorum lo guarda sin blancos, guiones ni puntos, en mayúsculas, y exige **16 letras y números** (si no, no se guarda en Revisar y no va al archivo). Avisa (sin bloquear) si no tiene la forma usual del número de una destinación del Sistema Informático MALVINA: año (2 dígitos), aduana (3 dígitos), tipo de destinación (2 letras y 2 dígitos, como `IC04`), número (6 dígitos) y un carácter de control (letra o dígito): `26001IC04123456K`. Esa estructura no está en ninguna fuente oficial que haya podido consultar (ver «No confirmado»).

**Lector de despachos en PDF.** Si un PDF sin QR de ARCA tiene un número con la forma del SIM, la palabra «oficialización», algo de la Aduana, la destinación o la liquidación, y **no** tiene CAE, se lee como despacho (una factura de un importador suele imprimir el número de despacho de la mercadería, y no es un despacho). Lee: el número de despacho (si aparece uno solo), la fecha de oficialización (si hay una sola), y de la liquidación la línea del IVA (base, alícuota e importe; la alícuota, si la línea la dice o si el IVA es justo el 21 o el 10,5 % de la base), el IVA adicional, la percepción de Ganancias y la de Ingresos Brutos (cada una si aparece una sola vez). Lo que aparece dos veces con valores distintos o no se puede asegurar queda en blanco; el total es la suma de lo leído. **Nunca** lee el proveedor del exterior. El comprobante queda «para revisar» con un aviso que dice qué se leyó y qué no, y lo leído queda con origen «texto».

## Períodos: lo presentado y lo que llega tarde

**Período del libro.** Cada comprobante de compra (y cada despacho) tiene su período del libro (AAAA-MM). Por defecto es el mes de la fecha (de emisión, o de oficialización en un despacho). En ventas, siempre el mes de la fecha.

**Exportar un mes lo registra.** Bajar el ZIP de un libro guarda una exportación: cliente, período, libro (compras, ventas o importaciones), quién, cuándo, la opción («Todos» o «Solo los listos»), el SHA-256 y el tamaño de cada archivo, y cada comprobante que fue con su clave (emisor, tipo, punto de venta y número; el despacho; en ventas, tipo, punto de venta y número) y la **huella** de su registro (un hash de su línea y sus alícuotas). Un período con al menos una exportación queda **presentado**. Se puede volver a exportar; la pantalla compara lo que se exportaría ahora (con la misma opción que la última vez) con la última exportación y dice qué **entró**, qué **salió** y qué **cambió**.

**Lo que llega tarde.** Un comprobante nuevo (o restaurado de la papelera) cuya fecha cae en un período **ya presentado** para ese cliente (en el libro que le toca: compras, o importaciones si es un despacho) va al **primer período abierto posterior**, con el hallazgo «Llegó después de presentar octubre de 2026: va en noviembre de 2026». Las Especificaciones dicen que «no se podrá registrar el mismo comprobante en dos períodos diferentes»: por eso se queda en su período lo que **fue** en la última exportación de su mes (un archivo que se vuelve a procesar, o lo que se restaura después de haberse presentado con él). En el registro va **con su fecha**; la pantalla del mes nuevo lo muestra en «Entraron de meses anteriores». Si cambia la fecha o el tipo en Revisar, el período se vuelve a decidir.

**A mano.** En Revisar, el período del libro se puede cambiar (usuario o más): AAAA-MM, no anterior al mes de la fecha. Queda como corrección, con quién y cuándo. Vacío vuelve al automático.

**¿Hasta cuándo se puede computar el crédito fiscal de un comprobante atrasado? No confirmado.** El artículo 12 de la Ley de IVA (t.o. 1997) dice que se resta «el gravamen que, en el período fiscal que se liquida, se les hubiera facturado»; no fija un plazo para computar en un período posterior lo que se recibió tarde, y no encontré una norma ni una respuesta oficial vigente de ARCA que lo fije (lo que aparece son opiniones de doctrina y un dictamen viejo que admite computarlo después). Por eso Factorum **no afirma un límite**: solo pasa el comprobante al primer período abierto y avisa. Si el comprobante es muy viejo, conviene consultarlo.

## No confirmado y ambigüedades

Lo que no pude confirmar en una fuente oficial, y lo que se decidió:

1. **Saltos de línea: no confirmado.** Ningún documento oficial dice si el separador es CRLF o LF. Se usa **CRLF**, el fin de línea de los archivos de texto «ANSI» de Windows que piden las Especificaciones; conviene confirmarlo con la primera importación real. Tampoco dice si el último registro lleva salto: lleva.
2. **Nombres de los archivos: no confirmado** que el aplicativo los exija. Se usan las denominaciones del diseño con `.txt`.
3. **Códigos numéricos de moneda: no confirmado.** La tabla del Libro IVA Digital escribe `2`, `60`; el campo es alfanumérico de 3 y la regla general diría «60» con un blanco a la derecha. Factorum escribe `060`, con tres dígitos, como los códigos de moneda de factura electrónica y como Factorum guarda el euro. Si el aplicativo rechazara el código, el cambio es una línea en `internal/libroiva/monedas.go`. Con `PES` y `DOL` no hay duda.
4. **Notas de crédito en positivo: deducido.** Las Especificaciones piden valores positivos y no hablan de notas de crédito; el tipo de comprobante dice que resta. No encontré un texto oficial que lo diga con esas palabras.
5. **Despacho de importación (campo 5).** El diseño lo define alfanumérico (que se completaría con blancos), pero las Especificaciones dicen «se completará con ceros». En compras se usan **ceros**; en importaciones, el número de despacho.
6. **Código de operación de B y C.** Las Especificaciones lo piden «si la alícuota de IVA es igual a cero» y en B y C no hay alícuotas. Va en **blanco** («no corresponde»), como en el diseño para exentos.
7. **Punto de venta.** Las Especificaciones piden «mayor o igual a 00001 y menor a 09998», pero el campo tiene 5 dígitos y hay puntos de venta electrónicos más altos. Un punto de venta de 9998 o más va con advertencia, no se bloquea.
8. **Archivo de alícuotas vacío** (todo B, C o tiques): no confirmado que el aplicativo lo acepte vacío; se baja vacío igual.
9. **Nombre del archivo de importaciones.** El diseño lo llama `LIBRO_IVA_DIGITAL_IMPORTACION_BIENES_ALICUOTA`, el índice de las Especificaciones `LIBRO_IVA_DIGITAL_COMPRAS_IMPORTACIONES` y el texto `LIBRO_IVA_DIGITAL_IMPORTACIONES`. Se usa el del Anexo I (ver «Qué archivos van»). La cabecera de importaciones se llama `LIBRO_IVA_DIGITAL_IMPORTACION_BIENES_CBTE.txt` por convención.
10. **Comprobantes de meses anteriores.** No confirmado que el aplicativo acepte sin observaciones, en un período, un comprobante de compra con fecha de un mes anterior (es la práctica, y es lo que hace Factorum con lo que llegó tarde). Tampoco el plazo para computar su crédito fiscal (ver «Períodos»).
11. **Prorrateo del crédito fiscal.** El campo 21 va igual al IVA (sin prorrateo). Si el cliente prorratea, se ajusta en el aplicativo.
12. **Formato del número de despacho.** La estructura año, aduana, tipo de destinación, número y control no está en el diseño ni en las Especificaciones del Libro IVA Digital, que solo piden 16 alfanuméricos. Factorum solo avisa si no la tiene.
13. **Identificación del vendedor en un despacho (campos 6 y 7).** Las Especificaciones piden el código `80` en todos los casos y, para personas del exterior sin documento argentino, «la tabla CUIT PAIS». Factorum pide la CUIT país del proveedor del exterior (11 dígitos que empiezan con 50, 51 o 55, sin controlarla contra la tabla, que no se pudo bajar). No encontré un texto oficial que diga si para un despacho va la CUIT país del proveedor o la de la Aduana.
14. **Columnas por alícuota de Mis Comprobantes.** Arcanum lee las posiciones de las alícuotas del JSON de Mis Comprobantes deduciéndolas del orden de las columnas del CSV de la misma pantalla; no se verificaron en vivo. Por eso solo se usan si cierran con los totales (si no, el comprobante queda sin detalle).
15. **Códigos de tributos de WSFEv1.** El reparto de los tributos de la emisión propia (1 nacionales, 2 provinciales, 3 municipales, 4 internos, 5 IIBB, 6 percepción de IVA, 7 percepción de IIBB, 8 percepciones municipales, 9 otras percepciones, 13 percepción de IVA a no categorizados, 99 otros) sale de la tabla de tributos de factura electrónica (`FEParamGetTiposTributos`); no se consultó en vivo para esta versión.
16. **Fecha de vencimiento o pago (ventas, campo 22).** Es opcional salvo en los tipos 017 y 018 (que Factorum no arma); sin dato va en ceros, como cualquier campo numérico opcional.
17. **Exportaciones.** No encontré cómo quiere el aplicativo el importe de una factura E que solo trae el total (si como exento o sin repartir). Va solo el total, con código `X`.

## Qué quedó afuera

- Importación de servicios, TurIVA, bienes usados (049), liquidaciones (033, 058, 059, 060, 063, 331, 332) y comprobantes anulados de ventas.
- Ventas de sujetos exentos (su propio diseño de registro) y facturas C en ventas.
- Prorrateo del crédito fiscal (el campo 21 va igual al IVA).
- Un plazo para computar el crédito fiscal de lo atrasado (no confirmado: ver «Períodos»).
- Cambiar el cliente de un lote no vuelve a decidir el período de sus comprobantes (lo hace el próximo cambio de fecha o tipo en Revisar).

