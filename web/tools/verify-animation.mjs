/*
 * Verifies that a page's charts tween between filters instead of snapping.
 *
 * Reasoning about merge modes is not evidence. This drives a real browser, changes a
 * filter, and samples the rendered canvas part way through the transition. A chart
 * that animates shows a bar length strictly between its old and new values; a chart
 * that jumps reaches its new state in a single frame.
 *
 * It also checks the canvas element is the same DOM node afterwards, which is the
 * thing that actually broke: gating a panel on isLoading unmounted the chart on every
 * filter change, so ECharts disposed the instance and the replacement had no previous
 * state to animate from.
 *
 * No dependencies: Node's built-in WebSocket speaks the DevTools protocol directly.
 *
 * The page and the card it probes are both parameters rather than only the
 * dashboard's, because the Cars and Tracks pages carry the same keepPrevious
 * discipline on five queries and were never exercised by this tool. The card
 * title has to be passed alongside the path: each page names its chart-bearing
 * card differently, and there is no way to infer one from the other.
 *
 *   node tools/verify-animation.mjs [baseUrl] [pagePath] [cardTitle]
 *   node tools/verify-animation.mjs http://127.0.0.1:47047 /cars?range=90 "BEST LAP BY MONTH"
 */

import { spawn } from 'node:child_process'
import { rmSync } from 'node:fs'

const BASE = process.argv[2] ?? 'http://127.0.0.1:47047'
const PAGE_PATH = process.argv[3] ?? '/dashboard?range=90'
const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const PROFILE = '/tmp/chrome-lapdog-verify'
const PORT = 9333

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

/** launch starts headless Chrome with the DevTools endpoint open. */
async function launch() {
  rmSync(PROFILE, { recursive: true, force: true })
  const proc = spawn(
    CHROME,
    [
      '--headless=new',
      '--disable-gpu',
      '--hide-scrollbars',
      '--no-first-run',
      '--no-default-browser-check',
      `--remote-debugging-port=${PORT}`,
      `--user-data-dir=${PROFILE}`,
      '--window-size=1600,1400',
      'about:blank',
    ],
    { stdio: 'ignore' },
  )

  // Wait for the endpoint rather than guessing a fixed delay.
  for (let i = 0; i < 60; i++) {
    try {
      const res = await fetch(`http://127.0.0.1:${PORT}/json/version`)
      if (res.ok) return proc
    } catch {
      /* not up yet */
    }
    await sleep(250)
  }
  throw new Error('Chrome DevTools endpoint never became available')
}

/** connect opens a session against the first page target. */
async function connect() {
  const targets = await (await fetch(`http://127.0.0.1:${PORT}/json`)).json()
  const page = targets.find((t) => t.type === 'page')
  if (!page) throw new Error('no page target')

  const ws = new WebSocket(page.webSocketDebuggerUrl)
  await new Promise((res, rej) => {
    ws.addEventListener('open', res, { once: true })
    ws.addEventListener('error', rej, { once: true })
  })

  let id = 0
  const pending = new Map()
  ws.addEventListener('message', (ev) => {
    const msg = JSON.parse(ev.data)
    if (msg.id && pending.has(msg.id)) {
      const { resolve, reject } = pending.get(msg.id)
      pending.delete(msg.id)
      msg.error ? reject(new Error(msg.error.message)) : resolve(msg.result)
    }
  })

  const send = (method, params = {}) =>
    new Promise((resolve, reject) => {
      const mid = ++id
      pending.set(mid, { resolve, reject })
      ws.send(JSON.stringify({ id: mid, method, params }))
    })

  /** evaluate runs an expression in the page and returns its value. */
  const evaluate = async (expression) => {
    const r = await send('Runtime.evaluate', {
      expression,
      returnByValue: true,
      awaitPromise: true,
    })
    if (r.exceptionDetails) {
      throw new Error(r.exceptionDetails.exception?.description ?? 'evaluation failed')
    }
    return r.result.value
  }

  return { send, evaluate, close: () => ws.close() }
}

/*
 * Injected into the page.
 *
 * The probe checksums the chart's pixels rather than measuring a bar.
 *
 * Measuring the longest bar does not work: a value axis rescales to its data, so the
 * longest bar occupies nearly the same width whatever the numbers are. What separates
 * a tween from a jump is whether the painted pixels pass through intermediate states,
 * so the probe samples a cheap checksum repeatedly and counts how many distinct
 * frames appear.
 */
