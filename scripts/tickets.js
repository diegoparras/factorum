'use strict';
// Genera tickets de compra de prueba (tique y tique factura de controlador fiscal) como imágenes:
// HTML en papel térmico angosto (58 u 80 mm, letra monoespaciada) que se saca con Playwright.
// De cada uno sale una versión limpia (PNG) y otra degradada como una foto de celular (JPG:
// fondo gris, papel torcido, desenfoque, menos resolución y ruido).
//
// Uso: node scripts/tickets.js testdata/tickets
//
// Los CUIT son de fantasía (con dígito verificador válido). Los importes cierran: neto + IVA = total.
const fs = require('fs');
const path = require('path');
const { chromium } = require('playwright');

const out = process.argv[2] || 'testdata/tickets';
fs.mkdirSync(out, { recursive: true });

// Formato de importe argentino: 1.234,50
const ar = (n) => n.toLocaleString('es-AR', { minimumFractionDigits: 2, maximumFractionDigits: 2 }).replace(/ /g, ' ');
// Una fila con texto a la izquierda y a la derecha, en `ancho` columnas.
const lr = (izq, der, ancho) => izq + ' '.repeat(Math.max(1, ancho - izq.length - der.length)) + der;
const centro = (s, ancho) => ' '.repeat(Math.max(0, Math.floor((ancho - s.length) / 2))) + s;
const linea = (ancho) => '-'.repeat(ancho);

const tickets = [
  {
    // Supermercado, tique factura B con detalle de IVA (10,5% y 21%). 80 mm, 48 columnas.
    nombre: '01_super_tfb',
    mm: 80,
    lineas: (w) => [
      centro('LA ECONOMIA', w),
      centro('SUPERMERCADOS LA ECONOMIA S.A.', w),
      centro('Av. Rivadavia 5200 - CABA', w),
      centro('C.U.I.T. Nro.: 30-71222333-9', w),
      centro('IVA RESPONSABLE INSCRIPTO', w),
      centro('Ing. Brutos: 901-555222-1', w),
      centro('Inicio de actividades: 01/03/2012', w),
      linea(w),
      'TIQUE FACTURA B Nro 0005-00012345',
      lr('Fecha: 21/11/2026', 'Hora: 13:45:10', w),
      'A CONSUMIDOR FINAL',
      linea(w),
      'LECHE ENTERA 1L (10,50)',
      lr('  2 x 1.150,00', '2.300,00', w),
      lr('PAN LACTAL 550G (10,50)', '2.890,00', w),
      lr('ACEITE GIRASOL 1,5L (10,50)', '4.200,00', w),
      lr('DETERGENTE 750ML (21,00)', '1.815,00', w),
      'GASEOSA COLA 2,25L (21,00)',
      lr('  2 x 2.420,00', '4.840,00', w),
      linea(w),
      lr('SUBTOTAL', ar(16045), w),
      lr('NETO GRAVADO', ar(13997.74), w),
      lr('IVA 21,00%', ar(1155), w),
      lr('IVA 10,50%', ar(892.26), w),
      '',
      lr('TOTAL', '$ ' + ar(16045), w),
      '',
      lr('RECIBIMOS EFECTIVO', ar(20000), w),
      lr('VUELTO', ar(3955), w),
      linea(w),
      centro('GRACIAS POR SU COMPRA', w),
      lr('CAJA 03  CAJERO 117', 'P.V. 0005', w),
      centro('REG. CF: AB 12345678', w),
    ],
  },
  {
    // Estación de servicio YPF, tique a consumidor final (83). 58 mm, 32 columnas.
    nombre: '02_ypf_tique',
    mm: 58,
    lineas: (w) => [
      centro('YPF', w),
      centro('EST. DE SERVICIO EL TREBOL', w),
      centro('ESTACION DE SERVICIO', w),
      centro('EL TREBOL S.R.L.', w),
      centro('Ruta 8 Km 54 - Pilar', w),
      'CUIT: 30-70888888-1',
      'IVA RESPONSABLE INSCRIPTO',
      linea(w),
      centro('TIQUE', w),
      'P.V. Nro.: 0003',
      'Nro. T.: 00045678',
      lr('Fecha: 22/11/26', 'Hora: 08:12:45', w),
      'CF: A CONSUMIDOR FINAL',
      linea(w),
      'NAFTA SUPER (21,00)',
      lr(' 35,250 L x 1.250,00', ar(44062.5), w),
      linea(w),
      lr('TOTAL', '$' + ar(44062.5), w),
      lr('IVA CONTENIDO', ar(7647.13), w),
      lr('TARJETA DEBITO', ar(44062.5), w),
      linea(w),
      centro('SURTIDOR 4 - PLAYERO 22', w),
    ],
  },
  {
    // Farmacia de una monotributista, tique factura C. 58 mm, 32 columnas.
    nombre: '03_farmacia_tfc',
    mm: 58,
    lineas: (w) => [
      centro('FARMACIA DEL PUEBLO', w),
      centro('de GONZALEZ ANA MARIA', w),
      centro('Belgrano 120 - Lujan', w),
      'C.U.I.T. Nro.: 27-27272727-4',
      'RESPONSABLE MONOTRIBUTO',
      linea(w),
      'TIQUE FACTURA C',
      'Nro 0002-00000789',
      lr('Fecha: 23/11/2026', '19:03:22', w),
      'A CONSUMIDOR FINAL',
      linea(w),
      'IBUPROFENO 400 X10',
      lr(' 1 x 3.450,00', '3.450,00', w),
      'ALCOHOL 70 500ML',
      lr(' 1 x 2.100,00', '2.100,00', w),
      linea(w),
      lr('TOTAL', '$ ' + ar(5550), w),
      lr('RECIBI(MOS)', ar(10000), w),
      lr('SU VUELTO', ar(4450), w),
      linea(w),
      centro('CONSERVE ESTE TICKET', w),
    ],
  },
  {
    // Parrilla, tique factura A a un responsable inscripto. 80 mm, 48 columnas.
    nombre: '04_parrilla_tfa',
    mm: 80,
    lineas: (w) => [
      centro('LA PARRILLA DE DON TITO S.R.L.', w),
      centro('Av. Cabildo 2100 - CABA', w),
      centro('CUIT: 30-71555444-1', w),
      centro('IVA RESPONSABLE INSCRIPTO', w),
      linea(w),
      lr('TIQUE FACTURA A', 'P.V. Nro.: 0004', w),
      lr('Nro. T.: 00001234', '', w),
      lr('Fecha: 20/11/26', 'Hora: 21:30:05', w),
      linea(w),
      'GOMEZ MARIA LAURA',
      'CUIT: 20-22222222-3',
      'IVA RESPONSABLE INSCRIPTO',
      linea(w),
      'BIFE DE CHORIZO (21,00)',
      lr('  2 x 15.000,00', '30.000,00', w),
      lr('ENSALADA MIXTA (21,00)', '6.000,00', w),
      'AGUA MINERAL 500ML (21,00)',
      lr('  2 x 2.000,00', '4.000,00', w),
      linea(w),
      lr('SUBTOTAL', ar(40000), w),
      lr('NETO GRAVADO', ar(40000), w),
      lr('IVA 21%', ar(8400), w),
      lr('T O T A L', '$ ' + ar(48400), w),
      lr('TARJETA CREDITO VISA', ar(48400), w),
      linea(w),
      centro('MESA 12 - MOZO 3', w),
    ],
  },
];

