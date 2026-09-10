// Vanilla JS review queue. All user text goes through textContent, never innerHTML.
const token = document.querySelector('meta[name="blinken-token"]').content;

async function api(path, opts = {}) {
  const res = await fetch(path, {
    ...opts,
    headers: { 'Content-Type': 'application/json', 'X-Blinken-Token': token, ...(opts.headers || {}) },
  });
  if (!res.ok) throw new Error(`${res.status} ${await res.text()}`);
  return res.json();
}

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

function row(dl, label, value) {
  if (!value) return;
  dl.append(el('dt', null, label), el('dd', null, value));
}

function render(data) {
  document.getElementById('project').textContent = data.project.name + ' — ' + data.project.root;
  const c = data.counts;
  document.getElementById('counts').textContent =
    `${c.unreviewed} unreviewed | ${c.followup} followup | ${c.high_impact} high-impact | ${c.low_confidence} low-confidence`;
  const queue = document.getElementById('queue');
  queue.replaceChildren();
  document.getElementById('empty').hidden = data.guesses.length > 0;
  for (const g of data.guesses) {
    const li = el('li');
    li.dataset.id = g.id;
    const top = el('div', 'top');
    top.append(el('span', 'pill ' + g.status, g.status), el('span', 'summary', g.summary),
      el('span', 'score', `${g.priority.reason} · score ${g.priority.score}`));
    li.append(top);
    const dl = el('dl');
    row(dl, 'kind', g.kind);
    row(dl, 'ambiguity', g.ambiguity);
    row(dl, 'reason', g.reason);
    row(dl, 'alternative', g.alternative);
    row(dl, 'would ask', g.would_ask);
    row(dl, 'cwd', g.cwd_rel || g.cwd);
    row(dl, 'files', (g.files || []).map(f => f.line ? `${f.path}:${f.line}` : f.path).join(', '));
    row(dl, 'note', g.review_note);
    li.append(dl);
    const actions = el('div', 'actions');
    const note = el('input', 'note');
    note.placeholder = 'optional note';
    for (const s of ['accepted', 'rejected', 'followup']) {
      const b = el('button', null, s === 'followup' ? 'follow up' : s.replace(/ed$/, ''));
      b.onclick = async () => {
        await api(`/api/guesses/${g.id}/status`, { method: 'POST', body: JSON.stringify({ status: s, note: note.value }) });
        load();
      };
      actions.append(b);
    }
    actions.append(note);
    li.append(actions);
    queue.append(li);
  }
}

async function load() { render(await api('/api/review')); }
load();
