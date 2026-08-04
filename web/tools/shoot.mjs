/*
 * Screenshots dashboard cards, and measures where a canvas's ink actually sits.
 *
 * "Looks centred" is not a check. For the calendar the grid's width follows how many
 * weeks are in range, so the only way to know it is centred is to find the leftmost
 * and rightmost painted columns and compare the two margins — at more than one range,
 * since a layout can be centred at one width and not another.
 *
 * No dependencies: Node's built-in WebSocket speaks the DevTools protocol directly.
 *
 *   node tools/shoot.mjs <card-title-substring> <range,range,...> [outdir]
 */

import { spawn } from 'node:child_process'
import { mkdirSync, rmSync, writeFileSync } from 'node:fs'

const TITLE = (process.argv[2] ?? 'CALENDAR').toUpperCase()
const RANGES = (process.argv[3] ?? '90,365,730').split(',')
const OUT = process.argv[4] ?? '/tmp/lapdog-shots'
const BASE = 'http://127.0.0.1:47047'
const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const PROFILE = '/tmp/chrome-lapdog-shoot'
const PORT = 9334

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

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
      '--force-device-scale-factor=2',
      '--window-size=1500,1600',
      'about:blank',
    ],
    { stdio: 'ignore' },
  )
  for (let i = 0; i < 60; i++) {
    try {
      if ((await fetch(`http://127.0.0.1:${PORT}/json/version`)).ok) return proc
    } catch {
      /* not up yet */
    }
    await sleep(250)
  }
  throw new Error('Chrome DevTools endpoint never became available')
}

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
 * inkExtent scans columns for any painted pixel and returns the first and last, so
 * the margins can be compared. It ignores the month and weekday labels by cropping
 * to the vertical band the cells occupy — those labels sit outside the grid and would
 * otherwise drag the measured left edge outwards.
 */
const PROBE = `
(function () {
  function card(titleText) {
    return [...document.querySelectorAll('.card')].find((c) => {
      const t = c.querySelector('.card-title');
      return t && t.textContent.trim().toUpperCase().includes(titleText);
    });
  }

  window.__shootCardBox = function (titleText) {
    const c = card(titleText);
    if (!c) return null;
    const r = c.getBoundingClientRect();
    return { x: r.x, y: r.y, width: r.width, height: r.height };
  };

  window.__shootInkExtent = function (titleText, topFrac, botFrac) {
    const c = card(titleText);
    if (!c) return { error: 'card not found: ' + titleText };
    const canvas = c.querySelector('canvas');
    if (!canvas) return { error: 'no canvas in card' };
    const ctx = canvas.getContext('2d');
    const w = canvas.width, h = canvas.height;
    const y0 = Math.floor(h * topFrac), y1 = Math.floor(h * botFrac);
    const px = ctx.getImageData(0, 0, w, h).data;

    let first = -1, last = -1;
    for (let x = 0; x < w; x++) {
      let inked = false;
      for (let y = y0; y < y1; y++) {
        const i = (y * w + x) * 4;
        const a = px[i + 3];
        if (a < 8) continue;
        const r = px[i], g = px[i + 1], b = px[i + 2];
        // Only count the cells, by requiring a blue cast.
        //
        // The month, weekday and year labels are grey, and the year label sits well
        // to the left of the grid — counting any non-white pixel measured the labels
        // and reported the grid as touching the edge when it was not. The sequential
        // ramp is blue, so chroma is what separates a cell from its labels.
        if (b <= r + 10) continue;
        inked = true; break;
      }
      if (inked) { if (first < 0) first = x; last = x; }
    }
    // Report in CSS pixels so the numbers are comparable to the card box.
    const dpr = w / canvas.getBoundingClientRect().width;
    return {
      canvasWidth: Math.round(w / dpr),
      left: first < 0 ? -1 : Math.round(first / dpr),
      right: last < 0 ? -1 : Math.round(last / dpr),
    };
  };
  return true;
})()
`

async function main() {
  mkdirSync(OUT, { recursive: true })
  const chrome = await launch()
  const { send, evaluate, close } = await connect()
  let worst = 0
  const clipped = []

  try {
    await send('Page.enable')
    await send('Runtime.enable')

    for (const range of RANGES) {
      await send('Page.navigate', { url: `${BASE}/dashboard?range=${range}` })
      await sleep(4000)
      await evaluate(PROBE)

      // The calendar's cells occupy the middle band; labels sit above and below it.
      const ink = await evaluate(`window.__shootInkExtent(${JSON.stringify(TITLE)}, 0.12, 0.80)`)
      if (ink?.error) throw new Error(ink.error)

      const box = await evaluate(`window.__shootCardBox(${JSON.stringify(TITLE)})`)
      const shot = await send('Page.captureScreenshot', {
        format: 'png',
        clip: { ...box, scale: 2 },
        captureBeyondViewport: true,
      })
      const file = `${OUT}/${TITLE.toLowerCase().replace(/\W+/g, '-')}-${range}.png`
      writeFileSync(file, Buffer.from(shot.data, 'base64'))

      const leftGap = ink.left
      const rightGap = ink.canvasWidth - ink.right
      const skew = Math.abs(leftGap - rightGap)
      worst = Math.max(worst, skew)
      const gridWidth = ink.right - ink.left

      // Ink touching an edge means the grid is wider than the canvas and is being
      // clipped, not centred. That case reports a skew of zero — perfectly balanced,
      // because it overflows equally in both directions — so it has to be caught
      // separately or it reads as the best possible result.
      if (leftGap <= 0 || rightGap <= 0) clipped.push(range)

      console.log(
        `  range ${String(range).padStart(3)}d: ` +
          `grid ${String(gridWidth).padStart(4)}px of ${ink.canvasWidth}px  ` +
          `margins ${String(leftGap).padStart(3)}/${String(rightGap).padStart(3)}  ` +
          `skew ${String(skew).padStart(3)}px  -> ${file}`,
      )
    }

    console.log()
    // The weekday labels sit outside the measured grid on the left only, so a small
    // rightward bias is expected and correct; a large one means it is not centred.
    if (clipped.length) {
      console.log(`  FAIL: clipped at range(s) ${clipped.join(', ')} — ink reaches the edge,`)
      console.log(`        so the grid is wider than the card and days are being cut off.`)
      process.exitCode = 1
    } else if (worst <= 40) {
      console.log(`  PASS: centred and fully visible at every range (worst skew ${worst}px).`)
    } else {
      console.log(`  FAIL: skew ${worst}px — the grid is not centred.`)
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