const MEASURE = `
(function () {
  function cardCanvas(titleText) {
    const cards = [...document.querySelectorAll('.card')];
    const card = cards.find((c) => {
      const t = c.querySelector('.card-title');
      return t && t.textContent.trim().toUpperCase().includes(titleText);
    });
    if (!card) return null;
    return card.querySelector('canvas');
  }

  window.__lapdogChecksum = function (titleText) {
    const canvas = cardCanvas(titleText);
    if (!canvas) return { error: 'card or canvas not found: ' + titleText };
    const ctx = canvas.getContext('2d');
    const { width: w, height: h } = canvas;
    const px = ctx.getImageData(0, 0, w, h).data;

    // Sum of channel values over a coarse stride: cheap, and sensitive to any change
    // in what is painted without being sensitive to antialiasing noise.
    let sum = 0, ink = 0;
    for (let i = 0; i < px.length; i += 4 * 17) {
      const r = px[i], g = px[i + 1], b = px[i + 2], a = px[i + 3];
      sum = (sum + r * 3 + g * 5 + b * 7 + a) % 2147483647;
      const light = r > 235 && g > 235 && b > 235;
      const dark = r < 45 && g < 45 && b < 45;
      if (a > 8 && !light && !dark) ink++;
    }
    return { sum, ink };
  };

  window.__lapdogStamp = function () {
    let n = 0;
    for (const c of document.querySelectorAll('canvas')) {
      if (!c.__lapdogId) c.__lapdogId = 'canvas-' + (++n) + '-' + Math.random().toString(36).slice(2, 8);
    }
    return [...document.querySelectorAll('canvas')].map((c) => c.__lapdogId);
  };

  window.__lapdogSetRange = function (value) {
    const select = [...document.querySelectorAll('select')].find((s) =>
      [...s.options].some((o) => o.value === value));
    if (!select) return false;
    const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set;
    setter.call(select, value);
    select.dispatchEvent(new Event('change', { bubbles: true }));
    return true;
  };
  return true;
})()
`

const TITLE = (process.argv[4] ?? 'DRIVING HOURS BY CATEGORY').toUpperCase()

async function main() {
  const chrome = await launch()
  const { send, evaluate, close } = await connect()

  try {
    await send('Page.enable')
    await send('Runtime.enable')
    await send('Page.navigate', { url: `${BASE}${PAGE_PATH}` })
    await sleep(3500)

    await evaluate(MEASURE)
    const stampsBefore = await evaluate('window.__lapdogStamp()')

    const probe = `window.__lapdogChecksum(${JSON.stringify(TITLE)})`
    const before = await evaluate(probe)
    if (before.error) throw new Error(before.error)

    const changed = await evaluate(`window.__lapdogSetRange('365')`)
    if (!changed) throw new Error('could not find the date-range control')

    // Sample rapidly across the animation window, then once well past it.
    const frames = []
    for (let i = 0; i < 10; i++) {
      await sleep(55)
      frames.push(await evaluate(probe))
    }
    await sleep(900)
    const settled = await evaluate(probe)

    const stampsAfter = await evaluate(
      '[...document.querySelectorAll("canvas")].map(c => c.__lapdogId)',
    )
    const survived = stampsBefore.filter((id) => id && stampsAfter.includes(id)).length

    const sums = frames.map((f) => f.sum)
    const distinct = new Set([...sums, settled.sum, before.sum]).size
    const distinctDuring = new Set(sums).size
    // Frames that match neither endpoint are genuine intermediate states.
    const intermediate = sums.filter((v) => v !== before.sum && v !== settled.sum).length

    console.log('  canvases surviving   :', `${survived}/${stampsBefore.length}`,
      '(0 would mean the charts were unmounted)')
    console.log('  ink before / settled :', before.ink, '/', settled.ink)
    console.log('  distinct frames       :', distinctDuring, 'during, ', distinct, 'overall')
    console.log('  intermediate frames   :', intermediate, 'of', sums.length)

    console.log()
    if (survived === 0) {
      console.log('  FAIL: every canvas was replaced, so the chart cannot animate.')
      process.exitCode = 1
    } else if (before.sum === settled.sum) {
      console.log('  INCONCLUSIVE: the chart looks identical before and after, so there')
      console.log('  is nothing to animate. Pick filter states that differ.')
      process.exitCode = 2
    } else if (intermediate >= 2) {
      console.log('  PASS: the chart passed through', intermediate, 'intermediate frames')
      console.log('        between the old and new states, and was not remounted.')
    } else {
      console.log('  FAIL: the chart went straight from the old state to the new one')
      console.log('        with no intermediate frames. It is jumping, not animating.')
      process.exitCode = 1
    }
  } finally {
    close()
    chrome.kill('SIGKILL')
  }
}

main().catch((err) => {
  console.error('  error:', err.message)
  process.exitCode = 1
})
