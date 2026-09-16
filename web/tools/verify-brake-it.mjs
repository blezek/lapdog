/*
 * Opens every Brake-It route in real Chrome, checks the shared navigation and
 * horizontal layout, and captures desktop and phone screenshots for inspection.
 *
 *   node tools/verify-brake-it.mjs [outdir]
 */

import { spawn } from 'node:child_process'
import { mkdirSync, rmSync, writeFileSync } from 'node:fs'

const OUT = process.argv[2] ?? '/tmp/lapdog-brake-it-shots'
const BASE = 'http://127.0.0.1:47047'
const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const PROFILE = '/tmp/chrome-lapdog-brake-it'
const PORT = 9335
const ROUTES = ['simulator', 'scenarios', 'devices', 'results']
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

async function launch() {
  rmSync(PROFILE, { recursive: true, force: true })
  const process = spawn(CHROME, [
    '--headless=new', '--disable-gpu', '--hide-scrollbars', '--no-first-run',
    '--no-default-browser-check', `--remote-debugging-port=${PORT}`,
    `--user-data-dir=${PROFILE}`, 'about:blank',
  ], { stdio: 'ignore' })
  for (let attempt = 0; attempt < 60; attempt += 1) {
    try {
      if ((await fetch(`http://127.0.0.1:${PORT}/json/version`)).ok) return process
    } catch {
      // Chrome is still starting.
    }
    await sleep(250)
  }
  throw new Error('Chrome DevTools endpoint never became available')
}

async function connect() {
  const targets = await (await fetch(`http://127.0.0.1:${PORT}/json`)).json()
  const page = targets.find((target) => target.type === 'page')
  if (!page) throw new Error('no page target')
  const socket = new WebSocket(page.webSocketDebuggerUrl)
  await new Promise((resolve, reject) => {
    socket.addEventListener('open', resolve, { once: true })
    socket.addEventListener('error', reject, { once: true })
  })
  let id = 0
  const pending = new Map()
  socket.addEventListener('message', (event) => {
    const message = JSON.parse(event.data)
    if (!message.id || !pending.has(message.id)) return
    const request = pending.get(message.id)
    pending.delete(message.id)
    message.error ? request.reject(new Error(message.error.message)) : request.resolve(message.result)
  })
  const send = (method, params = {}) => new Promise((resolve, reject) => {
    const requestID = ++id
    pending.set(requestID, { resolve, reject })
    socket.send(JSON.stringify({ id: requestID, method, params }))
  })
  const evaluate = async (expression) => {
    const result = await send('Runtime.evaluate', {
      expression, returnByValue: true, awaitPromise: true,
    })
    if (result.exceptionDetails) throw new Error(result.exceptionDetails.text)
    return result.result.value
  }
  return { send, evaluate, close: () => socket.close() }
}

async function inspectRoute(client, route, width, height) {
  await client.send('Emulation.setDeviceMetricsOverride', {
    width, height, deviceScaleFactor: 1, mobile: width < 600,
  })
  await client.send('Page.navigate', { url: `${BASE}/brake-it/${route}` })
  await sleep(1200)
  const state = await client.evaluate(`({
    title: document.title,
    text: document.body.innerText,
    overflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
    back: document.querySelector('.brake-back')?.getAttribute('href'),
    tabs: document.querySelectorAll('.brake-tabs a').length,
    imagesReady: [...document.querySelectorAll('.brake-it img')].every(img => img.complete && img.naturalWidth > 0),
    catalogFilters: document.querySelectorAll('.brake-catalog-filters select').length,
    error: document.querySelector('[role="alert"]')?.textContent ?? null
  })`)
  if (state.title !== 'Brake-It · LapDog') throw new Error(`${route}: wrong title ${state.title}`)
  if (!state.text.includes('Brake-It') || state.back !== '/dashboard' || state.tabs !== 4) {
    throw new Error(`${route}: shared navigation is incomplete`)
  }
  if (!state.imagesReady) throw new Error(`${route}: a Brake-It image did not load`)
  if (state.error) throw new Error(`${route}: interface error: ${state.error}`)
  if (route === 'scenarios' && state.catalogFilters !== 2) {
    throw new Error(`${route}: expected car and track catalog filters`)
  }
  if (state.overflow > 1) throw new Error(`${route}: ${state.overflow}px horizontal overflow at ${width}px`)
  return state
}

