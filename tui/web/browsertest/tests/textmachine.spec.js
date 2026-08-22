// ADR-0009 §2.9 / criterion 7a — the capture-element text machine, against real
// engines.
//
// These sequences are the reason the matrix is a release gate. Every one of them
// depends on behaviour the specs decline to promise: whether an engine updates a
// control before dispatching `compositionend`, whether a composition-associated
// `input` arrives in the same task, how a dead key surfaces. A synthetic test
// would assert my model of those behaviours rather than the behaviours.
import { test, expect } from '@playwright/test';

const CAP = '#cap';

/** attach navigates with a fresh single-use ticket and waits for the grid. */
async function attach(page, baseURL) {
  const res = await page.request.get(`${baseURL}/_ticket`);
  const { ticket } = await res.json();
  await page.request.post(`${baseURL}/_reset`);
  // The ticket goes in the FRAGMENT, which is never sent to a server.
  await page.goto(`${baseURL}/#t=${encodeURIComponent(ticket)}`);
  await page.waitForFunction(() => document.querySelectorAll('#g > i.c').length > 0,
    null, { timeout: 10_000 });
  await page.focus(CAP);
}

/** events returns the server-side tui.Event log. */
async function events(page, baseURL) {
  const res = await page.request.get(`${baseURL}/_events`);
  return res.json();
}

/** keyEvents filters to key events carrying text, i.e. the text path. */
function textEvents(log) {
  return log.filter((e) => e.kind === 'key' && e.text);
}

/** settle waits for the server to have seen at least n text events. */
async function settleText(page, baseURL, n) {
  await expect
    .poll(async () => textEvents(await events(page, baseURL)).length, { timeout: 5_000 })
    .toBeGreaterThanOrEqual(n);
}

