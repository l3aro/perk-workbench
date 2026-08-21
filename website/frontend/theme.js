/* Theme bootstrap: applies the saved (or system-preferred) theme before
 * first paint. Loaded synchronously from <head> so the correct `data-theme`
 * is set before the stylesheet renders; the toggle in app.js flips the same
 * attribute and persists it. Kept as an external file so page HTML never
 * contains a literal <script> element (see TestSearchEscapesQuery). */
(function () {
  var saved = localStorage.getItem('theme');
  var theme = saved === 'light' || saved === 'dark'
    ? saved
    : (matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark');
  document.documentElement.dataset.theme = theme;
})();
