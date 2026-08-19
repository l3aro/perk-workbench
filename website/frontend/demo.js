/* Live terminal demo: bridges xterm.js to /ws/tui, which runs the real
 * Perk Workbench TUI against the embedded Chinook SQLite demo, read-only. */
import { Terminal } from '@xterm/xterm';
import '@xterm/xterm/css/xterm.css';

(function () {
  var container = document.getElementById('demo-terminal');
  if (!container) { return; }

  // DejaVuSansM Nerd Font Mono at 14px: advance 8.43px, line 16.8px.
  var cell = { width: 8.43, height: 16.8 };

  var term = new Terminal({
    cols: 80,
    rows: 24,
    cursorBlink: true,
    fontFamily: '"DejaVuSansM Nerd Font Mono", monospace',
    fontSize: 14,
    lineHeight: 1.2,
    theme: {
      background: '#10141b',
      foreground: '#d7dee8',
      cursor: '#d7dee8',
      selectionBackground: '#334155'
    }
  });
  term.open(container);
  window.demoTerm = term;

  function fit() {
    var cols = Math.max(40, Math.min(200, Math.floor((container.clientWidth - 4) / cell.width)));
    var rows = Math.max(12, Math.min(60, Math.floor((container.clientHeight - 4) / cell.height)));
    if (cols !== term.cols || rows !== term.rows) { term.resize(cols, rows); }
  }
  fit();

  // Fullscreen toggle: CSS overlay (position:fixed) instead of the Fullscreen
  // API, so ESC can't exit — the button is the only way out. fit() reflows the
  // TUI to the new size (term.resize re-sends resize to the server via onResize).
  var fullscreenEl = document.getElementById('demo-fullscreen');
  var fullscreenBtn = document.getElementById('demo-fullscreen-btn');
  if (fullscreenEl && fullscreenBtn) {
    function setFullscreen(active) {
      fullscreenEl.classList.toggle('is-fullscreen', active);
      document.body.style.overflow = active ? 'hidden' : '';
      fullscreenBtn.textContent = active ? 'Exit fullscreen' : 'Fullscreen';
      requestAnimationFrame(function () {
        fit();
        term.focus();
      });
    }
    fullscreenBtn.addEventListener('click', function () {
      setFullscreen(!fullscreenEl.classList.contains('is-fullscreen'));
    });
    window.addEventListener('resize', function () {
      if (fullscreenEl.classList.contains('is-fullscreen')) {
        fit();
      }
    });
  }

  var protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  var ws = new WebSocket(protocol + '//' + location.host + '/ws/tui');
  var connected = false;     // socket currently usable
  var everConnected = false; // was ever usable (for the disconnect message)
  var readyTimer = null;

  function showStatus(message) {
    var status = document.createElement('p');
    status.className = 'note demo-terminal-status';
    status.textContent = message;
    container.after(status);
  }

  ws.onopen = function () {
    connected = true;
    everConnected = true;
    ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
    term.focus();
  };
  ws.onmessage = function (event) { term.write(event.data); };
  ws.onclose = function () {
    connected = false;
    if (readyTimer) { clearInterval(readyTimer); }
    if (everConnected) { showStatus('Live demo disconnected. Refresh to reconnect.'); }
  };
  ws.onerror = function () {
    if (!everConnected) { showStatus('Live demo unavailable right now.'); }
  };
  term.onData(function (data) {
    if (connected) { ws.send(JSON.stringify({ type: 'input', data: data })); }
  });
  term.onResize(function (size) {
    if (connected) { ws.send(JSON.stringify({ type: 'resize', cols: size.cols, rows: size.rows })); }
  });

  // The TUI starts focused on the schema sidebar in vim normal mode. Once the
  // ready screen renders, focus the query editor and enter insert mode so
  // typing a query works immediately. READONLY appears in the footer in both
  // the full and compact (narrow) layouts; pane titles do not.
  readyTimer = setInterval(function () {
    if (!connected || window.demoTermFocused) { return; }
    var found = false;
    for (var i = 0; i < term.buffer.active.length; i++) {
      if (term.buffer.active.getLine(i).translateToString(true).indexOf('READONLY') >= 0) {
        found = true;
        break;
      }
    }
    if (found) {
      window.demoTermFocused = true;
      clearInterval(readyTimer);
      ws.send(JSON.stringify({ type: 'input', data: '\ti' }));
    }
  }, 400);
})();