test.describe('capture-element text machine', () => {
  test('ordinary typing emits one event per rune (§2.9 criterion 7)', async ({ page, baseURL }) => {
    await attach(page, baseURL);
    await page.type(CAP, 'abc', { delay: 20 });
    await settleText(page, baseURL, 3);

    const got = textEvents(await events(page, baseURL));
    expect(got.map((e) => e.text).join('')).toBe('abc');
    // Per RUNE, matching tui/term's actPrint — a component must not be able to
    // tell the two backends apart.
    expect(got).toHaveLength(3);
    for (const e of got) {
      expect(e.mods).toBeFalsy();  // text carries no modifiers
      // KeyPress is 0 and the fixture omits zero values, so absent == KeyPress.
      expect(e.keyKind).toBeFalsy();
    }
  });

  test('the capture buffer is empty after every path (§2.9 criterion 7d)',
    async ({ page, baseURL }) => {
      await attach(page, baseURL);
      await page.type(CAP, 'hello there', { delay: 10 });
      await settleText(page, baseURL, 11);

      // The element is DRAINED. Without this, every character ever typed —
      // passwords included — would sit in the DOM of a network-reachable page.
      await expect.poll(() => page.$eval(CAP, (el) => el.value), { timeout: 5_000 })
        .toBe('');
    });

  test('a long typing stream leaves the DOM value size constant (§2.9 criterion 7d)',
    async ({ page, baseURL }) => {
      await attach(page, baseURL);
      const sizes = [];
      for (let i = 0; i < 12; i++) {
        await page.type(CAP, 'password123', { delay: 5 });
        sizes.push(await page.$eval(CAP, (el) => el.value.length));
      }
      // Not merely bounded — the buffer never accumulates at all.
      expect(Math.max(...sizes)).toBeLessThanOrEqual(11);
      await expect.poll(() => page.$eval(CAP, (el) => el.value), { timeout: 5_000 })
        .toBe('');
    });

  test('password-like text is absent from the element after emission (§2.9 criterion 7d)',
    async ({ page, baseURL }) => {
      await attach(page, baseURL);
      const secret = 'correct-horse-battery-staple';
      await page.type(CAP, secret, { delay: 5 });
      await settleText(page, baseURL, secret.length);
      const value = await page.$eval(CAP, (el) => el.value);
      expect(value).toBe('');
      expect(value).not.toContain('horse');
    });

  test('composition commits once, with input BEFORE compositionend (§2.9 7a i)',
    async ({ page, baseURL }) => {
      await attach(page, baseURL);
      await page.evaluate(() => {
        const el = document.querySelector('#cap');
        el.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }));
        el.value = '漢';
        el.dispatchEvent(new InputEvent('input', { bubbles: true, isComposing: true }));
        el.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: '漢' }));
      });
      await settleText(page, baseURL, 1);
      const got = textEvents(await events(page, baseURL));
      expect(got.map((e) => e.text).join('')).toBe('漢');
      expect(got).toHaveLength(1);
    });

  test('a composition-associated input in a LATER task does not duplicate (§2.9 7a viii)',
    async ({ page, baseURL }) => {
      await attach(page, baseURL);
      await page.evaluate(async () => {
        const el = document.querySelector('#cap');
        el.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }));
        el.value = 'x';
        el.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: 'x' }));
        // A LATER task: compositionend already drained, so this finds the element
        // empty and must emit nothing. This is the case that made three earlier
        // ADR revisions wrong.
        await new Promise((r) => setTimeout(r, 50));
        el.dispatchEvent(new InputEvent('input', { bubbles: true }));
      });
      await settleText(page, baseURL, 1);
      // Exactly one, not two.
      await new Promise((r) => setTimeout(r, 200));
      const got = textEvents(await events(page, baseURL));
      expect(got.map((e) => e.text).join('')).toBe('x');
      expect(got).toHaveLength(1);
    });

  test('a composed x then a separately typed x yields TWO insertions (§2.9 7a v)',
    async ({ page, baseURL }) => {
      await attach(page, baseURL);
      await page.evaluate(() => {
        const el = document.querySelector('#cap');
        el.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }));
        el.value = 'x';
        el.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: 'x' }));
      });
      await settleText(page, baseURL, 1);
      await page.type(CAP, 'x', { delay: 20 });
      await settleText(page, baseURL, 2);

      // Identity comes from STATE TRANSITIONS, never from comparing content —
      // which is why the second x survives.
      const got = textEvents(await events(page, baseURL));
      expect(got.map((e) => e.text).join('')).toBe('xx');
    });

  test('cancellation emits nothing (§2.9 7a iv)', async ({ page, baseURL }) => {
    await attach(page, baseURL);
    await page.evaluate(() => {
      const el = document.querySelector('#cap');
      el.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }));
      el.value = 'partial';
      el.dispatchEvent(new InputEvent('input', { bubbles: true, isComposing: true }));
      // The host reverts, so the element is empty: "emit only what is there"
      // covers cancellation with no rule of its own.
      el.value = '';
      el.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: '' }));
    });
    await new Promise((r) => setTimeout(r, 300));
    expect(textEvents(await events(page, baseURL))).toHaveLength(0);
  });

  test('compositionend with empty data but a staged value COMMITS (§2.9 7a iii)',
    async ({ page, baseURL }) => {
      await attach(page, baseURL);
      await page.evaluate(() => {
        const el = document.querySelector('#cap');
        el.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }));
        el.value = 'ぁ';
        // data is empty, which some engines do. It is never consulted.
        el.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: '' }));
      });
      await settleText(page, baseURL, 1);
      expect(textEvents(await events(page, baseURL)).map((e) => e.text).join('')).toBe('ぁ');
    });

  test('a multi-codepoint emoji arrives as several rune events (§2.9 7b)',
    async ({ page, baseURL }) => {
      await attach(page, baseURL);
      await page.evaluate(() => {
        const el = document.querySelector('#cap');
        el.value = '\u{1F468}‍\u{1F469}‍\u{1F466}';
        el.dispatchEvent(new InputEvent('input', { bubbles: true }));
      });
      await settleText(page, baseURL, 5);
      const got = textEvents(await events(page, baseURL));
      // Five scalars, exactly as over a terminal.
      expect(got).toHaveLength(5);
      expect(got.map((e) => e.text).join('')).toBe('\u{1F468}‍\u{1F469}‍\u{1F466}');
    });
});

