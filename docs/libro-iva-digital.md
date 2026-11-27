# Libro IVA Digital: exportar las compras

Factorum arma los archivos de importación de las **compras** del Libro IVA Digital de ARCA (RG 4597 y modificatorias) para un cliente (CUIT) y un mes: el de comprobantes y el de alícuotas. Este documento dice qué diseño de registro se usó, de dónde salió, cómo se completa cada campo con lo que Factorum guarda y qué no se pudo confirmar.

El código está en `internal/libroiva` (puro, sin base ni red), con golden files en `internal/libroiva/testdata`.

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

Desde noviembre de 2025 la declaración jurada de IVA se presenta por **IVA Simple**; la registración de las operaciones (el ex Libro IVA Digital) es su «Módulo Registración Electrónica de Operaciones», y la página de ARCA remite a los mismos diseños de registro de arriba para importar compras.

## Qué archivos van

| Archivo | Diseño | Longitud | Lo arma Factorum |
|---|---|---|---|
| `LIBRO_IVA_DIGITAL_COMPRAS_CBTE` | «Compras e Importación de Bienes - Cabecera» | 325 | Sí, un registro por comprobante |
| `LIBRO_IVA_DIGITAL_COMPRAS_ALICUOTAS` | «Compras - Alícuotas» | 84 | Sí, un registro por alícuota de cada A o M |
| `LIBRO_IVA_DIGITAL_IMPORTACION_BIENES_ALICUOTA` | «Importaciones de Bienes - Alícuotas» | 50 | **No** (ver abajo) |
| `LIBRO_IVA_DIGITAL_IMPORTACION_SERVICIOS_CREDITO_FISCAL` | «Importaciones de Servicios» | 211 | **No** |

**Importaciones: no.** El archivo de importaciones de bienes es para los despachos de importación (tipo 066), que se registran con la fecha de oficialización y el número de despacho en lugar de la factura del exterior («En el caso de importación de bienes, no se registrará la Factura del exterior, sino el Despacho de Importación»). Factorum lee facturas, notas y tiques de ARCA; no lee despachos ni guarda su número, así que no tiene con qué armarlo. Si aparece un comprobante con un tipo que Factorum no conoce (un 066 cargado a mano, por ejemplo), queda como problema «Tipo de comprobante que no va» y no se exporta. La importación de servicios tampoco: necesita el pago del impuesto (fecha, monto e identificación de la constancia), que no está en el comprobante.

**Nombres dentro del ZIP.** `LIBRO_IVA_DIGITAL_COMPRAS_CBTE.txt` y `LIBRO_IVA_DIGITAL_COMPRAS_ALICUOTAS.txt`: la «denominación del archivo» de cada diseño más `.txt`. El manual dice que en «Importar archivos» el sistema pide los dos archivos (uno de comprobantes y otro de alícuotas); no encontré que exija un nombre, así que el nombre es una convención (ver «No confirmado»). Además va `resumen.csv`, que es para la persona y no se importa.

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
| 5 | Despacho de importación | 37 | 52 | 16 | 3 | - | 16 ceros («Este campo se completará con ceros», Especificaciones) |
| 6 | Código de documento del vendedor | 53 | 54 | 2 | 2 | - | `80` (CUIT), obligatorio en compras |
| 7 | Número de identificación del vendedor | 55 | 74 | 20 | 3 | ceros izq. | CUIT del emisor |
| 8 | Apellido y nombre o denominación del vendedor | 75 | 104 | 30 | 3 | blancos der. | Razón social, recortada a 30 (ver «Texto») |
| 9 | Importe total de la operación | 105 | 119 | 15 | 2 | ceros izq. | Total del comprobante |
| 10 | Conceptos que no integran el precio neto gravado | 120 | 134 | 15 | 2 | ceros izq. | No gravado (solo A y M) |
| 11 | Importe de operaciones exentas | 135 | 149 | 15 | 2 | ceros izq. | Exento (solo A y M) |
| 12 | Percepciones o pagos a cuenta de IVA | 150 | 164 | 15 | 2 | ceros izq. | 0 (ver «Otros tributos») |
| 13 | Percepciones o pagos a cuenta de otros impuestos nacionales | 165 | 179 | 15 | 2 | ceros izq. | 0 |
| 14 | Percepciones de Ingresos Brutos | 180 | 194 | 15 | 2 | ceros izq. | 0 |
| 15 | Percepciones de impuestos municipales | 195 | 209 | 15 | 2 | ceros izq. | 0 |
| 16 | Impuestos internos | 210 | 224 | 15 | 2 | ceros izq. | 0 |
| 17 | Código de moneda | 225 | 227 | 3 | 3 | - | `PES`, `DOL`, `060`... (ver «Moneda») |
| 18 | Tipo de cambio | 228 | 237 | 10 | 2 | ceros izq. | 4 enteros y 6 decimales; `0001000000` en pesos («deberá completarse aún cuando el campo 17 sea igual a 'PES'») |
| 19 | Cantidad de alícuotas de IVA | 238 | 238 | 1 | 2 | - | Registros de alícuotas; 0 en B, C y tiques sin letra A o M |
| 20 | Código de operación | 239 | 239 | 1 | 1 | - | `E`, `N` o blanco (ver abajo) |
| 21 | Crédito Fiscal Computable | 240 | 254 | 15 | 2 | ceros izq. | IVA total en A y M; 0 en el resto |
| 22 | Otros Tributos | 255 | 269 | 15 | 2 | ceros izq. | Otros tributos del comprobante |
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

