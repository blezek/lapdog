/* Exercise the dedicated Races view in real Chrome. */
import { spawn } from 'node:child_process'
import { rmSync, writeFileSync } from 'node:fs'

const BASE = process.argv[2] ?? 'http://127.0.0.1:47047'
const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const PROFILE = '/tmp/chrome-lapdog-races'
const PORT = 9337
const SHOT = '/private/tmp/lapdog-races.png'
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

async function main() {
  rmSync(PROFILE, { recursive: true, force: true })
  const chrome = spawn(CHROME, [
    '--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check',
    `--remote-debugging-port=${PORT}`, `--user-data-dir=${PROFILE}`,
    '--window-size=1500,1100', 'about:blank',
  ], { stdio: 'ignore' })
  try {
    for (let i = 0; i < 60; i++) {
      try { if ((await fetch(`http://127.0.0.1:${PORT}/json/version`)).ok) break } catch { /* wait */ }
      await sleep(250)
    }
    const targets = await (await fetch(`http://127.0.0.1:${PORT}/json`)).json()
    const page = targets.find((target) => target.type === 'page')
    if (!page) throw new Error('Chrome created no page target')
    const ws = new WebSocket(page.webSocketDebuggerUrl)
    await new Promise((resolve, reject) => {
      ws.addEventListener('open', resolve, { once: true })
      ws.addEventListener('error', reject, { once: true })
    })
    let id = 0
    const pending = new Map()
    ws.addEventListener('message', (event) => {
      const message = JSON.parse(event.data)
      if (!message.id || !pending.has(message.id)) return
      const job = pending.get(message.id)
      pending.delete(message.id)
      message.error ? job.reject(new Error(message.error.message)) : job.resolve(message.result)
    })
    const send = (method, params = {}) => new Promise((resolve, reject) => {
      const messageID = ++id
      pending.set(messageID, { resolve, reject })
      ws.send(JSON.stringify({ id: messageID, method, params }))
    })
    const evaluate = async (expression) => {
      const result = await send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true })
      if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description ?? 'evaluation failed')
      return result.result.value
    }
    const assert = (condition, message) => { if (!condition) throw new Error(message) }

    await send('Page.enable')
    await send('Runtime.enable')
    await send('Page.navigate', { url: `${BASE}/races?range=all&car=999999` })
    await sleep(1800)

    const filtered = await evaluate(`(() => ({
      heading: document.querySelector('h1')?.textContent,
      active: document.querySelector('.nav-item.active')?.textContent.trim(),
      text: document.body.innerText,
      typeFilterVisible: [...document.querySelectorAll('button')]
        .some((button) => button.textContent.includes('Session types')),
    }))()`)
    assert(filtered.heading === 'Races' && filtered.active === 'Races', 'Races route or sidebar state is wrong')
    assert(!filtered.typeFilterVisible, 'race-only view exposes a contradictory session-type filter')
    assert(filtered.text.includes('No races match this filter.'), 'empty filtered state is not explicit')

    await send('Page.navigate', { url: `${BASE}/races?range=all` })
    await sleep(1800)
    const full = await evaluate(`(() => ({
      text: document.body.innerText,
      headers: [...document.querySelectorAll('th')].map((cell) => cell.textContent.trim()),
      rows: document.querySelectorAll('tbody tr').length,
    }))()`)
    const fullText = full.text.toLocaleLowerCase()
    assert(
      fullText.includes('race time') && fullText.includes('average positions gained'),
      `race summaries are missing: ${full.text.slice(0, 1200)}`,
    )
    for (const heading of ['Date', 'Track', 'Car', 'Driving', 'Grid', 'Finish', 'Grid to finish']) {
      assert(full.headers.some((value) => value.startsWith(heading)), `missing race column: ${heading}`)
    }
    assert(full.rows > 0, 'real capture dataset produced no race rows')

    const shot = await send('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true })
    writeFileSync(SHOT, Buffer.from(shot.data, 'base64'))
    console.log(`PASS: Races rendered ${full.rows} result rows -> ${SHOT}`)
    ws.close()
  } finally {
    chrome.kill('SIGKILL')
  }
}

main().catch((error) => {
  console.error(`FAIL: ${error.message}`)
  process.exitCode = 1
})
