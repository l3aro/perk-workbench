/* Live terminal demo: bridges xterm.js to /ws/tui, which runs the real
 * Perk Workbench TUI against the embedded Chinook SQLite demo, read-only. */
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";

export function initDemo() {
  var container = document.getElementById("demo-terminal");
  if (!container) {
    return function () {};
  }

  var disposed = false;

  // DejaVuSansM Nerd Font Mono at 14px: advance 8.43px, line 16.8px.
  var cell = { width: 8.43, height: 16.8 };
  var terminalThemes = {
    dark: {
      background: "#0b0e14",
      foreground: "#d7dee8",
      cursor: "#d7dee8",
      selectionBackground: "#334155",
    },
    light: {
      background: "#f6f8fa",
      foreground: "#24292f",
      cursor: "#24292f",
      selectionBackground: "#c8d1dc",
    },
  };

  function activeTheme() {
    return document.documentElement.dataset.theme === "light" ? "light" : "dark";
  }

  var term = new Terminal({
    cols: 80,
    rows: 24,
    cursorBlink: true,
    fontFamily: '"DejaVuSansM Nerd Font Mono", monospace',
    fontSize: 14,
    lineHeight: 1.2,
    theme: terminalThemes[activeTheme()],
  });
  term.open(container);
  window.demoTerm = term;
  window.demoTermFocused = false;

  function fit() {
    if (disposed) return;
    var cols = Math.max(40, Math.min(200, Math.floor((container.clientWidth - 4) / cell.width)));
    var rows = Math.max(12, Math.min(60, Math.floor((container.clientHeight - 4) / cell.height)));
    if (cols !== term.cols || rows !== term.rows) {
      term.resize(cols, rows);
    }
  }
  fit();

  // Fullscreen toggle: a utility-class overlay (position: fixed) instead of
  // the Fullscreen API, so ESC can't exit — the button is the only way out.
  // fit() reflows the TUI to the new size (term.resize re-sends resize to the
  // server via onResize).
  var fullscreenEl = document.getElementById("demo-fullscreen");
  var fullscreenBtn = document.getElementById("demo-fullscreen-btn");
  var fullscreenClasses = [
    "fixed",
    "inset-0",
    "z-50",
    "flex",
    "flex-col",
    "gap-3",
    "mt-0",
    "p-3",
    "overflow-hidden",
    "bg-canvas",
  ];
  var terminalFullscreenClasses = ["flex-1", "h-auto", "border-0", "rounded-none"];
  var fullscreenClickHandler = null;
  var fullscreenResizeHandler = null;
  var initialBodyOverflow = document.body.style.overflow;
  if (fullscreenEl && fullscreenBtn) {
    function setFullscreen(active) {
      if (disposed) return;
      fullscreenEl.classList.toggle("mt-12", !active);
      fullscreenClasses.forEach(function (className) {
        fullscreenEl.classList.toggle(className, active);
      });
      terminalFullscreenClasses.forEach(function (className) {
        container.classList.toggle(className, active);
      });
      fullscreenEl.dataset.fullscreen = String(active);
      fullscreenBtn.setAttribute("aria-pressed", String(active));
      document.body.style.overflow = active ? "hidden" : "";
      fullscreenBtn.textContent = active ? "Exit fullscreen" : "Fullscreen";
      requestAnimationFrame(function () {
        if (disposed) return;
        fit();
        term.focus();
      });
    }
    fullscreenClickHandler = function () {
      setFullscreen(fullscreenEl.dataset.fullscreen !== "true");
    };
    fullscreenBtn.addEventListener("click", fullscreenClickHandler);
    fullscreenResizeHandler = function () {
      if (fullscreenEl.dataset.fullscreen === "true") {
        fit();
      }
    };
    window.addEventListener("resize", fullscreenResizeHandler);
  }

  var protocol = location.protocol === "https:" ? "wss:" : "ws:";
  var ws = null;
  var connected = false; // socket currently usable
  var everConnected = false; // was ever usable (for the disconnect message)
  var reconnectOnClose = false;
  var readyTimer = null;
  var terminalTheme = activeTheme();
  var statusNodes = [];

  function showStatus(message) {
    if (disposed) return;
    var status = document.createElement("p");
    status.className = "mt-4 text-sm text-muted";
    status.dataset.demoTerminalStatus = "";
    status.textContent = message;
    container.after(status);
    statusNodes.push(status);
  }

  function connect() {
    if (disposed) return;
    if (ws && ws.readyState !== WebSocket.CLOSED && ws.readyState !== WebSocket.CLOSING) {
      return;
    }
    var socket = new WebSocket(protocol + "//" + location.host + "/ws/tui?theme=" + terminalTheme);
    ws = socket;
    startReadyPolling();
    socket.onopen = function () {
      if (disposed || socket !== ws) return;
      connected = true;
      everConnected = true;
      socket.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
      term.focus();
    };
    socket.onmessage = function (event) {
      if (!disposed && socket === ws) {
        term.write(event.data);
      }
    };
    socket.onclose = function () {
      if (disposed || socket !== ws) return;
      ws = null;
      connected = false;
      var shouldReconnect = reconnectOnClose;
      reconnectOnClose = false;
      clearInterval(readyTimer);
      readyTimer = null;
      if (shouldReconnect) {
        connect();
        return;
      }
      if (everConnected) {
        showStatus("Live demo disconnected. Refresh to reconnect.");
      }
    };
    socket.onerror = function () {
      if (!disposed && socket === ws && !everConnected && !reconnectOnClose) {
        showStatus("Live demo unavailable right now.");
      }
    };
  }

  var dataSubscription = term.onData(function (data) {
    if (!disposed && connected && ws) {
      ws.send(JSON.stringify({ type: "input", data: data }));
    }
  });
  var resizeSubscription = term.onResize(function (size) {
    if (!disposed && connected && ws) {
      ws.send(JSON.stringify({ type: "resize", cols: size.cols, rows: size.rows }));
    }
  });

  function startReadyPolling() {
    clearInterval(readyTimer);
    readyTimer = setInterval(function () {
      if (disposed || !connected || window.demoTermFocused) {
        return;
      }
      var found = false;
      for (var i = 0; i < term.buffer.active.length; i++) {
        if (term.buffer.active.getLine(i).translateToString(true).indexOf("READONLY") >= 0) {
          found = true;
          break;
        }
      }
      if (found) {
        window.demoTermFocused = true;
        clearInterval(readyTimer);
        readyTimer = null;
        if (ws) {
          ws.send(JSON.stringify({ type: "input", data: "\ti" }));
        }
      }
    }, 400);
  }

  function handleThemeChange(event) {
    if (disposed) return;
    var nextTheme =
      event.detail === "light" || event.detail === "dark" ? event.detail : activeTheme();
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
  }
  window.addEventListener("themechange", handleThemeChange);

  // The TUI starts focused on the schema sidebar in vim normal mode. Once the
  // ready screen renders, focus the query editor and enter insert mode so
  // typing a query works immediately. READONLY appears in the footer in both
  // the full and compact (narrow) layouts; pane titles do not.
  connect();

  return function cleanup() {
    if (disposed) return;
    disposed = true;
    reconnectOnClose = false;
    connected = false;
    clearInterval(readyTimer);
    readyTimer = null;

    var socket = ws;
    ws = null;
    if (
      socket &&
      socket.readyState !== WebSocket.CLOSED &&
      socket.readyState !== WebSocket.CLOSING
    ) {
      socket.close();
    }

    window.removeEventListener("themechange", handleThemeChange);
    if (fullscreenBtn && fullscreenClickHandler) {
      fullscreenBtn.removeEventListener("click", fullscreenClickHandler);
    }
    if (fullscreenResizeHandler) {
      window.removeEventListener("resize", fullscreenResizeHandler);
    }
    if (fullscreenEl && fullscreenBtn) {
      fullscreenEl.classList.remove("mt-12");
      fullscreenClasses.forEach(function (className) {
        fullscreenEl.classList.remove(className);
      });
      terminalFullscreenClasses.forEach(function (className) {
        container.classList.remove(className);
      });
      fullscreenEl.dataset.fullscreen = "false";
      fullscreenBtn.setAttribute("aria-pressed", "false");
      document.body.style.overflow = initialBodyOverflow;
      fullscreenBtn.textContent = "Fullscreen";
    }

    if (dataSubscription) dataSubscription.dispose();
    if (resizeSubscription) resizeSubscription.dispose();
    term.dispose();
    statusNodes.forEach(function (status) {
      status.remove();
    });
    statusNodes = [];

    if (window.demoTerm === term) {
      window.demoTerm = undefined;
      window.demoTermFocused = undefined;
    }
  };
}
