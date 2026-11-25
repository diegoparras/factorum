// Tema y estado del menú antes de pintar (sin parpadeo). Va aparte porque la CSP no permite scripts inline.
(function () {
  try {
    var t = localStorage.getItem('factorum.tema') || (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
    document.documentElement.setAttribute('data-theme', t);
    if (localStorage.getItem('factorum.nav') === 'min') document.documentElement.setAttribute('data-nav', 'min');
  } catch (e) { /* sin almacenamiento: tema claro */ }
})();
