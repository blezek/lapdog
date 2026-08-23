/* Drive the shared, multi-value, saved-filter workflow in real Chrome. */
import { spawn } from 'node:child_process'
import { rmSync, writeFileSync } from 'node:fs'

const BASE = process.argv[2] ?? 'http://127.0.0.1:47047'
const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const PROFILE = '/tmp/chrome-lapdog-filters'
const PORT = 9335
const SHOT = '/private/tmp/lapdog-filter-overhaul.png'
const DEBUG_SHOT = '/private/tmp/lapdog-date-filter-debug.png'
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

    await send('Page.navigate', { url: `${BASE}/dashboard?range=today` })
    await sleep(700)
    const debugBounds = await evaluate(`(async () => {
      document.querySelector('[data-filter-trigger="date"]').click();
      for (let i = 0; i < 20 && !document.querySelector('[aria-label="Resolved date filter bounds"]'); i++) {
        await new Promise((resolve) => setTimeout(resolve, 50));
      }
      return {
        text: document.querySelector('[aria-label="Resolved date filter bounds"]')?.textContent ?? '',
      };
    })()`)
    assert(debugBounds.text.includes('Beginning') && debugBounds.text.includes('End'),
      `debug date bounds are missing: ${JSON.stringify(debugBounds)}`)
    const shownDays = debugBounds.text.match(/\d{4}-\d{2}-\d{2}/g) ?? []
    assert(shownDays.length === 2 && shownDays[0] === shownDays[1],
      `Today does not begin and end on one server-local date: ${JSON.stringify(debugBounds)}`)
    assert(debugBounds.text.includes('00:00:00') && debugBounds.text.includes('23:59:59'),
      `Today does not cover the full server-local day: ${JSON.stringify(debugBounds)}`)
    assert(!debugBounds.text.includes('No lower bound') && !debugBounds.text.includes('No upper bound'),
      `Today unexpectedly has an open bound: ${JSON.stringify(debugBounds)}`)
    const debugShot = await send('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true })
    writeFileSync(DEBUG_SHOT, Buffer.from(debugShot.data, 'base64'))
    await evaluate(`document.querySelector('[data-filter-trigger="date"]').click()`)

    const presetDismissal = await evaluate(`(async () => {
      const trigger = () => document.querySelector('[data-filter-trigger="date"]');
      const choose = (label) => [...document.querySelectorAll('#filter-panel-date .chip')]
        .find((button) => button.textContent.trim() === label);
      trigger().click();
      await new Promise((resolve) => setTimeout(resolve, 50));
      choose('Yesterday').click();
      await new Promise((resolve) => setTimeout(resolve, 400));
      const preset = {
        expanded: trigger().getAttribute('aria-expanded'),
        panel: document.getElementById('filter-panel-date') != null,
      };
      trigger().click();
      for (let i = 0; i < 20 && !document.getElementById('filter-panel-date'); i++) {
        await new Promise((resolve) => setTimeout(resolve, 50));
      }
      choose('Custom range').click();
      await new Promise((resolve) => setTimeout(resolve, 100));
      const custom = {
        expanded: trigger().getAttribute('aria-expanded'),
        panel: document.getElementById('filter-panel-date') != null,
      };
      trigger().click();
      return { preset, custom };
    })()`)
    assert(
      presetDismissal.preset.expanded === 'false' && !presetDismissal.preset.panel,
      `one-click date preset stayed open: ${JSON.stringify(presetDismissal)}`,
    )
    assert(
      presetDismissal.custom.expanded === 'true' && presetDismissal.custom.panel,
      `custom date selection closed before editing: ${JSON.stringify(presetDismissal)}`,
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
      [...document.querySelectorAll('#filter-panel-saved button')]
        .find((button) => button.textContent.includes('Save current filters')).click();
      await new Promise((resolve) => setTimeout(resolve, 50));
      const input = document.querySelector('input[aria-label="Saved view name"]');
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

    const savedView = await evaluate(`(async () => {
      document.querySelector('[data-filter-trigger="saved"]').click();
      await new Promise((resolve) => setTimeout(resolve, 50));
      const row = [...document.querySelectorAll('#filter-panel-saved .saved-view')]
        .find((button) => button.querySelector('strong')?.textContent === 'Two-by-two');
      const summary = row.querySelector('small').textContent;
      row.click();
      const trigger = document.querySelector('[data-filter-trigger="saved"]');
      for (let i = 0; i < 20 && trigger.textContent.trim() !== 'View: Two-by-two'; i++) {
        await new Promise((resolve) => setTimeout(resolve, 50));
      }
      return {
        summary,
        trigger: trigger.textContent.trim(),
        query: location.search,
        stored: JSON.parse(localStorage.getItem('lapdog.savedFilters.v1'))[0].query,
      };
    })()`)
    await sleep(700)
    assert((await evaluate('location.search')).includes('car='), 'saved filter did not reload')
    assert(savedView.summary.includes('2 tracks') && savedView.summary.includes('2 cars'),
      `saved view summary omitted criteria: ${savedView.summary}`)
    assert(savedView.trigger === 'View: Two-by-two',
      `saved view trigger did not identify selection: ${JSON.stringify(savedView)}`)

    const modifiedLabels = await evaluate(`(async () => {
      const ai = [...document.querySelectorAll('.control')]
        .find((control) => control.textContent.includes('Exclude AI')).querySelector('input');
      const trigger = document.querySelector('[data-filter-trigger="saved"]');
      ai.click();
      for (let i = 0; i < 20 && !trigger.textContent.includes('Modified'); i++) {
        await new Promise((resolve) => setTimeout(resolve, 50));
      }
      const modified = trigger.textContent.trim();
      ai.click();
      for (let i = 0; i < 20 && trigger.textContent.includes('Modified'); i++) {
        await new Promise((resolve) => setTimeout(resolve, 50));
      }
      return { modified, restored: trigger.textContent.trim() };
    })()`)
    assert(modifiedLabels.modified === 'View: Two-by-two • Modified',
      `changed saved view was not marked modified: ${JSON.stringify(modifiedLabels)}`)
    assert(modifiedLabels.restored === 'View: Two-by-two',
      `restored saved view did not regain its name: ${JSON.stringify(modifiedLabels)}`)

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
      [...document.querySelectorAll('#filter-panel-saved button')]
        .find((button) => button.textContent.includes('Manage saved views')).click();
      await new Promise((resolve) => setTimeout(resolve, 50));
      document.querySelector('button[aria-label="Delete Two-by-two"]').click();
    })()`)
    await sleep(300)
    assert(await evaluate(`JSON.parse(localStorage.getItem('lapdog.savedFilters.v1')).length`) === 0, 'filter set not deleted')

    console.log(`PASS: filters and debug bounds -> ${SHOT}, ${DEBUG_SHOT}`)
    ws.close()
  } finally {
    chrome.kill('SIGKILL')
  }
}

main().catch((error) => {
  console.error(`FAIL: ${error.message}`)
  process.exitCode = 1
})
