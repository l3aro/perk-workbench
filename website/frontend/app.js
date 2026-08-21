/* Site-wide behaviours: HTMX powers the live search-as-you-type results on
 * /search; the theme toggle flips the `data-theme` attribute the head
 * script also honours; copy buttons copy their data-copy payload. The
 * terminal-native extras (typewriter loop, cursor spotlight, status-bar
 * keyboard shortcuts) degrade to static content without JS.
 * Bundled here so every script the site owns ships through the versioned
 * frontend build. */
import 'htmx.org';

function setTheme(next) {
  document.documentElement.dataset.theme = next;
  localStorage.setItem('theme', next);
}

const themeToggle = document.getElementById('theme-toggle');
if (themeToggle) {
  themeToggle.addEventListener('click', () => {
    setTheme(document.documentElement.dataset.theme === 'light' ? 'dark' : 'light');
  });
}

const statusTheme = document.querySelector('[data-status-theme]');
if (statusTheme) {
  statusTheme.addEventListener('click', () => {
    setTheme(document.documentElement.dataset.theme === 'light' ? 'dark' : 'light');
  });
}
/* Command blocks: every <pre><code> outside the home hero chrome gets a
 * copy button on a row below the snippet, so docs and demo snippets are
 * one click away without repeating markup in every template. */
const COPY_ICON = '<svg class="size-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';
const CHECK_ICON = '<svg class="size-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m5 13 4 4L19 7"/></svg>';

document.querySelectorAll('main pre').forEach((pre) => {
  if (pre.closest('.install-cmd, .boot-term, .term-window')) return;
  if (!pre.querySelector('code') || pre.querySelector('.copy-btn')) return;
  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'copy-btn copy-btn-inline';
  const code = pre.querySelector('code');
  button.dataset.copy = code.textContent.trim();
  if (code.textContent.includes('\n')) pre.classList.add('is-multiline');
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
      button.classList.add('is-copied');
      setTimeout(() => {
        button.innerHTML = original;
        button.classList.remove('is-copied');
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

/* Status-bar style keyboard shortcuts: ? docs, / search, t theme.
 * Ignored while typing in form fields or with modifier keys held. */
document.addEventListener('keydown', (event) => {
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
    window.location.href = '/search';
  } else if (event.key === 't') {
    setTheme(document.documentElement.dataset.theme === 'light' ? 'dark' : 'light');
  }
});