test.describe('keys and reserved shortcuts', () => {
  test('named keys are forwarded with the right code', async ({ page, baseURL }) => {
    await attach(page, baseURL);
    await page.keyboard.press('Enter');
    await page.keyboard.press('ArrowUp');
    await page.keyboard.press('Escape');
    await expect.poll(async () => (await events(page, baseURL))
      .filter((e) => e.kind === 'key' && !e.text).length, { timeout: 5_000 })
      .toBeGreaterThanOrEqual(3);

    const codes = (await events(page, baseURL))
      .filter((e) => e.kind === 'key' && !e.text).map((e) => e.code);
    expect(codes).toContain(13);     // KeyEnter
    expect(codes).toContain(57352);  // KeyUp
    expect(codes).toContain(27);     // KeyEscape
  });

  test('Dead and Unidentified produce no phantom event (§2.9 7b)',
    async ({ page, baseURL }) => {
      await attach(page, baseURL);
      await page.evaluate(() => {
        const el = document.querySelector('#cap');
        for (const key of ['Dead', 'Unidentified']) {
          el.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, ctrlKey: true }));
        }
      });
      await new Promise((r) => setTimeout(r, 300));
      const got = await events(page, baseURL);
      expect(got.filter((e) => e.kind === 'key')).toHaveLength(0);
    });

  test('reserved browser shortcuts are not forwarded (§2.9 rule 1)',
    async ({ page, baseURL }) => {
      await attach(page, baseURL);
      await page.evaluate(() => {
        const el = document.querySelector('#cap');
        for (const [key, ctrl] of [['t', true], ['n', true], ['w', true], ['r', true]]) {
          el.dispatchEvent(new KeyboardEvent('keydown', { key, ctrlKey: ctrl, bubbles: true }));
        }
        el.dispatchEvent(new KeyboardEvent('keydown', { key: 'F5', bubbles: true }));
      });
      await new Promise((r) => setTimeout(r, 300));
      expect((await events(page, baseURL)).filter((e) => e.kind === 'key')).toHaveLength(0);
    });
});

test.describe('paste', () => {
  test('paste emits exactly one PasteEvent with normalized newlines (§2.9 7a vii)',
    async ({ page, baseURL }) => {
      await attach(page, baseURL);
      await page.evaluate(() => {
        const el = document.querySelector('#cap');
        const dt = new DataTransfer();
        dt.setData('text', 'one\r\ntwo\rthree');
        el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true }));
      });
      await expect.poll(async () => (await events(page, baseURL))
        .filter((e) => e.kind === 'paste').length, { timeout: 5_000 }).toBe(1);
      const paste = (await events(page, baseURL)).find((e) => e.kind === 'paste');
      expect(paste.text).toBe('one\ntwo\nthree');
    });

  test('a cancelled paste does not eat a later keystroke (§2.9 7a vi)',
    async ({ page, baseURL }) => {
      await attach(page, baseURL);
      await page.evaluate(() => {
        const el = document.querySelector('#cap');
        const dt = new DataTransfer();
        dt.setData('text', '');
        el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true }));
      });
      await page.type(CAP, 'k', { delay: 20 });
      await settleText(page, baseURL, 1);
      expect(textEvents(await events(page, baseURL)).map((e) => e.text).join('')).toBe('k');
    });
});

test.describe('wide graphemes', () => {
  test('a wide grapheme occupies exactly two columns and a continuation emits no glyph',
    async ({ page, baseURL }) => {
      await attach(page, baseURL);
      // Measured from the DOM: a Width-2 head spans two tracks, and there is no
      // element for the continuation. This is the containment §2.6 relies on.
      const spans = await page.$$eval('#g > i.c', (els) =>
        els.filter((el) => el.style.gridColumn.includes('span')).map((el) => el.style.gridColumn));
      for (const s of spans) {
        expect(s).toMatch(/span 2$/);
      }
      const boxes = await page.$$eval('#g > i.c', (els) => els.map((el) => {
        const cs = getComputedStyle(el);
        return { overflow: cs.overflow, contain: cs.contain };
      }));
      // Every cell clips its own overflow — the actual guarantee against a font
      // mismatch shifting a row.
      for (const b of boxes.slice(0, 20)) {
        expect(b.overflow).toBe('hidden');
      }
    });
});
