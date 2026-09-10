// Vanilla JS review queue. All user text goes through textContent, never innerHTML.
(() => {
  const token = document.querySelector('meta[name="blinken-token"]').content;
  const $ = (id) => document.getElementById(id);
  const state = { project: '', guesses: [], focus: 0, selected: new Set(), open: new Set() };

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
  const row = (dl, label, value) => { if (value) dl.append(el('dt', null, label), el('dd', null, value)); };
  const label = (s) => s === 'followup' ? 'follow up' : s.replace(/ed$/, '');

  function filters() {
    const status = [...document.querySelectorAll('#status-filter input:checked')].map(i => i.value);
    const q = new URLSearchParams();
    if (state.project) q.set('project', state.project);
    if ($('session').value) q.set('session', $('session').value);
    q.set('status', status.length ? status.join(',') : 'none');
    for (const k of ['kind', 'confidence', 'impact']) if ($(k).value) q.set(k, $(k).value);
    return q;
  }

  async function loadProjects() {
    const data = await api('/api/projects');
    state.project = data.current;
    const sel = $('project');
    sel.replaceChildren();
    for (const p of data.projects) {
      const o = el('option', null, `${p.name} (${p.unresolved})`);
      o.value = p.id;
      if (p.id === data.current) o.selected = true;
      sel.append(o);
    }
  }

  async function loadSessions() {
    const ss = await api('/api/sessions?project=' + encodeURIComponent(state.project));
    const sel = $('session');
    sel.replaceChildren(el('option', null, 'all'));
    sel.firstChild.value = '';
    for (const s of ss) {
      const when = new Date(s.started_at).toLocaleString();
      const o = el('option', null, `${when} · ${s.agent || 'agent'} · ${s.guesses}`);
      o.value = s.id;
      sel.append(o);
    }
  }

  async function load(keepFocus = true) {
    const q = filters();
    if (q.get('status') === 'none') { state.guesses = []; render(); return; }
    const data = await api('/api/review?' + q.toString());
    state.guesses = data.guesses;
    $('root').textContent = data.project.root || '';
    const c = data.counts;
    $('counts').textContent = `${c.unreviewed} unreviewed | ${c.followup} followup | ${c.high_impact} high-impact | ${c.low_confidence} low-confidence`;
    const ids = new Set(state.guesses.map(g => g.id));
    for (const id of state.selected) if (!ids.has(id)) state.selected.delete(id);
    if (!keepFocus) state.focus = 0;
    state.focus = Math.min(state.focus, Math.max(0, state.guesses.length - 1));
    render();
  }

  function render() {
    const queue = $('queue');
    queue.replaceChildren();
    $('empty').hidden = state.guesses.length > 0;
    state.guesses.forEach((g, i) => {
      const li = el('li');
      li.dataset.id = g.id;
      li.tabIndex = -1;
      if (i === state.focus) li.classList.add('focus');
      if (state.selected.has(g.id)) li.classList.add('selected');
      if (state.open.has(g.id)) li.classList.add('open');

      const cb = el('input');
      cb.type = 'checkbox';
      cb.checked = state.selected.has(g.id);
      cb.onchange = () => { toggleSelect(g.id); };
      li.append(cb);

      const body = el('div');
      const top = el('div', 'top');
      top.append(el('span', 'pill ' + g.status, g.status), el('span', 'summary', g.summary),
        el('span', 'score', `${g.priority.reason} · ${g.priority.score}`));
      top.onclick = () => { state.focus = i; toggleOpen(g.id); };
      body.append(top);

      const details = el('div', 'details');
      const dl = el('dl');
      row(dl, 'kind', g.kind);
      row(dl, 'ambiguity', g.ambiguity);
      row(dl, 'chosen', g.chosen_behavior);
      row(dl, 'reason', g.reason);
      row(dl, 'alternative', g.alternative);
      row(dl, 'would ask', g.would_ask);
      row(dl, 'cwd', g.cwd_rel ? './' + g.cwd_rel : g.cwd);
      row(dl, 'files', (g.files || []).map(f => f.line ? `${f.path}:${f.line}` : f.path).join(', '));
      row(dl, 'session', g.session_id);
      row(dl, 'created', new Date(g.created_at).toLocaleString());
      row(dl, 'note', g.review_note);
      details.append(dl);
      const actions = el('div', 'actions');
      const note = el('input', 'note');
      note.placeholder = 'optional note';
      note.dataset.note = g.id;
      note.value = g.review_note || '';
      for (const s of ['accepted', 'rejected', 'followup']) {
        const b = el('button', null, label(s));
        b.onclick = (e) => { e.stopPropagation(); state.focus = i; act(g.id, s, note.value); };
        actions.append(b);
      }
      actions.append(note);
      details.append(actions);
      body.append(details);
      li.append(body);
      queue.append(li);
    });
    const bulk = $('bulk');
    bulk.hidden = state.selected.size === 0;
    $('bulk-count').textContent = `${state.selected.size} selected`;
    const f = queue.children[state.focus];
    if (f) f.scrollIntoView({ block: 'nearest' });
  }

  function toggleSelect(id) { state.selected.has(id) ? state.selected.delete(id) : state.selected.add(id); render(); }
  function toggleOpen(id) { state.open.has(id) ? state.open.delete(id) : state.open.add(id); render(); }

  // After an action the row leaves the queue (unless its new status is still
  // shown), so keeping the same index lands on the next unresolved guess.
  async function act(id, status, note) {
    await api(`/api/guesses/${id}/status`, { method: 'POST', body: JSON.stringify({ status, note: note || '' }) });
    state.open.delete(id);
    await load(true);
  }

  async function bulk(status) {
    const ids = [...state.selected];
    if (!ids.length) return;
    if (!confirm(`${label(status)} ${ids.length} guess${ids.length === 1 ? '' : 'es'}?`)) return;
    await api('/api/guesses/bulk', { method: 'POST', body: JSON.stringify({ ids, status, note: $('bulk-note').value }) });
    state.selected.clear();
    $('bulk-note').value = '';
    await load(true);
  }

  function move(delta) {
    if (!state.guesses.length) return;
    state.focus = (state.focus + delta + state.guesses.length) % state.guesses.length;
    render();
  }

  document.addEventListener('keydown', (e) => {
    if (e.target instanceof Element && e.target.matches('input, select, textarea')) {
      if (e.key === 'Escape') e.target.blur();
      return;
    }
    const g = state.guesses[state.focus];
    switch (e.key) {
      case 'j': case 'ArrowDown': move(1); e.preventDefault(); break;
      case 'k': case 'ArrowUp': move(-1); e.preventDefault(); break;
      case 'Enter': case 'Return': if (g) toggleOpen(g.id); break;
      case 'x': if (g) toggleSelect(g.id); break;
      case 'a': if (g) act(g.id, 'accepted', currentNote(g.id)); break;
      case 'r': if (g) act(g.id, 'rejected', currentNote(g.id)); break;
      case 'f': if (g) act(g.id, 'followup', currentNote(g.id)); break;
      case 'n': if (g) { state.open.add(g.id); render(); document.querySelector(`input[data-note="${g.id}"]`)?.focus(); e.preventDefault(); } break;
    }
  });
  const currentNote = (id) => document.querySelector(`input[data-note="${id}"]`)?.value || '';

  $('project').onchange = async () => { state.project = $('project').value; state.selected.clear(); await loadSessions(); load(false); };
  $('session').onchange = () => load(false);
  for (const id of ['kind', 'confidence', 'impact']) $(id).onchange = () => load(false);
  document.querySelectorAll('#status-filter input').forEach(i => i.onchange = () => load(false));
  document.querySelectorAll('#bulk button[data-status]').forEach(b => b.onclick = () => bulk(b.dataset.status));
  $('bulk-clear').onclick = () => { state.selected.clear(); render(); };

  (async () => { await loadProjects(); await loadSessions(); await load(false); })();
})();
