/* Live terminal demo: bridges xterm.js to /ws/tui, which runs the real
 * Perk Workbench TUI against the embedded Chinook SQLite demo, read-only. */
import { Terminal } from '@xterm/xterm';
import '@xterm/xterm/css/xterm.css';

(function () {
  var container = document.getElementById('demo-terminal');
  if (!container) { return; }

  // DejaVuSansM Nerd Font Mono at 14px: advance 8.43px, line 16.8px.
  var cell = { width: 8.43, height: 16.8 };
  var terminalThemes = {
    dark: {
      background: '#0b0e14',
      foreground: '#d7dee8',
      cursor: '#d7dee8',
      selectionBackground: '#334155'
    },
    light: {
      background: '#f6f8fa',
      foreground: '#24292f',
      cursor: '#24292f',
      selectionBackground: '#c8d1dc'
    }
  };

  function activeTheme() {
    return document.documentElement.dataset.theme === 'light' ? 'light' : 'dark';
  }

  var term = new Terminal({
    cols: 80,
    rows: 24,
    cursorBlink: true,
    fontFamily: '"DejaVuSansM Nerd Font Mono", monospace',
    fontSize: 14,
    lineHeight: 1.2,
    theme: terminalThemes[activeTheme()]
  });
  term.open(container);
  window.demoTerm = term;

  function fit() {
    var cols = Math.max(40, Math.min(200, Math.floor((container.clientWidth - 4) / cell.width)));
    var rows = Math.max(12, Math.min(60, Math.floor((container.clientHeight - 4) / cell.height)));
    if (cols !== term.cols || rows !== term.rows) { term.resize(cols, rows); }
  }
  fit();

  // Fullscreen toggle: a utility-class overlay (position: fixed) instead of
  // the Fullscreen API, so ESC can't exit — the button is the only way out.
  // fit() reflows the TUI to the new size (term.resize re-sends resize to the
  // server via onResize).
  var fullscreenEl = document.getElementById('demo-fullscreen');
  var fullscreenBtn = document.getElementById('demo-fullscreen-btn');
  var fullscreenClasses = [
    'fixed', 'inset-0', 'z-50', 'flex', 'flex-col', 'gap-3', 'mt-0', 'p-3',
    'overflow-hidden', 'bg-canvas'
  ];
  var terminalFullscreenClasses = ['flex-1', 'h-auto', 'border-0', 'rounded-none'];
  if (fullscreenEl && fullscreenBtn) {
    function setFullscreen(active) {
      fullscreenEl.classList.toggle('mt-12', !active);
      fullscreenClasses.forEach(function (className) {
        fullscreenEl.classList.toggle(className, active);
      });
      terminalFullscreenClasses.forEach(function (className) {
        container.classList.toggle(className, active);
      });
      fullscreenEl.dataset.fullscreen = String(active);
      fullscreenBtn.setAttribute('aria-pressed', String(active));
      document.body.style.overflow = active ? 'hidden' : '';
      fullscreenBtn.textContent = active ? 'Exit fullscreen' : 'Fullscreen';
      requestAnimationFrame(function () {
        fit();
        term.focus();
      });
    }
    fullscreenBtn.addEventListener('click', function () {
      setFullscreen(fullscreenEl.dataset.fullscreen !== 'true');
    });
    window.addEventListener('resize', function () {
      if (fullscreenEl.dataset.fullscreen === 'true') {
        fit();
      }
    });
  }

  var protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  var ws = null;
  var connected = false;     // socket currently usable
  var everConnected = false; // was ever usable (for the disconnect message)
  var reconnectOnClose = false;
  var readyTimer = null;
  var terminalTheme = activeTheme();

  function showStatus(message) {
    var status = document.createElement('p');
    status.className = 'mt-4 text-sm text-muted';
    status.dataset.demoTerminalStatus = '';
    status.textContent = message;
    container.after(status);
  }

  function connect() {
    if (ws && ws.readyState !== WebSocket.CLOSED && ws.readyState !== WebSocket.CLOSING) {
      return;
    }
    var socket = new WebSocket(protocol + '//' + location.host + '/ws/tui?theme=' + terminalTheme);
    ws = socket;
    startReadyPolling();
    socket.onopen = function () {
      if (socket !== ws) return;
      connected = true;
      everConnected = true;
      socket.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
      term.focus();
    };
    socket.onmessage = function (event) {
      if (socket === ws) { term.write(event.data); }
    };
    socket.onclose = function () {
      if (socket !== ws) return;
      ws = null;
      connected = false;
      var shouldReconnect = reconnectOnClose;
      reconnectOnClose = false;
      clearInterval(readyTimer);
      if (shouldReconnect) {
        connect();
        return;
      }
      if (everConnected) { showStatus('Live demo disconnected. Refresh to reconnect.'); }
    };
    socket.onerror = function () {
      if (socket === ws && !everConnected && !reconnectOnClose) {
        showStatus('Live demo unavailable right now.');
      }
    };
  }

  term.onData(function (data) {
    if (connected && ws) { ws.send(JSON.stringify({ type: 'input', data: data })); }
  });
  term.onResize(function (size) {
    if (connected && ws) { ws.send(JSON.stringify({ type: 'resize', cols: size.cols, rows: size.rows })); }
  });

  function startReadyPolling() {
    clearInterval(readyTimer);
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
        if (ws) { ws.send(JSON.stringify({ type: 'input', data: '\ti' })); }
      }
    }, 400);
  }

  window.addEventListener('themechange', function (event) {
    var nextTheme = event.detail === 'light' || event.detail === 'dark'
      ? event.detail
      : activeTheme();
    if (nextTheme === terminalTheme) return;
    terminalTheme = nextTheme;
    term.options.theme = terminalThemes[terminalTheme];
    window.demoTermFocused = false;
    term.reset();

    if (ws && ws.readyState !== WebSocket.CLOSED) {
      reconnectOnClose = true;
      if (ws.readyState !== WebSocket.CLOSING) ws.close();
      return;
    }
    reconnectOnClose = false;
    connect();
  });

  // The TUI starts focused on the schema sidebar in vim normal mode. Once the
  // ready screen renders, focus the query editor and enter insert mode so
  // typing a query works immediately. READONLY appears in the footer in both
  // the full and compact (narrow) layouts; pane titles do not.
  connect();
})();