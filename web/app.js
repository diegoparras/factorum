'use strict';
// Factorum: interfaz sin frameworks ni build. Sin scripts ni handlers inline (CSP): todo por delegación.

const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const soloDig = (s) => String(s || '').replace(/\D/g, '');

// ---------- Íconos (SVG line, sin emojis: regla de la suite) ----------
const svg = (d) => `<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${d}</svg>`;
const I = {
  verificar: svg('<path d="M12 3l7 3v6c0 4.5-3 7.5-7 9-4-1.5-7-4.5-7-9V6z"/><path d="M9 12l2 2 4-4"/>'),
  subir: svg('<path d="M12 16V4M7 9l5-5 5 5"/><path d="M4 20h16"/>'),
  lotes: svg('<rect x="4" y="4" width="16" height="5" rx="1.5"/><rect x="4" y="11" width="16" height="9" rx="1.5"/>'),
  comprobantes: svg('<path d="M6 3h9l3 3v15l-3-2-3 2-3-2-3 2z"/><path d="M9 9h6M9 13h6"/>'),
  resumen: svg('<path d="M4 20V10M10 20V4M16 20v-7M22 20H2"/>'),
  canales: svg('<path d="M21 12a8 8 0 0 1-11.5 7.2L4 21l1.8-5.5A8 8 0 1 1 21 12z"/>'),
  ojo: svg('<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/>'),
  ojoNo: svg('<path d="M3 3l18 18"/><path d="M10.6 5.1A10 10 0 0 1 12 5c6.5 0 10 7 10 7a17 17 0 0 1-3 3.9M6.6 6.6C3.8 8.4 2 12 2 12s3.5 7 10 7a9.8 9.8 0 0 0 5.4-1.6"/><path d="M9.9 9.9a3 3 0 0 0 4.2 4.2"/>'),
  sol: svg('<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>'),
  luna: svg('<path d="M21 13A9 9 0 1 1 11 3a7 7 0 0 0 10 10z"/>'),
  salir: svg('<path d="M15 4h4v16h-4M10 8l-4 4 4 4M6 12h11"/>'),
  info: svg('<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/>'),
  inicio: svg('<path d="M4 11l8-7 8 7"/><path d="M6 10v10h12V10"/><path d="M10 20v-6h4v6"/>'),
  revisar: svg('<path d="M4 13l3-8h10l3 8"/><path d="M4 13v6h16v-6"/><path d="M4 13h5l1 2h4l1-2h5"/>'),
  novedades: svg('<path d="M12 3l2.2 5.4L20 9l-4.4 3.8L17 19l-5-3-5 3 1.4-6.2L4 9l5.8-.6z"/>'),
  check: svg('<path d="M5 12.5l4.5 4.5L19 7.5"/>'),
  cruz: svg('<path d="M7 7l10 10M17 7L7 17"/>'),
  alerta: svg('<path d="M12 4l9 16H3z"/><path d="M12 10v4M12 17h.01"/>'),
  doc: svg('<path d="M7 3h7l4 4v14H7z"/><path d="M14 3v4h4"/><path d="M10 12h5M10 16h5"/>'),
};

const NAV = [
  ['Trabajo', [['inicio', 'Inicio'], ['revisar', 'Revisar'], ['verificar', 'Verificar'], ['subir', 'Cargar facturas']]],
  ['Datos', [['lotes', 'Lotes'], ['comprobantes', 'Comprobantes'], ['resumen', 'Resumen']]],
  ['Configuración', [['canales', 'Canales']]],
];
const VISTAS = NAV.flatMap((g) => g[1]);
const PENDIENTES = { error: 0, revisar: 0 };

// ---------- Estado ----------
let CLAVE = '';
let SALUD = null;
let vista = 'inicio';
const filtro = { estado: '', q: '', cliente: '', desde: '', hasta: '', lote: '', offset: 0, limite: 100 };
let listado = []; // comprobantes de la tabla, para Anterior / Siguiente
let rev = null; // { idx, cbte, original }
let timerLotes = null;
let timerCanales = null;

function leerClave() {
  try { return sessionStorage.getItem('factorum.clave') || localStorage.getItem('factorum.clave') || ''; } catch { return ''; }
}
function guardarClave(c, recordar) {
  try {
    sessionStorage.setItem('factorum.clave', c);
    if (recordar) localStorage.setItem('factorum.clave', c); else localStorage.removeItem('factorum.clave');
  } catch { /* sin almacenamiento */ }
}
function borrarClave() {
  try { sessionStorage.removeItem('factorum.clave'); localStorage.removeItem('factorum.clave'); } catch { /* nada */ }
}

// ---------- API ----------
async function api(ruta, opts = {}) {
  const h = Object.assign({ 'X-API-Key': CLAVE }, opts.headers || {});
  if (opts.json !== undefined) { h['Content-Type'] = 'application/json'; opts.body = JSON.stringify(opts.json); }
  const r = await fetch(ruta, { method: opts.method || 'GET', headers: h, body: opts.body });
  if (r.status === 401) { salir(); throw new Error('La clave no es válida'); }
  if (opts.binario) {
    if (!r.ok) throw new Error(`Error ${r.status}`);
    return r.blob();
  }
  const d = await r.json().catch(() => ({}));
  if (!r.ok || d.ok === false) throw new Error(d.error || `Error ${r.status}`);
  return d;
}

function toast(msg, err) {
  const t = $('#toast');
  t.textContent = msg;
  t.className = 'toast' + (err ? ' err' : '');
  clearTimeout(toast.t);
  toast.t = setTimeout(() => t.classList.add('hidden'), err ? 6000 : 3000);
}