**Códigos de operación (compras):** `A` no alcanzado, blanco o `0` no corresponde, `C` canje, `D` devolución IVA turistas, `E` exentas, `N` no gravado, `T` reintegro Decreto 1043/2016, `X` importación del exterior, `Z` importación de zona franca. Factorum usa `E`, `N` y blanco.

**Documento:** `80` CUIT (las compras lo exigen salvo bienes usados, no registrables o material reciclado).

**Monedas:** `PES` pesos, `DOL` dólar estadounidense y códigos numéricos (`2` dólar libre, `12` real, `60` euro, etc.). Factorum reconoce los de la tabla, además de los ISO más comunes (`USD`, `EUR`, `BRL`...), y escribe los numéricos con tres dígitos (`060`).

**Tipos de comprobante:** la tabla trae todos los de compras. Los que Factorum lee: 001 a 013 y 015 (A, B y C), 051 a 054 (M), 081, 082, 083 y 109 a 120 (tiques), 201 a 213 (FCE MiPyME).

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

Factorum guarda los otros tributos **sin detalle** (no sabe si son percepción de IVA, de Ingresos Brutos, municipal o impuesto interno). Van completos en el campo 22, «Otros Tributos» («el total de tributos no informados en los otros campos»), y los campos 12 a 16 van en 0. Cada comprobante con otros tributos aparece con la advertencia **«Otros tributos sin detalle»**: hay que pasarlos a su campo en el Libro IVA Digital, sobre todo las **percepciones de IVA**, que solo se computan como pago a cuenta en el campo 12.

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

**Qué va.** «Todos» exporta todo lo que no bloquea, con sus advertencias. «Solo los listos» exporta lo que está **bien o aprobado** y no tiene advertencias fuertes. Los comprobantes sin fecha del cliente aparecen como problema (no se puede saber si son del mes). Lo que está en la papelera no cuenta. La conciliación se pide a Arcanum solo para avisar: si Arcanum no está o no tiene el período, la pantalla lo dice y los archivos se arman igual.

## No confirmado y ambigüedades

Lo que no pude confirmar en una fuente oficial, y lo que se decidió:

1. **Saltos de línea: no confirmado.** Ningún documento oficial dice si el separador es CRLF o LF. Se usa **CRLF**, el fin de línea de los archivos de texto «ANSI» de Windows que piden las Especificaciones; conviene confirmarlo con la primera importación real. Tampoco dice si el último registro lleva salto: lleva.
2. **Nombres de los archivos: no confirmado** que el aplicativo los exija. Se usan las denominaciones del diseño con `.txt`.
3. **Códigos numéricos de moneda: no confirmado.** La tabla del Libro IVA Digital escribe `2`, `60`; el campo es alfanumérico de 3 y la regla general diría «60» con un blanco a la derecha. Factorum escribe `060`, con tres dígitos, como los códigos de moneda de factura electrónica y como Factorum guarda el euro. Si el aplicativo rechazara el código, el cambio es una línea en `internal/libroiva/monedas.go`. Con `PES` y `DOL` no hay duda.
4. **Notas de crédito en positivo: deducido.** Las Especificaciones piden valores positivos y no hablan de notas de crédito; el tipo de comprobante dice que resta. No encontré un texto oficial que lo diga con esas palabras.
5. **Despacho de importación (campo 5).** El diseño lo define alfanumérico (que se completaría con blancos), pero las Especificaciones dicen «se completará con ceros». Se usan **ceros**.
6. **Código de operación de B y C.** Las Especificaciones lo piden «si la alícuota de IVA es igual a cero» y en B y C no hay alícuotas. Va en **blanco** («no corresponde»), como en el diseño para exentos.
7. **Punto de venta.** Las Especificaciones piden «mayor o igual a 00001 y menor a 09998», pero el campo tiene 5 dígitos y hay puntos de venta electrónicos más altos. Un punto de venta de 9998 o más va con advertencia, no se bloquea.
8. **Archivo de alícuotas vacío** (todo B, C o tiques): no confirmado que el aplicativo lo acepte vacío; se baja vacío igual.
9. **Nombre del archivo de importaciones.** El diseño lo llama `LIBRO_IVA_DIGITAL_IMPORTACION_BIENES_ALICUOTA`, el índice de las Especificaciones `LIBRO_IVA_DIGITAL_COMPRAS_IMPORTACIONES` y el texto `LIBRO_IVA_DIGITAL_IMPORTACIONES`. No afecta: Factorum no lo arma.
10. **Comprobantes de meses anteriores.** En compras, un comprobante que llegó tarde se puede registrar en el período en que se recibe. Factorum toma el mes **por fecha de emisión**: lo de meses anteriores no aparece. Queda para una versión siguiente (marcar «ya exportado» y elegir el período de registración).
11. **Prorrateo del crédito fiscal.** El campo 21 va igual al IVA (sin prorrateo). Si el cliente prorratea, se ajusta en el aplicativo.

## Qué quedó afuera

- Ventas (Factorum lee compras).
- Importación de bienes y de servicios, TurIVA, bienes usados (049) y liquidaciones (033, 058, 059, 060, 063, 331, 332).
- Separar los otros tributos en percepciones de IVA, Ganancias, Ingresos Brutos, municipales e internos: Factorum no tiene ese detalle.
- Recordar qué se exportó y en qué período.
