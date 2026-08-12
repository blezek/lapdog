/* Drive the shared, multi-value, saved-filter workflow in real Chrome. */
import { spawn } from 'node:child_process'
import { rmSync, writeFileSync } from 'node:fs'

const BASE = process.argv[2] ?? 'http://127.0.0.1:47047'
const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const PROFILE = '/tmp/chrome-lapdog-filters'
const PORT = 9335
const SHOT = '/private/tmp/lapdog-filter-overhaul.png'
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

async function main() {
  rmSync(PROFILE, { recursive: true, force: true })
  const chrome = spawn(CHROME, [
    '--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check',
    `--remote-debugging-port=${PORT}`, `--user-data-dir=${PROFILE}`,
    '--window-size=1500,1000', 'about:blank',
  ], { stdio: 'ignore' })

  try {
    for (let i = 0; i < 60; i++) {
      try { if ((await fetch(`http://127.0.0.1:${PORT}/json/version`)).ok) break } catch { /* wait */ }
      await sleep(250)
    }
    const targets = await (await fetch(`http://127.0.0.1:${PORT}/json`)).json()
    const page = targets.find((target) => target.type === 'page')
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
    await send('Page.navigate', { url: `${BASE}/dashboard?range=30&sel=999` })
    await sleep(2500)

    const optionCounts = await evaluate(`(async () => {
      const count = async (name) => {
        const trigger = document.querySelector('button[aria-label="' + name + '"]');
        trigger.click();
        await new Promise((resolve) => setTimeout(resolve, 50));
        const result = document.getElementById(trigger.getAttribute('aria-controls'))
          .querySelectorAll('.filter-option input').length;
        trigger.click();
        await new Promise((resolve) => setTimeout(resolve, 50));
        return result;
      };
      return { cars: await count('Cars'), tracks: await count('Tracks') };
    })()`)
    assert(optionCounts.cars >= 2 && optionCounts.tracks >= 2, 'fixture needs at least two cars and tracks')

    const coordinated = await evaluate(`(async () => {
      const cars = document.querySelector('button[aria-label="Cars"]');
      const tracks = document.querySelector('button[aria-label="Tracks"]');
      cars.click();
      await new Promise((resolve) => setTimeout(resolve, 50));
      tracks.click();
      await new Promise((resolve) => setTimeout(resolve, 50));
      return {
        cars: cars.getAttribute('aria-expanded'),
        tracks: tracks.getAttribute('aria-expanded'),
        panels: document.querySelectorAll('[id^="filter-panel-"]').length,
      };
    })()`)
    assert(
      coordinated.cars === 'false' && coordinated.tracks === 'true' && coordinated.panels === 1,
      `filter popovers are not coordinated: ${JSON.stringify(coordinated)}`,
    )
    await evaluate(`document.querySelector('h1').dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))`)
    assert(
      await evaluate(`document.querySelectorAll('[id^="filter-panel-"]').length`) === 0,
      'outside click did not close the filter popover',
    )
    const escaped = await evaluate(`(async () => {
      const date = document.querySelector('[data-filter-trigger="date"]');
      date.click();
      await new Promise((resolve) => setTimeout(resolve, 50));
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
      await new Promise((resolve) => setTimeout(resolve, 50));
      return {
        expanded: date.getAttribute('aria-expanded'),
        focused: document.activeElement === date,
      };
    })()`)
    assert(
      escaped.expanded === 'false' && escaped.focused,
      `Escape did not close the popover and restore focus: ${JSON.stringify(escaped)}`,
    )

    for (const name of ['Cars', 'Tracks']) {
      for (let index = 0; index < 2; index++) {
        await evaluate(`(async () => {
          const trigger = document.querySelector('button[aria-label="${name}"]');
          if (trigger.getAttribute('aria-expanded') !== 'true') {
            trigger.click();
            await new Promise((resolve) => setTimeout(resolve, 50));
          }
          document.getElementById(trigger.getAttribute('aria-controls'))
            .querySelectorAll('.filter-option input')[${index}].click();
        })()`)
        await sleep(350)
      }
      await evaluate(`document.querySelector('button[aria-label="${name}"]').click()`)
    }

    const selectedQuery = await evaluate('location.search')
    assert(/car=\d+%2C\d+/.test(selectedQuery), `two cars missing from ${selectedQuery}`)
    assert(/track=\d+%2C\d+/.test(selectedQuery), `two tracks missing from ${selectedQuery}`)

    await evaluate(`(async () => {
      const cars = document.querySelector('button[aria-label="Cars"]');
      cars.click();
      await new Promise((resolve) => setTimeout(resolve, 50));
      document.querySelector('#filter-panel-car .filter-menu-clear').click();
    })()`)
    await sleep(350)
    const clearedQuery = await evaluate('location.search')
    assert(!clearedQuery.includes('car='), `per-menu clear left cars in ${clearedQuery}`)
    assert(clearedQuery.includes('track='), `per-menu clear removed another facet from ${clearedQuery}`)
    for (let index = 0; index < 2; index++) {
      await evaluate(`(async () => {
        const cars = document.querySelector('button[aria-label="Cars"]');
        if (cars.getAttribute('aria-expanded') !== 'true') {
          cars.click();
          await new Promise((resolve) => setTimeout(resolve, 50));
        }
        document.querySelectorAll('#filter-panel-car .filter-option input')[${index}].click();
      })()`)
      await sleep(350)
    }
    await evaluate(`document.querySelector('button[aria-label="Cars"]').click()`)

    await evaluate(`(async () => {
      document.querySelector('[data-filter-trigger="saved"]').click();
      await new Promise((resolve) => setTimeout(resolve, 50));
      const input = document.querySelector('input[aria-label="Filter set name"]');
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
      setter.call(input, 'Two-by-two'); input.dispatchEvent(new Event('input', { bubbles: true }));
      input.dispatchEvent(new Event('change', { bubbles: true }));
      [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Save').click();
    })()`)
    await sleep(500)
    assert(await evaluate(`JSON.parse(localStorage.getItem('lapdog.savedFilters.v1')).length`) === 1, 'filter set not saved')

    await evaluate(`[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Clear').click()`)
    await sleep(400)
    assert(!(await evaluate('location.search')).includes('car='), 'clear did not remove car filter')

    await evaluate(`(async () => {
      if (!document.querySelector('select[aria-label="Saved filter set"]')) {
        document.querySelector('[data-filter-trigger="saved"]').click();
        await new Promise((resolve) => setTimeout(resolve, 50));
      }
      const select = document.querySelector('select[aria-label="Saved filter set"]');
      const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set;
      setter.call(select, select.options[1].value);
      select.dispatchEvent(new Event('change', { bubbles: true }));
    })()`)
    await sleep(700)
    assert((await evaluate('location.search')).includes('car='), 'saved filter did not reload')

    await evaluate(`document.querySelector('a[href^="/sessions"]').click()`)
    await sleep(700)
    const destination = await evaluate('location.pathname + location.search')
    assert(destination.startsWith('/sessions?'), `navigation lost filters: ${destination}`)
    assert(destination.includes('car=') && destination.includes('track='), `navigation lost dimensions: ${destination}`)
    assert(!destination.includes('sel='), `navigation leaked local selection: ${destination}`)

    await evaluate(`document.querySelector('[data-filter-trigger="saved"]').click()`)
    await sleep(150)
    const shot = await send('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true })
    writeFileSync(SHOT, Buffer.from(shot.data, 'base64'))

    await evaluate(`(async () => {
      if (!document.querySelector('select[aria-label="Saved filter set"]')) {
        document.querySelector('[data-filter-trigger="saved"]').click();
        await new Promise((resolve) => setTimeout(resolve, 50));
      }
      const select = document.querySelector('select[aria-label="Saved filter set"]');
      const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set;
      setter.call(select, select.options[1].value);
      select.dispatchEvent(new Event('change', { bubbles: true }));
    })()`)
    await sleep(300)
    await evaluate(`[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Delete').click()`)
    await sleep(300)
    assert(await evaluate(`JSON.parse(localStorage.getItem('lapdog.savedFilters.v1')).length`) === 0, 'filter set not deleted')

    console.log(`PASS: multi-select, cross-view propagation, save, reload, and delete -> ${SHOT}`)
    ws.close()
  } finally {
    chrome.kill('SIGKILL')
  }
}

main().catch((error) => {
  console.error(`FAIL: ${error.message}`)
  process.exitCode = 1
})