// ---------- Formatos ----------
const pesos = (v) => {
  const n = Number(v || 0);
  return n.toLocaleString('es-AR', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
};
const fecha = (s) => (s ? String(s).slice(0, 10).split('-').reverse().join('/') : '');
const cuit = (s) => { const d = soloDig(s); return d.length === 11 ? `${d.slice(0, 2)}-${d.slice(2, 10)}-${d.slice(10)}` : (s || ''); };
const TIPOS = { 1: 'Factura A', 2: 'Nota de Débito A', 3: 'Nota de Crédito A', 6: 'Factura B', 7: 'Nota de Débito B', 8: 'Nota de Crédito B',
  11: 'Factura C', 12: 'Nota de Débito C', 13: 'Nota de Crédito C', 19: 'Factura E', 51: 'Factura M', 201: 'FCE MiPyME A', 206: 'FCE MiPyME B', 211: 'FCE MiPyME C' };
const nombreCbte = (c) => `${TIPOS[c.tipo] || 'Comprobante ' + (c.tipo || '')} ${String(c.punto_venta || 0).padStart(5, '0')}-${String(c.numero || 0).padStart(8, '0')}`;
const ETQ = { ok: 'Bien', revisar: 'Revisar', error: 'Con errores', aprobado: 'Aprobado',
  valida: 'Válida', invalida: 'No válida', apocrifa: 'Emisor apócrifo', con_errores: 'Con errores', no_verificable: 'No verificable',
  tique_controlado: 'Ticket controlado' };
const SELLO = { valida: 'check', tique_controlado: 'check', invalida: 'cruz', apocrifa: 'cruz', con_errores: 'alerta', no_verificable: 'alerta' };
const estado = (e) => `<span class="est ${esc(e)}">${esc(ETQ[e] || e)}</span>`;
const ORIGEN = { qr: 'QR', texto: 'texto', ocr: 'OCR', vision: 'IA', manual: 'manual', arcanum: 'ARCA', calculo: 'cálculo' };

function hallazgosHTML(h) {
  if (!h || !h.length) return '';
  return `<ul class="hall">${h.map((x) => `<li class="${esc(x.nivel)}">${esc(x.mensaje)}</li>`).join('')}</ul>`;
}

// ---------- Navegación ----------
function pintarNav() {
  const pend = PENDIENTES.error + PENDIENTES.revisar;
  $('#nav').innerHTML = NAV.map(([grupo, items]) => `<div class="nav-g">${grupo}</div>` + items.map(([id, t]) => {
    const badge = id === 'revisar' && pend ? `<span class="nav-badge${PENDIENTES.error ? ' err' : ''}" aria-label="${pend} pendientes">${pend > 999 ? '999+' : pend}</span>` : '';
    return `<button type="button" class="nav-it${id === vista ? ' active' : ''}" data-accion="ir" data-vista="${id}" title="${t}" aria-label="${t}"${id === vista ? ' aria-current="page"' : ''}>${I[id]}<span>${t}</span>${badge}</button>`;
  }).join('')).join('');
}

async function actualizarPendientes() {
  try {
    const [e, r] = await Promise.all([api('/api/comprobantes?estado=error&limite=1'), api('/api/comprobantes?estado=revisar&limite=1')]);
    PENDIENTES.error = e.total; PENDIENTES.revisar = r.total;
    pintarNav();
    const ce = $('#cntError'); if (ce) ce.textContent = e.total;
    const cr = $('#cntRevisar'); if (cr) cr.textContent = r.total;
  } catch { /* sin conexión: se reintenta en la próxima acción */ }
}

function ir(v) {
  vista = v;
  $$('.vista').forEach((s) => s.classList.toggle('hidden', s.id !== 'v-' + v));
  pintarNav();
  document.documentElement.removeAttribute('data-nav-movil');
  clearInterval(timerLotes); clearInterval(timerCanales);
  if (v === 'lotes') { cargarLotes(); timerLotes = setInterval(cargarLotes, 3000); }
  if (v === 'comprobantes') cargarCbtes();
  if (v === 'resumen') cargarResumen();
  if (v === 'canales') { cargarCanales(); timerCanales = setInterval(cargarCanales, 5000); }
  if (v === 'inicio') cargarInicio();
  if (v === 'revisar') cargarBandeja();
  const nom = (VISTAS.find(([id]) => id === v) || [])[1] || '';
  $('#crumb').textContent = v === 'inicio' ? '' : '/ ' + nom;
  document.title = v === 'inicio' ? 'Factorum' : `${nom} · Factorum`;
  try { history.replaceState(null, '', '#' + v); } catch { /* nada */ }
}

function toggleNav() {
  if (matchMedia('(max-width: 760px)').matches) {
    const el = document.documentElement;
    if (el.getAttribute('data-nav-movil') === 'abierto') el.removeAttribute('data-nav-movil'); else el.setAttribute('data-nav-movil', 'abierto');
    return;
  }
  const min = document.documentElement.getAttribute('data-nav') === 'min';
  if (min) document.documentElement.removeAttribute('data-nav'); else document.documentElement.setAttribute('data-nav', 'min');
  try { localStorage.setItem('factorum.nav', min ? '' : 'min'); } catch { /* nada */ }
}

function pintarMenu() {
  const dark = document.documentElement.getAttribute('data-theme') === 'dark';
  const s = SALUD || {};
  $('#menu').innerHTML = `
    <div class="menu-user">Factorum ${esc(s.version || '')}</div>
    <div class="menu-sep"></div>
    <button class="menu-item" data-accion="tema">${dark ? I.sol : I.luna}<span>${dark ? 'Tema claro' : 'Tema oscuro'}</span></button>
    <button class="menu-item" data-accion="novedades">${I.novedades}<span>Novedades</span></button>
    <button class="menu-item" data-accion="acerca">${I.info}<span>Acerca de Factorum</span></button>
    <div class="menu-sep"></div>
    <button class="menu-item" data-accion="salir">${I.salir}<span>Cerrar sesión</span></button>`;
}

async function cargarSalud() {
  try {
    const r = await fetch('/healthz');
    SALUD = await r.json();
    const chip = $('#saludChip');
    chip.textContent = SALUD.base ? '' : 'Sin conexión con la base';
    chip.classList.toggle('mal', !SALUD.base);
    $('#loteConstatar').closest('label').classList.toggle('hidden', !SALUD.arcanum);
    $('#loteIA').closest('label').classList.toggle('hidden', !(SALUD.ia && SALUD.ia.habilitada));
    $('#iaVerificar').closest('label').classList.toggle('hidden', !(SALUD.ia && SALUD.ia.habilitada));
  } catch { /* se reintenta en la próxima acción */ }
}

// ---------- Login ----------
function mostrarLogin(err) {
  $('#login').classList.remove('hidden');
  $('#loginErr').textContent = err || '';
  $('#loginErr').classList.toggle('hidden', !err);
  setTimeout(() => $('#clave').focus(), 50);
}

async function entrar(c, recordar) {
  CLAVE = c;
  try {
    await api('/api/lotes?limite=1');
    guardarClave(c, recordar);
    $('#login').classList.add('hidden');
    await cargarSalud();
    const pedida = location.hash.slice(1);
    ir(VISTAS.some(([id]) => id === pedida) ? pedida : 'inicio');
    actualizarPendientes();
    verNovedades(false);
  } catch (e) {
    CLAVE = '';
    mostrarLogin(e.message);
  }
}

function salir() {
  CLAVE = '';
  borrarClave();
  clearInterval(timerLotes); clearInterval(timerCanales);
  mostrarLogin();
}

// ---------- Verificar ----------
async function verificar(ev) {
  ev.preventDefault();
  const f = $('#archVerificar').files[0];
  if (!f) return;
  const fd = new FormData();
  fd.append('archivo', f);
  if (soloDig($('#cuitConsultante').value)) fd.append('cuit_consultante', soloDig($('#cuitConsultante').value));
  fd.append('ia', $('#iaVerificar').checked ? 'true' : 'false');
  const btn = $('#fVerificar button[type=submit]');
  btn.disabled = true; btn.textContent = 'Verificando...';
  try {
    const r = await api('/api/verificar', { method: 'POST', body: fd });
    pintarVerificacion(r);
  } catch (e) {
    toast(e.message, true);
  } finally {
    btn.disabled = false; btn.textContent = 'Verificar';
  }
}

function pintarVerificacion(r) {
  const res = r.resultado || {};
  const cbs = res.comprobantes || [];
  const kons = r.constatacion || [];
  const filas = cbs.map((rc, i) => {
    const c = rc.comprobante;
    const k = kons[i];
    const v = (r.comprobantes || [])[i];
    return `<div class="card">
      <div class="fila" style="justify-content:space-between"><strong>${esc(nombreCbte(c))}</strong>${v ? estado(v.veredicto) : estado(rc.estado)}</div>
      <dl class="kv">
        <dt>Emisor</dt><dd>${esc(c.razon_social_emisor || '')} <span class="mono">${esc(cuit(c.cuit_emisor))}</span></dd>
        <dt>Fecha</dt><dd>${fecha(c.fecha)}</dd>
        <dt>Total</dt><dd class="mono">$ ${pesos(c.total)} ${c.moneda && c.moneda !== 'PES' ? esc(c.moneda) : ''}</dd>
        <dt>CAE</dt><dd class="mono">${esc(c.cae || 'no encontrado')} ${c.vto_cae ? '(vence ' + fecha(c.vto_cae) + ')' : ''}</dd>
        ${k && k.resultado ? `<dt>ARCA</dt><dd>${k.valido ? 'Constatado: el comprobante existe con estos datos.' : 'Rechazado: ' + esc((k.observaciones || []).map((o) => o.message).join(' '))}</dd>` : ''}
        ${k && k.apoc && k.apoc.consultado ? `<dt>Apócrifos</dt><dd>${k.apoc.esApocrifo ? '<strong class="est error">El emisor figura como apócrifo</strong>' : 'El emisor no figura en la base'}</dd>` : ''}
        <dt>Leído con</dt><dd>${(res.extractores || []).map((x) => `<span class="origen ${esc(x)}">${esc(ORIGEN[x] || x)}</span>`).join(' ')} <span class="muted">${res.duracion_ms} ms</span></dd>
      </dl>
      ${hallazgosHTML(rc.hallazgos.filter((h) => h.nivel !== 'info' || h.codigo === 'constatado'))}
    </div>`;
  }).join('');
  $('#resVerificar').innerHTML = `<div class="veredicto ${esc(r.veredicto)}"><div class="sello">${I[SELLO[r.veredicto] || 'alerta']}</div><div><div class="titulo">${esc(ETQ[r.veredicto] || r.veredicto)}</div><div>${esc(r.mensaje)}</div></div></div>${filas}`;
}

// ---------- Subir ----------
async function subir(ev) {
  ev.preventDefault();
  const files = $('#archSubir').files;
  if (!files.length) return;
  const fd = new FormData();
  for (const f of files) fd.append('archivos', f);
  fd.append('nombre', $('#loteNombre').value.trim());
  fd.append('cliente_cuit', soloDig($('#loteCliente').value));
  fd.append('ia', $('#loteIA').checked ? 'true' : 'false');
  fd.append('constatar', $('#loteConstatar').checked ? 'true' : 'false');
  const prog = $('#progSubir');
  prog.classList.remove('hidden');
  const btn = $('#fSubir button[type=submit]');
  btn.disabled = true;
  // XHR para tener el progreso de la subida (fetch no lo da).
  const xhr = new XMLHttpRequest();
  xhr.open('POST', '/api/lotes');
  xhr.setRequestHeader('X-API-Key', CLAVE);
  xhr.upload.onprogress = (e) => { if (e.lengthComputable) prog.firstElementChild.style.width = `${Math.round((e.loaded / e.total) * 100)}%`; };
  xhr.onload = () => {
    btn.disabled = false;
    prog.classList.add('hidden');
    prog.firstElementChild.style.width = '0';
    let d = {};
    try { d = JSON.parse(xhr.responseText); } catch { /* nada */ }
    if (xhr.status >= 300 || !d.ok) { toast(d.error || `Error ${xhr.status}`, true); return; }
    let msg = `Lote ${d.lote.id}: ${d.recibidos} archivo(s) recibidos.`;
    if (d.repetidos) msg += ` ${d.repetidos} ya estaban cargados.`;
    if (d.descartados && d.descartados.length) msg += ` Se omitieron: ${d.descartados.join(', ')}.`;
    toast(msg);
    $('#fSubir').reset();
    ir('lotes');
  };
  xhr.onerror = () => { btn.disabled = false; prog.classList.add('hidden'); toast('No se pudo subir', true); };
  xhr.send(fd);
}

// ---------- Lotes ----------
async function cargarLotes() {
  try {
    const { lotes } = await api('/api/lotes?limite=50');
    if (!lotes.length) { $('#tablaLotes').innerHTML = vacio('doc', 'Todavía no hay lotes', 'Cada carga de facturas (desde acá, por chat, por MCP o desde Arcanum) aparece como un lote.', 'subir', 'Cargar facturas'); return; }
    $('#tablaLotes').innerHTML = `<div class="tabla-scroll"><table class="t"><thead><tr><th>Lote</th><th>Origen</th><th>Cliente</th><th>Creado</th><th>Progreso</th><th>Bien</th><th>Revisar</th><th>Errores</th><th></th></tr></thead><tbody>
      ${lotes.map((l) => {
        const pct = l.documentos ? Math.round((l.procesados / l.documentos) * 100) : 100;
        const c = l.comprobantes || {};
        return `<tr class="clic" data-accion="verLote" data-id="${l.id}"><td><strong>${l.id}</strong> ${esc(l.nombre)}</td><td>${esc(l.origen)}</td><td class="mono">${esc(cuit(l.cliente_cuit))}</td>
          <td>${new Date(l.creado).toLocaleString('es-AR', { dateStyle: 'short', timeStyle: 'short' })}</td>
          <td><span class="prog mini-prog"><div style="width:${pct}%"></div></span> <span class="muted">${l.procesados}/${l.documentos}</span>${l.con_error ? ` <span class="est error">${l.con_error} ilegibles</span>` : ''}</td>
          <td class="num">${(c.ok || 0) + (c.aprobado || 0)}</td><td class="num">${c.revisar || 0}</td><td class="num">${c.error || 0}</td>
          <td><button class="ghost mini" data-accion="exportarLote" data-id="${l.id}">Excel</button></td></tr>`;
      }).join('')}</tbody></table></div>`;
  } catch (e) { toast(e.message, true); }
}

// ---------- Comprobantes ----------
function qsFiltro(extra = {}) {
  const p = new URLSearchParams();
  const f = Object.assign({}, filtro, extra);
  for (const k of ['estado', 'q', 'cliente', 'desde', 'hasta', 'lote', 'limite', 'offset']) if (f[k] !== '' && f[k] !== undefined && f[k] !== null) p.set(k, f[k]);
  return p.toString();
}

function leerFiltro() {
  filtro.estado = $('#fEstado').value;
  filtro.q = $('#fQ').value.trim();
  filtro.cliente = soloDig($('#fCliente').value);
  filtro.desde = $('#fDesde').value;
  filtro.hasta = $('#fHasta').value;
  filtro.lote = soloDig($('#fLote').value);
}

function pintarFiltro() {
  $('#fEstado').value = filtro.estado; $('#fQ').value = filtro.q; $('#fCliente').value = filtro.cliente;
  $('#fDesde').value = filtro.desde; $('#fHasta').value = filtro.hasta; $('#fLote').value = filtro.lote;
}

async function cargarCbtes() {
  pintarFiltro();
  try {
    const { comprobantes, total } = await api('/api/comprobantes?' + qsFiltro());
    listado = comprobantes;
    if (!comprobantes.length) { $('#tablaCbtes').innerHTML = vacio('doc', 'No hay comprobantes con estos filtros', 'Probá con otro estado, otras fechas o borrando la búsqueda.'); $('#pager').innerHTML = ''; return; }
    $('#tablaCbtes').innerHTML = `<div class="tabla-scroll"><table class="t"><thead><tr><th>Fecha</th><th>Comprobante</th><th>Emisor</th><th class="num">Neto</th><th class="num">IVA</th><th class="num">Total</th><th>Estado</th><th>Observaciones</th></tr></thead><tbody>
      ${comprobantes.map((c, i) => {
        const iva = ['iva_27', 'iva_21', 'iva_10_5', 'iva_5', 'iva_2_5'].reduce((s, k) => s + Number(c[k] || 0), 0);
        const obs = (c.hallazgos || []).filter((h) => h.nivel !== 'info').map((h) => h.mensaje)[0] || '';
        return `<tr class="clic" data-accion="revisar" data-idx="${i}"><td>${fecha(c.fecha)}</td><td>${esc(nombreCbte(c))}</td>
          <td>${esc(c.razon_social_emisor || '')}<div class="muted mono">${esc(cuit(c.cuit_emisor))}</div></td>
          <td class="num">${pesos(c.neto_gravado)}</td><td class="num">${pesos(iva)}</td><td class="num">${pesos(c.total)}${c.moneda && c.moneda !== 'PES' ? ' ' + esc(c.moneda) : ''}</td>
          <td>${estado(c.estado)}</td><td class="muted">${esc(obs)}</td></tr>`;
      }).join('')}</tbody></table></div>`;
    const desde = filtro.offset + 1, hasta = filtro.offset + comprobantes.length;
    $('#pager').innerHTML = `<span>${desde} a ${hasta} de ${total}</span>
      ${filtro.offset > 0 ? '<button class="ghost mini" data-accion="pagina" data-dir="-1">Anteriores</button>' : ''}
      ${hasta < total ? '<button class="ghost mini" data-accion="pagina" data-dir="1">Siguientes</button>' : ''}`;
  } catch (e) { toast(e.message, true); }
}

async function descargar(ruta, nombre) {
  try {
    const b = await api(ruta, { binario: true });
    const u = URL.createObjectURL(b);
    const a = document.createElement('a');
    a.href = u; a.download = nombre;
    document.body.appendChild(a); a.click(); a.remove();
    setTimeout(() => URL.revokeObjectURL(u), 10000);
  } catch (e) { toast(e.message, true); }
}

// ---------- Revisión ----------
const CAMPOS = [
  ['fecha', 'Fecha', 'date'], ['tipo', 'Código de tipo'], ['punto_venta', 'Punto de venta'], ['numero', 'Número'],
  ['cuit_emisor', 'CUIT emisor'], ['razon_social_emisor', 'Razón social emisor', 'texto', true], ['condicion_iva_emisor', 'Condición IVA emisor', 'texto', true],
  ['doc_nro_receptor', 'Doc. receptor'], ['razon_social_receptor', 'Razón social receptor'],
  ['neto_gravado', 'Neto gravado', 'm'], ['no_gravado', 'No gravado', 'm'], ['exento', 'Exento', 'm'],
  ['iva_21', 'IVA 21%', 'm'], ['iva_10_5', 'IVA 10,5%', 'm'], ['iva_27', 'IVA 27%', 'm'], ['iva_5', 'IVA 5%', 'm'], ['iva_2_5', 'IVA 2,5%', 'm'],
  ['otros_tributos', 'Otros tributos', 'm'], ['subtotal', 'Subtotal (B y C)', 'm'], ['total', 'Total', 'm'],
  ['moneda', 'Moneda'], ['cotizacion', 'Cotización'], ['cae', 'CAE'], ['vto_cae', 'Vto. CAE', 'date'],
];

async function abrirRevision(idx) {
  if (idx < 0 || idx >= listado.length) return;
  const id = listado[idx].id;
  try {
    const { comprobante: c, correcciones } = await api('/api/comprobantes/' + id);
    if (rev && rev.url) URL.revokeObjectURL(rev.url);
    rev = { idx, cbte: c, url: null };
    $('#revision').classList.remove('hidden');
    $('#revTitulo').textContent = `${nombreCbte(c)}  ·  ${c.archivo}`;
    $('#revPos').textContent = `${idx + 1} de ${listado.length}`;
    $('#revHallazgos').innerHTML = `<div class="fila" style="justify-content:space-between;margin-bottom:8px">${estado(c.estado)}<span class="muted">confianza ${Math.round((c.confianza || 0) * 100)}%</span></div>${hallazgosHTML(c.hallazgos)}`;
    $('#revForm').innerHTML = CAMPOS.map(([k, t, tipo, ancho]) => {
      const o = (c.origenes || {})[k];
      const v = tipo === 'm' ? (Number(c[k] || 0) ? Number(c[k]).toFixed(2) : '') : (c[k] ?? '');
      return `<label class="${ancho ? 'ancho' : ''}"><span class="et"><span>${t}</span>${o ? `<span class="origen ${esc(o)}" title="Origen del dato">${esc(ORIGEN[o] || o)}</span>` : ''}</span>
        <input name="${k}" value="${esc(v === 0 ? '' : v)}" data-orig="${esc(v === 0 ? '' : v)}" ${tipo === 'date' ? 'type="date"' : ''} class="${tipo === 'm' ? 'num' : ''}" inputmode="${tipo === 'm' ? 'decimal' : 'text'}"></label>`;
    }).join('');
    $('#btnConstatar').classList.toggle('hidden', !(SALUD && SALUD.arcanum));
    const k = c.constatacion;
    $('#revConstatacion').innerHTML = k ? `<div class="card"><strong>Constatación ARCA</strong><dl class="kv">
      <dt>Resultado</dt><dd>${k.valido === true ? 'Aprobado (el comprobante existe)' : k.valido === false ? 'Rechazado' : esc(k.error || 'sin respuesta')}</dd>
      ${(k.observaciones || []).length ? `<dt>Observaciones</dt><dd>${esc(k.observaciones.map((o) => o.message).join(' '))}</dd>` : ''}
      ${k.apoc && k.apoc.consultado ? `<dt>Apócrifos</dt><dd>${k.apoc.esApocrifo ? 'El emisor figura como apócrifo' : 'No figura'}</dd>` : ''}
      <dt>Fecha</dt><dd>${k.fecha ? new Date(k.fecha).toLocaleString('es-AR') : ''}</dd></dl></div>` : '';
    const det = $('#revCorrecciones');
    det.classList.toggle('hidden', !correcciones.length);
    det.lastElementChild.innerHTML = correcciones.length ? `<table class="t"><thead><tr><th>Campo</th><th>Antes</th><th>Después</th><th>Quién</th><th>Cuándo</th></tr></thead><tbody>${correcciones.map((x) =>
      `<tr><td>${esc(x.campo)}</td><td class="mono">${esc(x.antes)}</td><td class="mono">${esc(x.despues)}</td><td>${esc(x.usuario)}</td><td>${new Date(x.creado).toLocaleString('es-AR')}</td></tr>`).join('')}</tbody></table>` : '';
    // Original a la izquierda.
    $('#revDoc').innerHTML = '<p class="muted" style="padding:16px">Cargando el archivo...</p>';
    const pag = (c.paginas || [1])[0];
    const b = await api(`/api/documentos/${c.documento_id}/pagina/${pag}`, { binario: true });
    rev.url = URL.createObjectURL(b);
    $('#revDoc').innerHTML = `<div class="rev-doc-barra"><span class="muted">${esc(c.archivo)}${(c.paginas || []).length ? ', página ' + pag : ''}</span>
      <button class="ghost mini" data-accion="abrirOriginal" data-doc="${c.documento_id}">Abrir el original</button></div>
      <div class="rev-doc-img"><img src="${rev.url}" alt="Comprobante original"></div>`;
  } catch (e) { toast(e.message, true); }
}

function cerrarRevision() {
  $('#revision').classList.add('hidden');
  if (rev && rev.url) URL.revokeObjectURL(rev.url);
  rev = null;
  cargarCbtes();
}

function cambiosRevision() {
  const campos = {};
  $$('#revForm input').forEach((i) => { if (i.value !== i.dataset.orig) campos[i.name] = i.value; });
  return campos;
}

async function guardarRevision(aprobar) {
  if (!rev) return;
  const campos = cambiosRevision();
  if (!aprobar && !Object.keys(campos).length) { toast('No hay cambios'); return; }
  try {
    const { comprobante } = await api('/api/comprobantes/' + rev.cbte.id, { method: 'PATCH', json: { campos, aprobar, usuario: 'panel' } });
    listado[rev.idx] = comprobante;
    actualizarPendientes();
    if (aprobar) {
      toast('Aprobado');
      if (rev.idx + 1 < listado.length) abrirRevision(rev.idx + 1); else cerrarRevision();
    } else {
      toast('Guardado');
      abrirRevision(rev.idx);
    }
  } catch (e) { toast(e.message, true); }
}

async function constatarRevision() {
  if (!rev) return;
  try {
    await api(`/api/comprobantes/${rev.cbte.id}/constatar`, { method: 'POST' });
    toast('Constatado');
    abrirRevision(rev.idx);
  } catch (e) { toast(e.message, true); }
}

// ---------- Resumen ----------
async function cargarResumen() {
  try {
    const { resumen: r } = await api('/api/resumen?' + qsFiltro({ limite: '', offset: '' }));
    const fila = (t, v, fuerte) => `<tr><td>${fuerte ? `<strong>${t}</strong>` : t}</td><td class="num">${fuerte ? `<strong>$ ${pesos(v)}</strong>` : '$ ' + pesos(v)}</td></tr>`;
    $('#resumen').innerHTML = `<div class="fila" style="align-items:flex-start">
      <table class="t" style="max-width:460px"><tbody>
        <tr><td>Comprobantes</td><td class="num">${r.cantidad}</td></tr>
        ${fila('Neto gravado', r.neto_gravado)}${fila('No gravado', r.no_gravado)}${fila('Exento', r.exento)}
        ${fila('IVA 21%', r.iva_21)}${fila('IVA 10,5%', r.iva_10_5)}${fila('IVA 27%', r.iva_27)}${fila('IVA 5%', r.iva_5)}${fila('IVA 2,5%', r.iva_2_5)}
        ${fila('IVA total', r.iva_total, true)}${fila('Otros tributos', r.otros_tributos)}${fila('Subtotal B y C', r.subtotal_b_c)}${fila('Total', r.total, true)}
      </tbody></table>
      <div class="card" style="flex:1;min-width:260px"><strong>Estados</strong><dl class="kv">
        ${Object.entries(r.por_estado || {}).map(([k, v]) => `<dt>${estado(k)}</dt><dd>${v}</dd>`).join('')}
        <dt>Moneda extranjera</dt><dd>${r.moneda_extranjera} (no sumados)</dd></dl>
        <strong>Tipos</strong><dl class="kv">${Object.entries(r.por_tipo || {}).map(([k, v]) => `<dt>${esc(k)}</dt><dd>${v}</dd>`).join('')}</dl>
        <div class="fila"><button class="ghost" data-accion="exportar" data-formato="xlsx">Descargar Excel</button><button class="ghost" data-accion="exportar" data-formato="csv">Descargar CSV</button></div>
      </div></div>`;
  } catch (e) { toast(e.message, true); }
}

// ---------- Canales ----------
async function cargarCanales() {
  try {
    const { canales } = await api('/api/canales');
    const NOM = { telegram: 'Telegram', whatsapp: 'WhatsApp', webhook: 'Webhook (secretaria, Evolution API, n8n)' };
    const html = canales.map((c) => {
      let extra = '';
      if (c.canal === 'whatsapp' && c.habilitado && !c.conectado) {
        extra = c.qr ? `<div><p>Escaneá este código desde WhatsApp: Dispositivos vinculados, Vincular un dispositivo.</p><img class="qr" id="qrWa" alt="Código QR de vinculación"></div>`
          : '<p class="muted">Esperando el código de vinculación...</p>';
        extra += `<form class="fila" id="fVincular"><label>O vinculá con un código de 8 letras<input id="telVincular" placeholder="Teléfono con código de país, ej. 5491155550000" inputmode="numeric"></label><button class="ghost" type="submit">Pedir código</button></form>`;
      }
      if (c.canal === 'whatsapp' && c.conectado) extra = '<button class="ghost mini" data-accion="desvincularWa">Desvincular</button>';
      return `<div class="card"><div class="fila" style="justify-content:space-between"><strong>${NOM[c.canal] || esc(c.canal)}</strong>
        ${!c.habilitado ? '<span class="est">Sin configurar</span>' : c.conectado ? '<span class="est ok">Conectado</span>' : '<span class="est revisar">Desconectado</span>'}</div>
        ${c.detalle ? `<p class="muted">${esc(c.detalle)}</p>` : ''}${extra}</div>`;
    }).join('');
    // No repintar mientras se escribe el teléfono.
    if (document.activeElement && document.activeElement.id === 'telVincular') return;
    $('#canales').innerHTML = html;
    if ($('#qrWa')) {
      const b = await api('/api/canales/whatsapp/qr.png', { binario: true }).catch(() => null);
      if (b && $('#qrWa')) $('#qrWa').src = URL.createObjectURL(b);
    }
  } catch (e) { toast(e.message, true); }
}

// ---------- Estado vacío ----------
function vacio(ico, titulo, texto, vistaBoton, textoBoton) {
  return `<div class="vacio"><div class="ico">${I[ico] || I.doc}</div><h3>${esc(titulo)}</h3><p>${esc(texto)}</p>${vistaBoton ? `<button type="button" data-accion="ir" data-vista="${vistaBoton}">${esc(textoBoton)}</button>` : ''}</div>`;
}

// ---------- Inicio ----------
const mil = (n) => Number(n || 0).toLocaleString('es-AR');
async function cargarInicio() {
  try {
    const { panel: p } = await api('/api/panel');
    if (!p.total) {
      $('#inicioSub').textContent = 'Todavía no cargaste facturas. Así se empieza:';
      $('#inicio').innerHTML = `<div class="vacio"><div class="ico">${I.doc}</div><h3>Bienvenido a Factorum</h3>
        <p>Lee facturas de ARCA por QR y texto, controla que todo cierre y, con Arcanum, te dice si son legales.</p>
        <div class="pasos">
          <div class="paso"><span class="n">1</span><strong>Verificá una factura</strong><span class="muted">Subí un PDF o una foto y mirá qué lee y qué controla. No se guarda.</span><button type="button" class="ghost" data-accion="ir" data-vista="verificar">Verificar</button></div>
          <div class="paso"><span class="n">2</span><strong>Cargá un lote</strong><span class="muted">PDFs sueltos o un ZIP con cientos: se procesan en segundo plano.</span><button type="button" class="ghost" data-accion="ir" data-vista="subir">Cargar facturas</button></div>
          <div class="paso"><span class="n">3</span><strong>Conectá el chat</strong><span class="muted">WhatsApp o Telegram: tus clientes mandan la foto y queda cargada.</span><button type="button" class="ghost" data-accion="ir" data-vista="canales">Canales</button></div>
        </div></div>`;
      return;
    }
    $('#inicioSub').textContent = 'Cómo viene todo, de un vistazo.';
    const e = p.por_estado || {};
    const pend = (e.revisar || 0) + (e.error || 0);
    const sinIA = p.total ? Math.round((1 - p.con_ia / p.total) * 100) : 100;
    const kpi = (et, val, pie, cls, estado) => estado
      ? `<button type="button" class="kpi ${cls || ''}" data-accion="verEstado" data-estado="${estado}"><span class="et">${et}</span><span class="val">${val}</span><span class="kpi-pie">${pie}</span></button>`
      : `<div class="kpi ${cls || ''}"><span class="et">${et}</span><span class="val">${val}</span><span class="kpi-pie">${pie}</span></div>`;
    const max = Math.max(1, ...p.por_dia.map((d) => d.ok + d.revisar + d.error));
    const alto = (n) => (n ? Math.max(2, Math.round((n / max) * 100)) : 0);
    const barras = p.por_dia.map((d) => `<div class="b" title="${fecha(d.dia)}: ${d.ok} bien, ${d.revisar} para revisar, ${d.error} con errores">
      <i class="ok" style="height:${alto(d.ok)}%"></i><i class="rv" style="height:${alto(d.revisar)}%"></i><i class="er" style="height:${alto(d.error)}%"></i></div>`).join('');
    const ejes = p.por_dia.map((d, i) => `<span>${i % 2 ? '' : `${d.dia.slice(8, 10)}<small>/${d.dia.slice(5, 7)}</small>`}</span>`).join('');
    const lotes = p.lotes.length ? p.lotes.map((l) => {
      const c = l.comprobantes || {};
      const pct = l.documentos ? Math.round((l.procesados / l.documentos) * 100) : 100;
      return `<button type="button" class="it" data-accion="verLote" data-id="${l.id}"><span><span class="t1">${esc(l.nombre || 'Lote ' + l.id)}</span><br><span class="t2">${esc(l.origen)} · ${new Date(l.creado).toLocaleString('es-AR', { dateStyle: 'short', timeStyle: 'short' })}</span></span>
        <span class="t2">${l.estado === 'terminado' ? `${(c.ok || 0) + (c.aprobado || 0)} bien · ${c.revisar || 0} revisar · ${c.error || 0} error` : `<span class="prog mini-prog"><div style="width:${pct}%"></div></span> ${l.procesados}/${l.documentos}`}</span></button>`;
    }).join('') : '<p class="muted">Sin lotes todavía.</p>';
    $('#inicio').innerHTML = `<div class="kpis">
        ${kpi('Para revisar', mil(pend), pend ? 'tocá para verlos' : 'todo al día', e.error ? 'alerta' : pend ? 'aviso' : '', e.error ? 'error' : 'revisar')}
        ${kpi('Hoy', mil(p.hoy.comprobantes), `${mil(p.hoy.documentos)} archivos`)}
        ${kpi('Este mes', mil(p.mes.comprobantes), `${mil(p.mes.documentos)} archivos`)}
        ${kpi('Emisores apócrifos', mil(p.apocrifos), 'detectados con ARCA', p.apocrifos ? 'alerta' : '')}
        ${kpi('Posibles adulterados', mil(p.adulterados), 'texto distinto del QR', p.adulterados ? 'alerta' : '')}
        ${kpi('Leído sin IA', sinIA + '%', `${Math.round(p.duracion_promedio_ms)} ms por archivo, en promedio`)}
      </div>
      <div class="dos">
        <div class="card"><div class="card-cab"><h3>Últimos 14 días</h3><span class="leyenda"><span><i style="background:var(--accent)"></i>Bien</span><span><i style="background:var(--warn)"></i>Revisar</span><span><i style="background:var(--err)"></i>Errores</span></span></div>
          <div><div class="barras">${barras}</div><div class="ejes">${ejes}</div></div></div>
        <div class="card"><div class="card-cab"><h3>Últimos lotes</h3><button type="button" class="ghost mini" data-accion="ir" data-vista="lotes">Ver todos</button></div><div class="lista">${lotes}</div></div>
      </div>`;
  } catch (e) { toast(e.message, true); }
}

// ---------- Bandeja de revisión ----------
let bandejaEstado = 'error';
async function cargarBandeja() {
  $$('#bandejaTabs button').forEach((b) => b.classList.toggle('on', b.dataset.estado === bandejaEstado));
  try {
    const { comprobantes, total } = await api(`/api/comprobantes?estado=${bandejaEstado}&limite=200`);
    actualizarPendientes();
    listado = comprobantes;
    if (!comprobantes.length) {
      $('#bandeja').innerHTML = vacio('check', bandejaEstado === 'error' ? 'No hay comprobantes con errores' : 'No hay nada para revisar',
        'Todo lo cargado cerró solo o ya lo aprobaste.');
      return;
    }
    $('#bandeja').innerHTML = `<div class="bandeja">${comprobantes.map((c, i) => {
      const motivo = (c.hallazgos || []).find((h) => h.nivel === 'error') || (c.hallazgos || []).find((h) => h.nivel === 'aviso');
      return `<button type="button" class="bitem ${esc(c.estado)}" data-accion="abrirBandeja" data-idx="${i}"><span class="marca-e"></span>
        <span><span class="t1">${esc(c.razon_social_emisor || cuit(c.cuit_emisor))}</span> <span class="muted">· ${esc(nombreCbte(c))} · ${fecha(c.fecha)}</span><br>
        <span class="t2">${esc(motivo ? motivo.mensaje : '')}</span></span>
        <span class="monto">$ ${pesos(c.total)}</span></button>`;
    }).join('')}</div>${total > comprobantes.length ? `<p class="muted">Se muestran ${comprobantes.length} de ${total}.</p>` : ''}`;
  } catch (e) { toast(e.message, true); }
}

// ---------- Ventana: Novedades y Acerca de ----------
function abrirModal(titulo, html) {
  $('#modalTit').textContent = titulo;
  $('#modalCuerpo').innerHTML = html;
  $('#modal').classList.remove('hidden');
}
function cerrarModal() { $('#modal').classList.add('hidden'); }

// Markdown mínimo del CHANGELOG: ### títulos, - viñetas (con continuación), **negrita** y `código`.
function mdMini(md) {
  const inl = (t) => esc(t).replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>').replace(/`(.+?)`/g, '<code>$1</code>');
  const out = []; let enLista = false; let item = null;
  const cerrarItem = () => { if (item !== null) { out.push(`<li>${inl(item)}</li>`); item = null; } };
  for (const l of md.split('\n')) {
    if (/^###\s/.test(l)) { cerrarItem(); if (enLista) { out.push('</ul>'); enLista = false; } out.push(`<h4>${inl(l.replace(/^###\s/, ''))}</h4>`); }
    else if (/^- /.test(l)) { cerrarItem(); if (!enLista) { out.push('<ul>'); enLista = true; } item = l.slice(2); }
    else if (/^\s+\S/.test(l) && item !== null) item += ' ' + l.trim();
    else if (l.trim()) { cerrarItem(); if (enLista) { out.push('</ul>'); enLista = false; } out.push(`<p>${inl(l)}</p>`); }
  }
  cerrarItem(); if (enLista) out.push('</ul>');
  return out.join('');
}

// Al entrar después de una actualización, muestra lo nuevo desde la última versión vista.
async function verNovedades(todas) {
  try {
    const { novedades } = await fetch('/api/novedades').then((r) => r.json());
    if (!novedades || !novedades.length) return;
    let vista = '';
    try { vista = localStorage.getItem('factorum.version_vista') || ''; } catch { /* nada */ }
    const actual = novedades[0].version;
    const cmp = (a, b) => { const x = a.split('.').map(Number), y = b.split('.').map(Number); for (let i = 0; i < 3; i++) { if ((x[i] || 0) !== (y[i] || 0)) return (x[i] || 0) - (y[i] || 0); } return 0; };
    let lista = novedades;
    if (!todas) {
      if (vista && cmp(actual, vista) <= 0) return;
      lista = vista ? novedades.filter((n) => cmp(n.version, vista) > 0) : novedades.slice(0, 1);
    }
    try { localStorage.setItem('factorum.version_vista', actual); } catch { /* nada */ }
    abrirModal(todas ? 'Novedades' : `Factorum se actualizó a la ${actual}`,
      lista.map((n) => `<p><span class="ver">v${esc(n.version)}</span> <span class="muted">${esc(n.fecha || '')}</span></p>${mdMini(n.cuerpo)}`).join('<hr style="border:0;border-top:1px solid var(--border);margin:14px 0">'));
  } catch { /* sin novedades: no pasa nada */ }
}

function acercaDe() {
  const s = SALUD || {};
  const ia = s.ia && s.ia.habilitada ? `${esc(s.ia.modelo)}${s.ia.local ? ' (local)' : ' (externo: los comprobantes que la necesiten salen del servidor)'}` : 'apagada';
  abrirModal('Acerca de Factorum', `<div class="acerca"><img src="logo.svg" alt="" width="56" height="56"><div><strong style="font-size:17px;color:var(--text)">Factorum ${esc(s.version || '')}</strong><br>Lector de facturas de ARCA. Parte de la suite Escriba.</div></div>
    <p>Lee los comprobantes por su QR y su texto, sin depender de una IA; controla que todo cierre y, con Arcanum, los constata con ARCA.</p>
    <dl class="kv" style="margin-top:12px">
      <dt>OCR</dt><dd>${s.ocr ? 'Tesseract' : 'no instalado'}</dd>
      <dt>IA</dt><dd>${ia}</dd>
      <dt>Arcanum</dt><dd>${s.arcanum ? 'conectado' : 'sin configurar'}</dd>
      <dt>Archivos</dt><dd>${esc(s.almacen || '')}${s.cifrado ? ', cifrados' : ', sin cifrar'}</dd>
      <dt>Base</dt><dd>${s.base ? 'conectada' : 'sin conexión'}</dd>
    </dl>
    <p style="margin-top:12px"><a class="link" href="https://github.com/diegoparras/factorum" target="_blank" rel="noopener">Código y documentación</a></p>`);
}

// ---------- Eventos ----------
document.addEventListener('click', (ev) => {
  const el = ev.target.closest('[data-accion]');
  if (!el) {
    if (!ev.target.closest('.menu-wrap')) $('#menu').classList.add('hidden');
    return;
  }
  const a = el.dataset.accion;
  switch (a) {
    case 'ir': ir(el.dataset.vista); break;
    case 'nav': toggleNav(); break;
    case 'cerrarNav': document.documentElement.removeAttribute('data-nav-movil'); break;
    case 'menu': pintarMenu(); $('#menu').classList.toggle('hidden'); break;
    case 'tema': {
      const next = document.documentElement.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
      document.documentElement.setAttribute('data-theme', next);
      try { localStorage.setItem('factorum.tema', next); } catch { /* nada */ }
      pintarMenu();
      break;
    }
    case 'salir': $('#menu').classList.add('hidden'); salir(); break;
    case 'ojo': {
      const i = document.getElementById(el.dataset.para);
      i.type = i.type === 'password' ? 'text' : 'password';
      el.innerHTML = i.type === 'password' ? I.ojo : I.ojoNo;
      break;
    }
    case 'verLote': if (!ev.target.closest('[data-accion="exportarLote"]')) { Object.assign(filtro, { lote: el.dataset.id, estado: '', q: '', offset: 0 }); ir('comprobantes'); } break;
    case 'exportarLote': ev.stopPropagation(); descargar(`/api/export.xlsx?lote=${el.dataset.id}`, `factorum-lote-${el.dataset.id}.xlsx`); break;
    case 'exportar': leerFiltro(); descargar(`/api/export.${el.dataset.formato}?` + qsFiltro({ limite: '', offset: '' }), `factorum.${el.dataset.formato}`); break;
    case 'pagina': filtro.offset = Math.max(0, filtro.offset + Number(el.dataset.dir) * filtro.limite); cargarCbtes(); break;
    case 'revisar': abrirRevision(Number(el.dataset.idx)); break;
    case 'revAnterior': if (rev) abrirRevision(rev.idx - 1); break;
    case 'revSiguiente': if (rev) abrirRevision(rev.idx + 1); break;
    case 'revCerrar': cerrarRevision(); break;
    case 'bandeja': bandejaEstado = el.dataset.estado; cargarBandeja(); break;
    case 'abrirBandeja': abrirRevision(Number(el.dataset.idx)); break;
    case 'verEstado': Object.assign(filtro, { estado: el.dataset.estado, lote: '', q: '', offset: 0 }); ir('comprobantes'); break;
    case 'novedades': $('#menu').classList.add('hidden'); verNovedades(true); break;
    case 'acerca': $('#menu').classList.add('hidden'); acercaDe(); break;
    case 'modalCerrar': cerrarModal(); break;
    case 'modalFondo': if (ev.target === el) cerrarModal(); break;
    case 'abrirOriginal':
      api(`/api/documentos/${el.dataset.doc}/archivo`, { binario: true }).then((b) => {
        const u = URL.createObjectURL(b); window.open(u, '_blank', 'noopener'); setTimeout(() => URL.revokeObjectURL(u), 60000);
      }).catch((e) => toast(e.message, true));
      break;
    case 'revGuardar': guardarRevision(false); break;
    case 'revAprobar': guardarRevision(true); break;
    case 'revConstatar': constatarRevision(); break;
    case 'desvincularWa':
      if (confirm('¿Desvincular WhatsApp? Vas a tener que escanear el código otra vez.')) api('/api/canales/whatsapp/desvincular', { method: 'POST' }).then(cargarCanales).catch((e) => toast(e.message, true));
      break;
    default: break;
  }
});

document.addEventListener('submit', (ev) => {
  const f = ev.target;
  if (f.id === 'loginForm') { ev.preventDefault(); entrar($('#clave').value.trim(), $('#recordar').checked); }
  else if (f.id === 'fVerificar') verificar(ev);
  else if (f.id === 'fSubir') subir(ev);
  else if (f.id === 'fFiltro') { ev.preventDefault(); leerFiltro(); filtro.offset = 0; cargarCbtes(); }
  else if (f.id === 'revForm') { ev.preventDefault(); guardarRevision(false); }
  else if (f.id === 'fVincular') {
    ev.preventDefault();
    api('/api/canales/whatsapp/vincular', { method: 'POST', json: { telefono: soloDig($('#telVincular').value) } })
      .then((r) => toast(`Código: ${r.codigo}. Ingresalo en WhatsApp, Dispositivos vinculados, Vincular con número de teléfono.`))
      .catch((e) => toast(e.message, true));
  }
});

document.addEventListener('input', (ev) => {
  if (ev.target.closest('#revForm')) ev.target.classList.toggle('cambiado', ev.target.value !== ev.target.dataset.orig);
});

document.addEventListener('keydown', (ev) => {
  if (ev.key === 'Escape' && !$('#modal').classList.contains('hidden')) { cerrarModal(); return; }
  if (!rev || $('#revision').classList.contains('hidden')) return;
  if (ev.key === 'Escape') cerrarRevision();
  const escribiendo = ev.target.closest && ev.target.closest('input, textarea, select');
  if (!escribiendo && !ev.ctrlKey && !ev.metaKey && !ev.altKey) {
    const k = ev.key.toLowerCase();
    if (k === 'j') { ev.preventDefault(); abrirRevision(rev.idx + 1); return; }
    if (k === 'k') { ev.preventDefault(); abrirRevision(rev.idx - 1); return; }
    if (k === 'a') { ev.preventDefault(); guardarRevision(true); return; }
    if (k === 'e') { ev.preventDefault(); const i = $('#revForm input'); if (i) i.focus(); return; }
  }
  if (ev.altKey && ev.key === 'ArrowRight') abrirRevision(rev.idx + 1);
  if (ev.altKey && ev.key === 'ArrowLeft') abrirRevision(rev.idx - 1);
  if ((ev.ctrlKey || ev.metaKey) && ev.key === 'Enter') guardarRevision(true);
});

// Arrastrar y soltar, y lo elegido a la vista.
function tamanio(b) {
  if (b < 1024) return `${b} B`;
  if (b < 1048576) return `${Math.round(b / 1024)} KB`;
  return `${(b / 1048576).toLocaleString('es-AR', { maximumFractionDigits: 1 })} MB`;
}
function mostrarArchivos(z, input) {
  const files = [...input.files];
  const txt = z.querySelector('.zona-txt');
  const btn = z.querySelector('.zona-btn');
  z.querySelector('.zona-lista')?.remove();
  z.classList.toggle('con-archivos', files.length > 0);
  if (!files.length) {
    txt.textContent = txt.dataset.vacio;
    btn.textContent = input.multiple ? 'Elegir archivos' : 'Elegir archivo';
    return;
  }
  const total = files.reduce((a, f) => a + f.size, 0);
  txt.textContent = files.length === 1 ? '1 archivo listo' : `${files.length} archivos listos (${tamanio(total)})`;
  btn.textContent = input.multiple ? 'Cambiar archivos' : 'Cambiar archivo';
  const ul = document.createElement('ul');
  ul.className = 'zona-lista';
  const MAX = 5;
  for (const f of files.slice(0, MAX)) {
    const li = document.createElement('li');
    li.innerHTML = `<span class="nom">${esc(f.name)}</span><span class="tam">${tamanio(f.size)}</span>`;
    ul.append(li);
  }
  if (files.length > MAX) {
    const li = document.createElement('li');
    li.className = 'mas';
    li.textContent = `y ${files.length - MAX} más`;
    ul.append(li);
  }
  txt.after(ul);
}
for (const [zona, sel] of [['#zonaVerificar', '#archVerificar'], ['#zonaSubir', '#archSubir']]) {
  const z = $(zona);
  const input = $(sel);
  z.addEventListener('dragover', (e) => { e.preventDefault(); z.classList.add('sobre'); });
  z.addEventListener('dragleave', () => z.classList.remove('sobre'));
  z.addEventListener('drop', (e) => {
    e.preventDefault(); z.classList.remove('sobre');
    input.files = e.dataTransfer.files;
    mostrarArchivos(z, input);
  });
  input.addEventListener('change', () => mostrarArchivos(z, input));
  // Después de un reset el input queda vacío, pero el change no se dispara.
  input.form.addEventListener('reset', () => setTimeout(() => mostrarArchivos(z, input)));
}

// ---------- Arranque ----------
$('.pass-toggle').innerHTML = I.ojo;
pintarNav();
const guardada = leerClave();
if (guardada) entrar(guardada, !!(() => { try { return localStorage.getItem('factorum.clave'); } catch { return ''; } })());
else mostrarLogin();
