const root = document.documentElement;
const stateKey = `act-as-pr:${location.pathname}`;
let selectedTab = 'files';
let selectedCommit = '';

function selectTab(name) {
  if (!document.querySelector(`[data-view="${name}"]`)) name = 'files';
  selectedTab = name;
  document.querySelectorAll('[data-tab]').forEach(button => {
    const active = button.dataset.tab === name;
    button.classList.toggle('active', active);
    button.setAttribute('aria-selected', String(active));
  });
  document.querySelectorAll('[data-view]').forEach(pane => {
    pane.hidden = pane.dataset.view !== name;
  });
}

function showCommit(id) {
  const target = [...document.querySelectorAll('[data-commit-detail]')].find(item => item.dataset.commitDetail === id);
  selectedCommit = target ? id : '';
  document.querySelector('[data-commits-list]').hidden = Boolean(target);
  document.querySelectorAll('[data-commit-detail]').forEach(detail => {
    detail.hidden = detail !== target;
  });
}

function setMode(mode) {
  if (mode !== 'unified' && mode !== 'split') mode = 'unified';
  root.dataset.mode = mode;
  document.querySelectorAll('[data-set-mode]').forEach(item => {
    const active = item.dataset.setMode === mode;
    item.classList.toggle('active', active);
    item.setAttribute('aria-pressed', String(active));
  });
}

function historyView() {
  return { actAsPR: true, tab: selectedTab, commit: selectedCommit, y: scrollY };
}

function navigateTo(tab, commit = '') {
  if (tab !== 'commits') commit = '';
  if (tab === selectedTab && commit === selectedCommit) return;
  history.replaceState(historyView(), '', location.href);
  selectTab(tab);
  showCommit(commit);
  history.pushState({ ...historyView(), y: 0 }, '', location.href.split('#')[0]);
  scrollTo({ top: 0, behavior: 'instant' });
}

document.querySelectorAll('[data-tab]').forEach(button => {
  button.addEventListener('click', () => {
    navigateTo(button.dataset.tab);
  });
});
document.querySelectorAll('[data-commit-target]').forEach(button => {
  button.addEventListener('click', () => navigateTo('commits', button.dataset.commitTarget));
});
document.querySelectorAll('[data-back-commits]').forEach(button => {
  button.addEventListener('click', () => navigateTo('commits'));
});
document.querySelectorAll('[data-set-mode]').forEach(button => {
  button.addEventListener('click', () => setMode(button.dataset.setMode));
});
document.querySelectorAll('.copy-path').forEach(button => {
  let resetTimer;
  button.addEventListener('click', async event => {
    event.preventDefault();
    event.stopPropagation();
    const path = button.dataset.copyPath;
    let copied = false;
    if (navigator.clipboard?.writeText) {
      try {
        await navigator.clipboard.writeText(path);
        copied = true;
      } catch (_) {
        // Local file previews may not have Clipboard API permission.
      }
    }
    if (!copied) {
      const field = document.createElement('textarea');
      field.value = path;
      field.setAttribute('readonly', '');
      field.style.position = 'fixed';
      field.style.opacity = '0';
      document.body.append(field);
      field.select();
      try { copied = document.execCommand('copy'); } catch (_) { /* Clipboard access was denied. */ }
      field.remove();
      button.focus();
    }
    const message = copied ? 'Copied' : 'Copy failed';
    button.querySelector('.copy-feedback').textContent = message;
    button.setAttribute('aria-label', `${message}: ${path}`);
    button.title = message;
    clearTimeout(resetTimer);
    resetTimer = setTimeout(() => {
      button.querySelector('.copy-feedback').textContent = '';
      button.setAttribute('aria-label', `Copy path for ${path}`);
      button.title = 'Copy file path';
    }, 2000);
  });
});

function setViewed(card, value) {
  const button = card.querySelector('.viewed-toggle');
  const path = card.querySelector('.file-path').textContent;
  card.dataset.viewed = String(value);
  button.setAttribute('aria-pressed', String(value));
  button.setAttribute('aria-label', `${value ? 'Unmark' : 'Mark'} ${path} as viewed`);
  const link = document.querySelector(`.nav-file[href="#${card.id}"]`);
  if (link) link.classList.toggle('viewed', value);
}

function updateViewedProgress(workspace) {
  const viewed = workspace.querySelectorAll('.file-card[data-viewed="true"]').length;
  const total = workspace.querySelectorAll('.file-card').length;
  workspace.previousElementSibling.querySelector('.viewed-progress').textContent = `${viewed} of ${total} viewed`;
}

