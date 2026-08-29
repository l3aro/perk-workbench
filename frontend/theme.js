/* Theme bootstrap: applies the saved preference before first paint. Loaded
 * synchronously from <head> so the effective `data-theme` is set before the
 * stylesheet renders. The preference remains available to app.js and reacts
 * to operating-system changes while `system` is selected. */
(function () {
  var media = window.matchMedia("(prefers-color-scheme: light)");
  var saved = localStorage.getItem("theme");
  var preference = saved === "light" || saved === "dark" || saved === "system" ? saved : "system";
  var systemMatches = media.matches;
  var previousEffective = null;

  function effectiveTheme() {
    return preference === "system" ? (systemMatches ? "light" : "dark") : preference;
  }

  function dispatch(name, detail) {
    window.dispatchEvent(new CustomEvent(name, { detail: detail }));
  }

  function applyPreference(next, persist, notify) {
    if (next !== "light" && next !== "dark" && next !== "system") return;
    var changedPreference = preference !== next;
    preference = next;
    var nextEffective = effectiveTheme();
    document.documentElement.dataset.theme = nextEffective;
    document.documentElement.dataset.themePreference = preference;
    if (persist) localStorage.setItem("theme", preference);
    if (notify && (changedPreference || previousEffective !== nextEffective)) {
      dispatch("themechange", nextEffective);
    }
    if (notify && changedPreference) {
      dispatch("themepreferencechange", preference);
    }
    previousEffective = nextEffective;
  }

  function onMediaChange(event) {
    systemMatches = event && typeof event.matches === "boolean" ? event.matches : media.matches;
    if (preference !== "system") return;
    applyPreference("system", false, true);
  }

  if (media.addEventListener) {
    media.addEventListener("change", onMediaChange);
  } else if (media.addListener) {
    media.addListener(onMediaChange);
  }

  applyPreference(preference, false, false);
  window.getThemePreference = function () {
    return preference;
  };
  window.setThemePreference = function (next) {
    applyPreference(next, true, true);
  };
})();
