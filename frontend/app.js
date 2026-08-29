/* Site-wide behaviours: the global search spotlight (a <dialog> modal that
 * queries the server through htmx as you type); theme controls cycle the
 * saved light/dark/system preference; copy buttons copy their data-copy
 * payload. The terminal-native extras (typewriter loop, cursor spotlight,
 * status-bar keyboard shortcuts) degrade to static content without JS. */
import htmx from 'htmx.org';

const THEME_PREFERENCES = ['dark', 'light', 'system'];

function isThemePreference(value) {
  return THEME_PREFERENCES.includes(value);
}

function currentThemePreference() {
  const preference = window.getThemePreference?.() ||
    document.documentElement.dataset.themePreference;
  return isThemePreference(preference) ? preference : 'system';
}

function effectiveThemeFor(preference) {
  if (preference !== 'system') return preference;
  return matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
}

function nextThemePreference(preference) {
  const index = THEME_PREFERENCES.indexOf(preference);
  return THEME_PREFERENCES[(index + 1) % THEME_PREFERENCES.length];
}

function updateThemeControls(preference, effective) {
  const next = nextThemePreference(preference);
  document.documentElement.dataset.themePreference = preference;
  document.querySelectorAll('#theme-toggle, [data-status-theme]').forEach((control) => {
    control.dataset.themePreference = preference;
    control.dataset.themeEffective = effective;
    const label = `Color theme: ${preference} (activate to use ${next})`;
    control.setAttribute('aria-label', label);
    control.setAttribute('title', label);
  });
}

function setTheme(next) {
  if (!isThemePreference(next)) return;
  if (typeof window.setThemePreference === 'function') {
    window.setThemePreference(next);
    return;
  }
  const effective = effectiveThemeFor(next);
  document.documentElement.dataset.theme = effective;
  document.documentElement.dataset.themePreference = next;
  localStorage.setItem('theme', next);
  window.dispatchEvent(new CustomEvent('themechange', { detail: effective }));
  window.dispatchEvent(new CustomEvent('themepreferencechange', { detail: next }));
}

function cycleTheme() {
  setTheme(nextThemePreference(currentThemePreference()));
}

const themeToggle = document.getElementById('theme-toggle');
const statusTheme = document.querySelector('[data-status-theme]');
updateThemeControls(currentThemePreference(), document.documentElement.dataset.theme);
if (themeToggle) themeToggle.addEventListener('click', cycleTheme);
if (statusTheme) statusTheme.addEventListener('click', cycleTheme);
window.addEventListener('themechange', (event) => {
  const effective = event.detail === 'light' || event.detail === 'dark'
    ? event.detail
    : document.documentElement.dataset.theme;
  updateThemeControls(currentThemePreference(), effective);
});
/* Command blocks: every <pre><code> outside the home hero chrome gets a
 * copy button on a row below the snippet, so docs and demo snippets are
 * one click away without repeating markup in every template. */
const COPY_ICON = '<svg class="size-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';
const CHECK_ICON = '<svg class="size-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m5 13 4 4L19 7"/></svg>';

document.querySelectorAll('main pre').forEach((pre) => {
  if (pre.closest('.install-cmd, .boot-term, .term-window')) return;
  if (!pre.querySelector('code') || pre.querySelector('[data-copy]')) return;
  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'inline-grid size-8 shrink-0 cursor-pointer place-items-center rounded-md border border-line bg-transparent text-muted transition-colors duration-150 hover:border-[var(--color-line-strong)] hover:text-ink data-[copied=true]:border-good data-[copied=true]:text-good';
  button.dataset.copyButton = '';
  const code = pre.querySelector('code');
  button.dataset.copy = code.textContent.trim();
  code.classList.add('min-w-0', 'flex-1');
  pre.classList.add('flex', 'items-center', 'gap-4');
  if (code.textContent.includes('\n')) {
    pre.classList.remove('items-center');
    pre.classList.add('items-start');
  }
  button.setAttribute('aria-label', 'Copy to clipboard');
  button.innerHTML = COPY_ICON;
  pre.appendChild(button);
});

document.addEventListener('click', (event) => {
  const button = event.target.closest('[data-copy]');
  if (button) {
    const done = () => {
      const original = button.innerHTML;
      button.innerHTML = CHECK_ICON;
      button.dataset.copied = 'true';
      setTimeout(() => {
        button.innerHTML = original;
        delete button.dataset.copied;
      }, 1500);
    };
    const copyText = button.dataset.copy;
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(copyText).then(done);
    } else {
      const helper = document.createElement('textarea');
      helper.value = copyText;
      helper.setAttribute('readonly', '');
      helper.style.position = 'fixed';
      helper.style.opacity = '0';
      document.body.appendChild(helper);
      helper.select();
      document.execCommand('copy');
      helper.remove();
      done();
    }
    return;
  }

  const goto = event.target.closest('[data-goto]');
  if (goto) {
    window.location.href = goto.dataset.goto;
  }
});

/* Cursor spotlight: the ambient backdrop carries a soft glow that follows
 * the pointer via --mx/--my custom properties (rAF-throttled). */
