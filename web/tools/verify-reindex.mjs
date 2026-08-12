/* Exercise the Settings capture re-index workflow in real Chrome. */
import { spawn } from 'node:child_process'
import { rmSync, writeFileSync } from 'node:fs'

const BASE = process.argv[2] ?? 'http://127.0.0.1:47047'
const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const PROFILE = '/tmp/chrome-lapdog-reindex'
const PORT = 9336
const SHOT = '/private/tmp/lapdog-reindex-settings.png'
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
      const timer = setTimeout(() => {
        pending.delete(messageID)
        reject(new Error(`${method} timed out`))
      }, 10000)
      pending.set(messageID, {
        resolve: (value) => { clearTimeout(timer); resolve(value) },
        reject: (error) => { clearTimeout(timer); reject(error) },
      })
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
    await send('Page.navigate', { url: `${BASE}/settings` })
    await sleep(1800)

    const initial = await evaluate(`(() => {
      const button = [...document.querySelectorAll('button')]
        .find((item) => item.textContent.trim() === 'Re-index captures');
      return { found: !!button, disabled: button?.disabled, text: document.body.innerText };
    })()`)
    assert(initial.found && !initial.disabled, 're-index control is missing or disabled while disconnected')
    assert(initial.text.includes('Existing sessions are updated rather than duplicated'), 'debugging scope is not explained')

    await evaluate(`(() => {
      window.confirm = () => true;
      [...document.querySelectorAll('button')]
        .find((item) => item.textContent.trim() === 'Re-index captures').click();
    })()`)

    let status
    for (let i = 0; i < 120; i++) {
      status = await evaluate(`fetch('/api/captures/reindex').then((response) => response.json())`)
      if (status.state !== 'running' && status.state !== 'idle') break
      await sleep(100)
    }
    assert(status.state === 'complete', `re-index did not complete: ${JSON.stringify(status)}`)
    assert(status.replayed === status.total && status.failed === 0, `captures were not all replayed: ${JSON.stringify(status)}`)

    for (let i = 0; i < 30; i++) {
      if (await evaluate(`document.body.innerText.includes('Last run replayed')`)) break
      await sleep(100)
    }
    const text = await evaluate('document.body.innerText')
    assert(text.includes(`Last run replayed ${status.replayed} of ${status.total} capture(s)`), 'completion is not visible in Settings')
    assert(text.includes(`processed ${status.segments} session segment(s)`), 'segment count is not visible in Settings')

    const shot = await send('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true })
    writeFileSync(SHOT, Buffer.from(shot.data, 'base64'))
    console.log(`PASS: Settings replayed ${status.replayed} captures and processed ${status.segments} segments -> ${SHOT}`)
    ws.close()
  } finally {
    chrome.kill('SIGKILL')
  }
}

main().catch((error) => {
  console.error(`FAIL: ${error.message}`)
  process.exitCode = 1
})