document.querySelectorAll('.file-card').forEach(card => {
  card.querySelector('.viewed-toggle').addEventListener('click', event => {
    event.preventDefault();
    event.stopPropagation();
    const value = card.dataset.viewed !== 'true';
    setViewed(card, value);
    card.open = !value;
    updateViewedProgress(card.closest('.workspace'));
    saveUIState();
  });
});
window.addEventListener('popstate', event => {
  if (!event.state?.actAsPR) return;
  selectTab(event.state.tab);
  showCommit(event.state.tab === 'commits' ? event.state.commit : '');
  if (!location.hash) requestAnimationFrame(() => scrollTo({ top: event.state.y || 0, behavior: 'instant' }));
});

const backToTop = document.getElementById('back-to-top');
function updateBackToTop() {
  backToTop.hidden = scrollY < 400;
}
window.addEventListener('scroll', updateBackToTop, { passive: true });
backToTop.addEventListener('click', () => {
  scrollTo({ top: 0, behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth' });
});
updateBackToTop();

// Highlight a small set of common source tokens without interpreting diff text as HTML.
const sourceFiles = /\.(go|js|ts|jsx|tsx|rb|py|rs|java|c|h|cpp|css|html|json|yml|yaml)$/i;
const tokenPattern = /(\/\/[^\n]*|#[^\n]*|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|\b(?:func|package|import|return|if|else|for|range|var|const|let|function|class|def|end|module|type|struct|interface|public|private|async|await|true|false|null|nil)\b)/g;
document.querySelectorAll('.file-card').forEach(card => {
  const path = card.querySelector('.file-path').textContent;
  if (!sourceFiles.test(path)) return;
  card.querySelectorAll('.code-text').forEach(node => {
    const text = node.textContent;
    if (text.length > 4000) return;
    tokenPattern.lastIndex = 0;
    let match, from = 0;
    const fragment = document.createDocumentFragment();
    while ((match = tokenPattern.exec(text))) {
      fragment.append(document.createTextNode(text.slice(from, match.index)));
      const span = document.createElement('span');
      span.className = match[0].startsWith('//') || match[0].startsWith('#') ? 'token-comment' : match[0].startsWith('"') || match[0].startsWith("'") ? 'token-string' : 'token-keyword';
      span.textContent = match[0];
      fragment.append(span);
      from = tokenPattern.lastIndex;
    }
    if (from) {
      fragment.append(document.createTextNode(text.slice(from)));
      node.replaceChildren(fragment);
    }
  });
});

document.querySelectorAll('nav[id$="-nav"] a').forEach(link => {
  link.addEventListener('click', () => {
    history.replaceState(historyView(), '', location.href);
    const target = document.getElementById(link.hash.slice(1));
    if (target) target.open = true;
  });
});

function saveUIState() {
  try {
    const collapsed = [...document.querySelectorAll('.file-card:not([open])')].map(card => card.dataset.fileKey);
    const viewed = [...document.querySelectorAll('.file-card[data-viewed="true"]')].map(card => [card.dataset.fileKey, card.dataset.reviewFingerprint]);
    const navScroll = [...document.querySelectorAll('nav[id$="-nav"]')].map(nav => [nav.id, nav.scrollTop]);
    sessionStorage.setItem(stateKey, JSON.stringify({ tab: selectedTab, commit: selectedCommit, mode: root.dataset.mode, collapsed, viewed, navScroll, x: scrollX, y: scrollY }));
  } catch (_) {
    // Browser privacy settings may disable session storage.
  }
}

function restoreUIState() {
  try {
    const saved = JSON.parse(sessionStorage.getItem(stateKey) || 'null');
    if (!saved) return;
    selectTab(saved.tab);
    showCommit(saved.commit);
    setMode(saved.mode);
    const collapsed = new Set(saved.collapsed || []);
    const viewed = new Map(saved.viewed || []);
    document.querySelectorAll('.file-card').forEach(card => {
      const fingerprint = viewed.get(card.dataset.fileKey);
      const unchanged = fingerprint === card.dataset.reviewFingerprint;
      setViewed(card, Boolean(fingerprint && unchanged));
      card.open = !collapsed.has(card.dataset.fileKey) || Boolean(fingerprint && !unchanged);
    });
    document.querySelectorAll('.workspace').forEach(updateViewedProgress);
    for (const [id, top] of saved.navScroll || []) {
      const nav = document.getElementById(id);
      if (nav) nav.scrollTop = top;
    }
    requestAnimationFrame(() => requestAnimationFrame(() => scrollTo(saved.x || 0, saved.y || 0)));
  } catch (_) {
    // The viewer still works when storage is unavailable or stale.
  }
}

restoreUIState();
window.addEventListener('beforeunload', saveUIState);
if (root.dataset.watchEvents) {
  history.scrollRestoration = 'manual';
  const events = new EventSource(root.dataset.watchEvents);
  events.addEventListener('version', event => {
    if (event.data !== root.dataset.version) {
      saveUIState();
      events.close();
      location.reload();
    }
  });
}
history.replaceState(historyView(), '', location.href);
