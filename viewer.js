document.querySelectorAll('[data-set-mode]').forEach(button => {
  button.addEventListener('click', () => {
    const mode = button.dataset.setMode;
    document.documentElement.dataset.mode = mode;
    document.querySelectorAll('[data-set-mode]').forEach(item => {
      const active = item.dataset.setMode === mode;
      item.classList.toggle('active', active);
      item.setAttribute('aria-pressed', String(active));
    });
  });
});

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

document.querySelectorAll('#file-nav a').forEach(link => {
  link.addEventListener('click', () => {
    const target = document.getElementById(link.hash.slice(1));
    if (target) target.open = true;
  });
});