const reduceMotion = matchMedia('(prefers-reduced-motion: reduce)').matches;
if (!reduceMotion) {
  let frame = 0;
  document.addEventListener('pointermove', (event) => {
    if (frame) return;
    frame = requestAnimationFrame(() => {
      frame = 0;
      document.body.style.setProperty('--mx', `${event.clientX}px`);
      document.body.style.setProperty('--my', `${event.clientY}px`);
    });
  });
}

/* Typewriter loop for the hero boot terminal. Phrases come from
 * data-phrases (JSON array); without JS or with reduced motion the first
 * phrase is rendered statically. */
const typeTarget = document.getElementById('type-loop');
if (typeTarget) {
  let phrases = [];
  try {
    phrases = JSON.parse(typeTarget.dataset.phrases || '[]');
  } catch {
    phrases = [];
  }
  if (!phrases.length) {
    phrases = [
      'perk-workbench chinook-sqlite.db --read-only',
      'SELECT name FROM albums LIMIT 5;',
      '\\dt -- list tables',
      '.schema artists',
    ];
  }

  if (reduceMotion) {
    typeTarget.textContent = phrases[0];
  } else {
    let phraseIndex = 0;
    let charIndex = 0;
    let deleting = false;

    const tick = () => {
      const phrase = phrases[phraseIndex];
      charIndex += deleting ? -1 : 1;
      typeTarget.textContent = phrase.slice(0, charIndex);

      let delay = deleting ? 26 : 58;
      if (!deleting && charIndex === phrase.length) {
        delay = 2200;
        deleting = true;
      } else if (deleting && charIndex === 0) {
        deleting = false;
        phraseIndex = (phraseIndex + 1) % phrases.length;
        delay = 500;
      }
      setTimeout(tick, delay);
    };
    tick();
  }
}

/* Global search spotlight: a <dialog> in base.html, openable from any page
 * via `/`, Cmd/Ctrl+K, or any [data-search-open] trigger. Queries are
 * rendered server-side through htmx; results are keyboard-navigable
 * (↑/↓/Enter). */
const spotlight = document.getElementById('search-spotlight');
if (spotlight) {
  const input = spotlight.querySelector('#spotlight-input');
  let activeIndex = -1;
  const topbarInput = document.getElementById('docs-nav-search');

  const options = () => Array.from(
    spotlight.querySelectorAll('#spotlight-output [role="option"]'),
  );

  function setActive(next) {
    const items = options();
    if (!items.length) return;
    activeIndex = (next + items.length) % items.length;
    items.forEach((item, i) => {
      item.dataset.active = i === activeIndex ? 'true' : 'false';
      item.setAttribute('aria-selected', i === activeIndex ? 'true' : 'false');
    });
    items[activeIndex].scrollIntoView({ block: 'nearest' });
  }

  function openSpotlight(query = '', focusInput = true, modal = true) {
    if (spotlight.open) {
      if (focusInput) input.focus();
      return;
    }
    if (modal) {
      spotlight.showModal();
    } else {
      spotlight.show();
    }
    input.value = query;
    if (query.trim()) htmx.trigger(input, 'input');
    if (focusInput) input.focus();
  }

  document.querySelectorAll('[data-search-open]').forEach((trigger) => {
    trigger.addEventListener('click', () => openSpotlight());
  });

  if (topbarInput) {
    topbarInput.addEventListener('keydown', (event) => {
      if (event.key === 'Enter' || event.key === ' ') {
        event.preventDefault();
        openSpotlight();
      }
    });
  }

  input.addEventListener('keydown', (event) => {
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      setActive(activeIndex + 1);
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      setActive(activeIndex - 1);
    } else if (event.key === 'Enter') {
      // Never let the form's implicit submission reload the page and discard
      // the query; open the highlighted item, or the top hit when nothing is
      // highlighted yet.
      event.preventDefault();
      const items = options();
      if (activeIndex >= 0 && items[activeIndex]) {
        items[activeIndex].click();
      } else if (items.length > 0) {
        items[0].click();
      }
    }
  });

  document.body.addEventListener('htmx:after:swap', (event) => {
    if (event.detail?.target?.id === 'spotlight-output') {
      activeIndex = -1;
    }
  });

  // Clicking the backdrop (the dialog padding itself) closes the spotlight.
  spotlight.addEventListener('click', (event) => {
    if (event.target === spotlight) spotlight.close();
  });

  spotlight.addEventListener('close', () => {
    activeIndex = -1;
    input.value = '';
    htmx.trigger(input, 'input');
  });

  // Belt and braces: the dialog form must never submit and navigate away.
  spotlight.querySelector('[data-spotlight-form]')?.addEventListener('submit', (event) => {
    event.preventDefault();
  });

  window.openSpotlight = openSpotlight;
}

/* Status-bar style keyboard shortcuts: ? docs, / search, t theme.
 * Ignored while typing in form fields; Cmd/Ctrl+K works everywhere. */
document.addEventListener('keydown', (event) => {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
    event.preventDefault();
    window.openSpotlight?.();
    return;
  }
  if (event.metaKey || event.ctrlKey || event.altKey) return;
  const target = event.target;
  if (target instanceof HTMLElement &&
      (target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName))) {
    return;
  }

  if (event.key === '?') {
    window.location.href = '/docs';
  } else if (event.key === '/') {
    event.preventDefault();
    window.openSpotlight?.();
  } else if (event.key === 't') {
    cycleTheme();
  }
});
