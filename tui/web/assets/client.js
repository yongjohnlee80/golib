// WebTUI client (ADR-0009). It displays cells and reports input. Nothing else.
//
// The server decides what an input MEANS — §2.9's resolution order lives in Go so
// it is testable against the real event structs. This file reports faithfully and
// owns exactly two decisions it cannot delegate:
//
//   1. preventDefault, which must be synchronous and cannot wait for a round
//      trip. Its tables are INJECTED from the same Go values the decoder uses,
//      so the two cannot drift.
//   2. the capture-element text machine, which is DOM state and has no
//      server-side equivalent.
'use strict';
(function () {
  const CFG = window.__WEBTUI__;
  const NAMED = new Set(CFG.namedKeys);
  const grid = document.getElementById('g');
  const capture = document.getElementById('cap');
  const cursor = document.getElementById('cur');

  let ws = null;
  let cols = 0, rows = 0;
  let cellW = 0, cellH = 0;
  let cells = [];          // flat array of elements, row-major
  let composing = false;
  let sessionID = '';

  // --- the ticket, and why it is in the fragment -------------------------
  //
  // A URL query or path lands in browser history, in a Referer header, and in
  // every access log and proxy between here and the server. A FRAGMENT is never
  // transmitted. It is read once, scrubbed from the address bar with
  // replaceState before the socket opens, and sent in the first WebSocket
  // message instead.
  function takeTicket() {
    const frag = window.location.hash.replace(/^#/, '');
    if (frag) {
      history.replaceState(null, '', window.location.pathname + window.location.search);
    }
    const p = new URLSearchParams(frag);
    return { ticket: p.get('t') || '', session: p.get('s') || '' };
  }

  // --- measurement: the client measures, the server never guesses --------
  function measure() {
    const probe = document.createElement('span');
    probe.className = 'probe';
    // A run of the same character, then divide: measuring one glyph rounds to a
    // whole pixel and the error compounds across a wide row.
    probe.textContent = 'M'.repeat(100);
    document.body.appendChild(probe);
    const r = probe.getBoundingClientRect();
    cellW = r.width / 100;
    cellH = r.height;
    probe.remove();
    return cellW > 0 && cellH > 0;
  }

  // fontAgrees checks that a wide grapheme really is twice a narrow one.
  //
  // It informs the UnicodeCore capability and is never presented as proof: a
  // finite probe cannot establish that every Unicode grapheme agrees with the
  // server's width table. What actually keeps a mismatch safe is that each cell
  // box clips its own overflow (§2.6).
  function fontAgrees() {
    const probe = document.createElement('span');
    probe.className = 'probe';
    probe.textContent = '漢'.repeat(50);
    document.body.appendChild(probe);
    const w = probe.getBoundingClientRect().width / 50;
    probe.remove();
    return Math.abs(w - cellW * 2) < cellW * 0.15;
  }

  function gridSize() {
    const r = grid.getBoundingClientRect();
    return {
      cols: Math.max(1, Math.floor(r.width / cellW)),
      rows: Math.max(1, Math.floor(r.height / cellH)),
    };
  }

  // --- rendering ---------------------------------------------------------
  function rebuild(w, h) {
    cols = w; rows = h;
    grid.style.gridTemplateColumns = `repeat(${w}, ${cellW}px)`;
    grid.style.gridTemplateRows = `repeat(${h}, ${cellH}px)`;
    grid.textContent = '';
    cells = new Array(w * h);
    const frag = document.createDocumentFragment();
    for (let i = 0; i < w * h; i++) {
      const el = document.createElement('i');
      el.className = 'c';
      frag.appendChild(el);
      cells[i] = el;
    }
    grid.appendChild(frag);
  }

  const A_BOLD = 1, A_FAINT = 2, A_ITALIC = 4, A_UNDERLINE = 8,
        A_BLINK = 16, A_STRIKE = 64;

  function paint(u) {
    const i = u.y * cols + u.x;
    const el = cells[i];
    if (!el) return;
    // A continuation cell (w === 0) renders nothing and spans nothing: an empty
    // box would occupy a track and shift the rest of the row by one column for
    // every wide grapheme on it.
    if (u.w === 0) {
      el.textContent = '';
      el.style.display = 'none';
      return;
    }
    el.style.display = '';
    el.textContent = u.s || ' ';
    // textContent, never innerHTML: cell content is application data and an app
    // rendering a filename is not thinking about markup.
    el.style.gridColumn = u.w > 1 ? `${u.x + 1} / span ${u.w}` : '';
    el.style.color = u.f || '';
    el.style.background = u.b || '';
    const a = u.a || 0;
    el.style.fontWeight = (a & A_BOLD) ? '700' : '';
    el.style.opacity = (a & A_FAINT) ? '.6' : '';
    el.style.fontStyle = (a & A_ITALIC) ? 'italic' : '';
    const deco = [];
    if (a & A_UNDERLINE) deco.push('underline');
    if (a & A_STRIKE) deco.push('line-through');
    el.style.textDecoration = deco.join(' ');
    // Blink is deliberately not animated: browsers dropped it on purpose and it
    // is an accessibility hazard. The class lets a stylesheet opt in.
    el.classList.toggle('blink', !!(a & A_BLINK));
  }

  function applyFrame(m) {
    if (m.full || m.w !== cols || m.h !== rows) rebuild(m.w, m.h);
    const u = m.u || [];
    for (let i = 0; i < u.length; i++) paint(u[i]);
    if (m.cur) {
      cursor.style.display = m.cur.v ? '' : 'none';
      cursor.style.transform = `translate(${m.cur.x * cellW}px, ${m.cur.y * cellH}px)`;
      cursor.style.width = cellW + 'px';
      cursor.style.height = cellH + 'px';
      cursor.dataset.shape = String(m.cur.s || 0);
    }
    // Acknowledge only AFTER the frame is applied. Acknowledging on receipt
    // would let the server advance its baseline for a frame this client never
    // painted, and the next diff would omit those cells forever.
    send({ t: 'ack', rev: m.rev });
  }

  // --- input -------------------------------------------------------------
  function send(m) {
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(m));
  }

  function modsOf(e) {
    return { c: e.ctrlKey, a: e.altKey, s: e.shiftKey, m: e.metaKey };
  }

  // reserved walks the INJECTED rule table. There is no second implementation:
  // the rules come from the same Go values reservedShortcut evaluates, because
  // two hand-written copies of a security-relevant table drift and nothing
  // notices.
  function reserved(e) {
    for (const r of CFG.reserved) {
      const key = r.lower ? e.key.toLowerCase() : e.key;
      if (key !== r.key) continue;
      if (!r.need) return true;
      if (r.need === 'ctrl' && e.ctrlKey) return true;
      if (r.need === 'meta' && e.metaKey) return true;
      if (r.need === 'cmdOrCtrl' && (e.ctrlKey || e.metaKey)) return true;
    }
    return false;
  }

  // drain is the whole text machine.
  //
  // Emit whatever the capture element currently holds, then clear it
  // SYNCHRONOUSLY. Because the buffer is always drained, "the delta" is just
  // "the current value" — there is no baseline to diff against and so no
  // ambiguous string diff for anyone to get wrong later.
  //
  // Draining is also what keeps the DOM from becoming a keystroke log: without
  // it, every character the user ever typed — passwords included — would sit in
  // an element on a page reachable over the network.
  function drain() {
    const v = capture.value;
    if (v) {
      send({ t: 'text', x: v });
      // Cleared immediately, and before any await point, so a late
      // composition notification for already-committed work finds it EMPTY and
      // emits nothing. That is what makes event ordering stop mattering.
      capture.value = '';
    }
  }

  capture.addEventListener('compositionstart', () => { composing = true; });
  capture.addEventListener('compositionupdate', () => { /* state only */ });
  capture.addEventListener('compositionend', () => {
    // UI Events dispatches this AFTER the control is updated, which is the one
    // assumption this design makes and the browser matrix exists to verify.
    composing = false;
    drain();
  });
  capture.addEventListener('input', (e) => {
    if (composing || e.isComposing) return; // the commit will drain
    drain();
  });

  capture.addEventListener('keydown', (e) => {
    if (reserved(e)) return; // the browser keeps it; no preventDefault
    if (e.isComposing || e.getModifierState('AltGraph')) return;
    const named = NAMED.has(e.key);
    const modified = (e.ctrlKey || e.altKey || e.metaKey) && e.key.length === 1;
    if (!named && !modified) return; // it is text; the input event handles it
    e.preventDefault();
    send(Object.assign({
      t: 'key', k: e.key, rep: e.repeat,
      ag: e.getModifierState('AltGraph'), ic: e.isComposing,
    }, modsOf(e)));
  });

  capture.addEventListener('paste', (e) => {
    // preventDefault, so the clipboard text never enters the capture element:
    // no delta appears, and a cancelled paste cannot disturb the buffer or eat
    // a later keystroke.
    e.preventDefault();
    const text = (e.clipboardData || window.clipboardData).getData('text');
    if (text) send({ t: 'paste', x: text });
  });

  function cellAt(e) {
    const r = grid.getBoundingClientRect();
    return {
      x2: Math.max(0, Math.min(cols - 1, Math.floor((e.clientX - r.left) / cellW))),
      y2: Math.max(0, Math.min(rows - 1, Math.floor((e.clientY - r.top) / cellH))),
    };
  }

  function mouse(kind) {
    return (e) => {
      if (e.target !== grid && !grid.contains(e.target)) return;
      e.preventDefault();
      send(Object.assign({ t: 'mouse', mk: kind, btn: e.button }, cellAt(e), modsOf(e)));
      capture.focus({ preventScroll: true });
    };
  }
  grid.addEventListener('mousedown', mouse('down'));
  grid.addEventListener('mouseup', mouse('up'));
  grid.addEventListener('mousemove', mouse('move'));
  grid.addEventListener('wheel', (e) => {
    e.preventDefault();
    // Quantized to discrete steps: tui.MouseEvent has no delta field, so a
    // magnitude has nowhere faithful to go.
    const dir = Math.abs(e.deltaY) >= Math.abs(e.deltaX)
      ? (e.deltaY < 0 ? 'up' : 'down')
      : (e.deltaX < 0 ? 'left' : 'right');
    send(Object.assign({ t: 'mouse', mk: 'wheel', dir }, cellAt(e), modsOf(e)));
  }, { passive: false });

  window.addEventListener('focus', () => send({ t: 'focus', g: true }));
  window.addEventListener('blur', () => send({ t: 'focus', g: false }));

  let resizeTimer = 0;
  window.addEventListener('resize', () => {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(() => {
      if (!measure()) return;
      const s = gridSize();
      send({ t: 'resize', cols: s.cols, rows: s.rows, cw: cellW, ch: cellH });
    }, 100);
  });

  // --- connect -----------------------------------------------------------
  function connect(supplied) {
    // A mutable holder, so the reference can be dropped after the attempt. The
    // permanent open listener previously closed over the ticket and kept it
    // reachable for the page's whole lifetime (lector r1). No claim is made that
    // the string is erased from memory — a JS engine offers no such guarantee —
    // only that our code stops holding it.
    let cred = supplied || takeTicket();
    if (cred.session) sessionID = cred.session;
    const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    ws = new WebSocket(scheme + '//' + window.location.host + CFG.path);

    ws.addEventListener('open', () => {
      if (!measure()) {
        // Clear the credential and CLOSE on this branch too. It previously
        // returned early, leaving the permanent listener holding the ticket and
        // the socket open awaiting a hello that would never come — the r1
        // hygiene defect surviving on the failure path (lector r2).
        cred = { ticket: '', session: '' };
        status('could not measure the font');
        ws.close(1000, 'unmeasured');
        return;
      }
      const s = gridSize();
      rebuild(s.cols, s.rows);
      // Credentials go in the FIRST MESSAGE, never the URL.
      send({
        t: 'hello',
        ticket: cred.ticket, session: sessionID,
        cols: s.cols, rows: s.rows, cw: cellW, ch: cellH,
        pointer: window.matchMedia('(pointer: fine)').matches,
        dark: window.matchMedia('(prefers-color-scheme: dark)').matches,
        fontok: fontAgrees(),
      });
      // The credential has been presented; drop our reference to it.
      cred = { ticket: '', session: '' };
      capture.focus({ preventScroll: true });
    });

    ws.addEventListener('message', (ev) => {
      let m;
      try { m = JSON.parse(ev.data); } catch (_) { return; }
      if (m.t === 'frame') applyFrame(m);
      else if (m.t === 'ready') sessionID = m.session || sessionID;
      else if (m.t === 'bye') status(m.reason || 'session ended');
    });

    ws.addEventListener('close', (ev) => {
      // 1008 with an "unauthorized" reason means the policy refused us. If a
      // login route exists, offer it rather than leaving the user at a dead page.
      if (ev.code === 1008 && ev.reason && ev.reason.indexOf('unauthorized') === 0) {
        showLogin(ev.reason);
        return;
      }
      status(ev.reason ? 'disconnected: ' + ev.reason : 'disconnected');
    });
  }

  document.getElementById('lb').addEventListener('click', submitLogin);
  document.getElementById('lp').addEventListener('keydown', (e) => {
    if (e.key === 'Enter') submitLogin();
  });

  // --- password login ----------------------------------------------------
  //
  // The form does NOT put a password in the WebSocket hello. It POSTs to the
  // login route, which authenticates and returns a single-use ticket, and the
  // socket then attaches with that ticket like any other client. So the attach
  // path only ever carries one credential shape, and a captured hello is worth a
  // spent ticket rather than a reusable secret.
  function showLogin(message) {
    const box = document.getElementById('login');
    if (!CFG.loginPath) { status(message || 'unauthorized'); return; }
    box.hidden = false;
    document.getElementById('le').textContent = message || '';
    document.getElementById('lu').focus();
  }

  async function submitLogin() {
    const user = document.getElementById('lu');
    const pass = document.getElementById('lp');
    const err = document.getElementById('le');
    err.textContent = '';
    let body = JSON.stringify({ subject: user.value, password: pass.value });
    // Cleared before the request completes, not after: the field is a credential
    // and there is no reason for it to outlive the submission. No claim is made
    // that the string is erased from memory — a JS engine offers no such
    // guarantee — only that the DOM and our variables stop holding it.
    pass.value = '';
    try {
      const res = await fetch(CFG.loginPath, {
        method: 'POST',
        // same-origin, so the request cannot be aimed elsewhere by a redirect.
        credentials: 'omit',
        redirect: 'error',
        headers: { 'Content-Type': 'application/json' },
        body,
      });
      body = '';
      if (!res.ok) {
        const ref = res.headers.get('X-Auth-Attempt');
        err.textContent = 'Sign-in failed' + (ref ? ' (ref ' + ref + ')' : '');
        return;
      }
      const data = await res.json();
      document.getElementById('login').hidden = true;
      // Reconnect with the minted ticket.
      connect({ ticket: data.ticket, session: sessionID });
    } catch (e) {
      body = '';
      err.textContent = 'Sign-in unavailable';
    }
  }

  function status(text) {
    const el = document.getElementById('st');
    el.textContent = text;
    el.style.display = '';
  }

  // Keep the capture element focused: it is where all text arrives, and a click
  // anywhere in the terminal should put the caret back.
  document.addEventListener('click', () => capture.focus({ preventScroll: true }));

  connect();
})();