async function screenshot(client, name) {
  const result = await client.send('Page.captureScreenshot', {
    format: 'png', captureBeyondViewport: true,
  })
  const path = `${OUT}/${name}.png`
  writeFileSync(path, Buffer.from(result.data, 'base64'))
  return path
}

async function main() {
  mkdirSync(OUT, { recursive: true })
  const chrome = await launch()
  const client = await connect()
  try {
    await client.send('Page.enable')
    await client.send('Runtime.enable')
    for (const route of ROUTES) {
      await inspectRoute(client, route, 1440, 1000)
      console.log(`  PASS desktop /brake-it/${route}`)
      if (route === 'simulator' || route === 'scenarios') {
        console.log(`       ${await screenshot(client, `${route}-desktop`)}`)
      }
    }
    await inspectRoute(client, 'simulator', 390, 844)
    console.log('  PASS phone /brake-it/simulator')
    console.log(`       ${await screenshot(client, 'simulator-phone')}`)
    await inspectRoute(client, 'scenarios', 390, 844)
    console.log('  PASS phone /brake-it/scenarios')
    console.log(`       ${await screenshot(client, 'scenarios-phone')}`)

    await inspectRoute(client, 'simulator', 1440, 1000)
    const before = await client.evaluate(`fetch('/api/brake-it/results').then(r => r.json()).then(r => r.length)`)
    await client.evaluate(`[...document.querySelectorAll('button')].find(b => b.textContent === 'Audio on')?.click()`)
    await sleep(100)
    await client.evaluate(`[...document.querySelectorAll('button')].find(b => b.textContent === 'Start')?.click()`)
    await sleep(12000)
    const after = await client.evaluate(`fetch('/api/brake-it/results').then(r => r.json()).then(r => r.length)`)
    if (after !== before + 1) throw new Error(`completed run count changed from ${before} to ${after}`)
    await inspectRoute(client, 'results', 1440, 1000)
    if (!await client.evaluate(`document.body.innerText.includes('1 completed run') || document.body.innerText.includes('${after} completed runs')`)) {
      throw new Error('saved run did not survive results-page reload')
    }
    console.log('  PASS completed run persisted through the API and route reload')

    await client.evaluate(`document.querySelector('.brake-back').click()`)
    await sleep(1800)
    if (!await client.evaluate(`location.pathname === '/dashboard'`)) {
      throw new Error('Back to LapDog did not reach /dashboard')
    }
    const sidebarIconReady = await client.evaluate(`{
      const image = document.querySelector('a[href="/brake-it/simulator"] img.nav-module-icon');
      Boolean(image && image.complete && image.naturalWidth > 0);
    }`)
    if (!sidebarIconReady) throw new Error('LapDog Brake-It sidebar icon did not load')
    console.log(`       ${await screenshot(client, 'lapdog-sidebar-icon')}`)
    const lapdogLink = await client.evaluate(`{
      const link = [...document.querySelectorAll('a')].find(a => a.textContent.includes('Brake-It'));
      if (link) link.click();
      Boolean(link);
    }`)
    if (!lapdogLink) throw new Error('LapDog does not contain a Brake-It link')
    await sleep(1200)
    if (!await client.evaluate(`location.pathname === '/brake-it/simulator'`)) {
      throw new Error('LapDog Brake-It link did not return to the simulator')
    }
    console.log('  PASS navigation works in both directions')

    await client.evaluate(`document.documentElement.dataset.theme = 'dark'`)
    console.log(`       ${await screenshot(client, 'simulator-dark')}`)
  } finally {
    client.close()
    chrome.kill('SIGKILL')
  }
}

main().catch((error) => {
  console.error('  FAIL:', error.message)
  process.exitCode = 1
})