function html(t, foto) {
  const ancho = t.mm === 58 ? 32 : 48;
  // 58 mm = 219 px CSS, 80 mm = 302 px CSS; la letra llena el ancho con las columnas del papel.
  const px = t.mm === 58 ? 219 : 302;
  const fuente = (px - 16) / ancho / 0.6; // DejaVu Sans Mono: 0,6 em por caracter
  const texto = t.lineas(ancho).map((l) => l.replace(/&/g, '&amp;').replace(/</g, '&lt;')).join('\n');
  const papel = `font-family: 'DejaVu Sans Mono', monospace; font-size: ${fuente.toFixed(2)}px; line-height: 1.25;
    white-space: pre; width: ${px - 16}px; padding: 14px 8px 24px; color: #1d1d1d;`;
  if (!foto) {
    return `<html><body style="margin:0;background:#fff"><div id="t" style="${papel} background:#fff">${texto}</div></body></html>`;
  }
  // Foto de celular: mesa gris con luz despareja, papel térmico amarillento, un poco torcido y
  // desenfocado, con ruido encima.
  const ruido = `<svg xmlns='http://www.w3.org/2000/svg' width='300' height='300'><filter id='n'><feTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='2'/></filter><rect width='100%' height='100%' filter='url(%23n)' opacity='0.35'/></svg>`;
  return `<html><body style="margin:0;background:radial-gradient(ellipse at 30% 20%, #a9a9a4, #6d6d69 75%);padding:40px 46px">
    <div id="t" style="${papel} background:linear-gradient(170deg,#f3efe4,#dcd7ca); transform: rotate(${t.mm === 58 ? 1.4 : -1.1}deg);
      filter: blur(0.55px) contrast(0.85); box-shadow: 3px 5px 12px rgba(0,0,0,.45); color:#3a3a3a">${texto}</div>
    <div style="position:fixed;inset:0;background:url(&quot;data:image/svg+xml,${ruido}&quot;);mix-blend-mode:multiply;opacity:.5"></div>
  </body></html>`;
}

(async () => {
  const browser = await chromium.launch();
  for (const t of tickets) {
    for (const foto of [false, true]) {
      // La versión limpia sale a 2,5x (unos 200 dpi, como el papel); la foto, a 1,6x.
      const ctx = await browser.newContext({ deviceScaleFactor: foto ? 1.6 : 2.5, viewport: { width: 420, height: 400 } });
      const page = await ctx.newPage();
      await page.setContent(html(t, foto));
      const archivo = path.join(out, t.nombre + (foto ? '_foto.jpg' : '.png'));
      if (foto) {
        await page.screenshot({ path: archivo, fullPage: true, type: 'jpeg', quality: 62 });
      } else {
        await page.locator('#t').screenshot({ path: archivo });
      }
      await ctx.close();
      console.log(archivo);
    }
  }
  await browser.close();
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
