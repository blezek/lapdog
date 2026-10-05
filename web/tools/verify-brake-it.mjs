/*
 * Opens every Brake-It route in real Chrome, checks the shared navigation and
 * horizontal layout, and captures desktop and phone screenshots for inspection.
 *
 *   node tools/verify-brake-it.mjs [outdir]
 */

import { spawn } from 'node:child_process'
import { mkdirSync, rmSync, writeFileSync } from 'node:fs'

const OUT = process.argv[2] ?? '/tmp/lapdog-brake-it-shots'
const BASE = process.env.LAPDOG_BASE ?? 'http://127.0.0.1:47047'
const CHROME = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const PROFILE = '/tmp/chrome-lapdog-brake-it'
const PORT = 9335
const VIRTUAL_GAMEPAD_ID = 'LapDog verifier virtual racing pedals'
const verifierScenarioIDs = new Set()
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
  if (!state.text.includes('Brake-It') || state.back !== '/dashboard' || state.tabs !== 3) {
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

async function verifyScenarioParity(client) {
  const catalog = await client.evaluate(`fetch('/api/brake-it/scenarios')
    .then(response => response.json())
    .then(rows => {
      const row = rows.find(item => item.sourceProvider === 'garage61');
      return row ? { id: row.id, name: row.name } : null;
    })`)
  if (catalog) {
    await client.evaluate(`{
      const scenario = ${JSON.stringify(catalog)};
      [...document.querySelectorAll('.brake-list > button')]
        .find(button => button.textContent.includes(scenario.name))
        ?.click();
    }`)
    await sleep(300)
  }
  const state = await client.evaluate(`({
    labels: document.querySelector('.brake-chart')?.textContent ?? '',
    markers: document.querySelectorAll('.brake-chart .marker').length,
    zones: document.querySelectorAll('.brake-chart .zone').length,
    handles: document.querySelectorAll('.brake-interactive-chart button').length,
    hasZoom: Boolean(document.querySelector('input[aria-label="Trace zoom"]')),
    hasTolerance: Boolean(document.querySelector('.brake-chart .tolerance')),
    metadata: {
      car: document.querySelector('input[aria-label="Scenario car"]')?.value ?? null,
      track: document.querySelector('input[aria-label="Scenario track"]')?.value ?? null,
      readonly: [...document.querySelectorAll('input[aria-label="Scenario car"], input[aria-label="Scenario track"]')]
        .every(input => input.disabled),
    },
    chartBeforeForm: (() => {
      const chart = document.querySelector('.brake-chart');
      const form = document.querySelector('.brake-form');
      return Boolean(chart && form && (chart.compareDocumentPosition(form) & Node.DOCUMENT_POSITION_FOLLOWING));
    })(),
    sourceLinks: [...document.querySelectorAll('.brake-source-lap a')].map(link => link.href),
  })`)
  for (const label of ['0%', '100%', 'Brake', 'Trail', 'Accel']) {
    if (!state.labels.includes(label)) throw new Error(`scenarios: trace is missing ${label}`)
  }
  if (state.markers < 4 || state.zones < 6 || state.handles < 6 || !state.hasZoom || !state.hasTolerance || !state.chartBeforeForm) {
    throw new Error(`scenarios: trace parity failed: ${JSON.stringify(state)}`)
  }
  if (catalog && (!state.metadata.car || !state.metadata.track || !state.metadata.readonly)) {
    throw new Error(`scenarios: catalog car and track metadata is missing or editable: ${JSON.stringify(state.metadata)}`)
  }
  if (catalog && (state.sourceLinks.length === 0 || state.sourceLinks.some(link => !link.startsWith('https://garage61.net/app/')))) {
    throw new Error(`scenarios: Garage61 citations are missing or invalid: ${JSON.stringify(state.sourceLinks)}`)
  }
  if (catalog) {
    const scenarioIDsBeforeDuplicate = await client.evaluate(`fetch('/api/brake-it/scenarios')
      .then(response => response.json())
      .then(rows => rows.map(row => row.id))`)
    await client.evaluate(`[...document.querySelectorAll('button')].find(button => button.textContent === 'Duplicate')?.click()`)
    await sleep(500)
    const duplicatedID = await client.evaluate(`fetch('/api/brake-it/scenarios')
      .then(response => response.json())
      .then(rows => rows.find(row => !${JSON.stringify(scenarioIDsBeforeDuplicate)}.includes(row.id))?.id ?? null)`)
    if (!duplicatedID) throw new Error('scenarios: duplicate was not persisted')
    verifierScenarioIDs.add(duplicatedID)
    const editable = await client.evaluate(`({
      enabledHandles: [...document.querySelectorAll('.brake-interactive-chart button')].filter(button => !button.disabled).length,
      text: document.querySelector('.brake-panel-head')?.textContent ?? '',
    })`)
    if (editable.enabledHandles < 6 || !editable.text.includes('Custom scenario')) {
      throw new Error(`scenarios: duplicate did not enable trace editing: ${JSON.stringify(editable)}`)
    }
    const metadata = {
      car: `Verifier car ${Date.now()}`,
      track: `Verifier track ${Date.now()}`,
    }
    await client.evaluate(`{
      const input = document.querySelector('input[aria-label="Scenario car"]');
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
      setter.call(input, ${JSON.stringify(metadata.car)});
      input.dispatchEvent(new Event('input', { bubbles: true }));
    }`)
    await sleep(150)
    await client.evaluate(`{
      const input = document.querySelector('input[aria-label="Scenario track"]');
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
      setter.call(input, ${JSON.stringify(metadata.track)});
      input.dispatchEvent(new Event('input', { bubbles: true }));
    }`)
    await sleep(150)
    const metadataEditor = await client.evaluate(`({
      car: document.querySelector('input[aria-label="Scenario car"]')?.value ?? null,
      track: document.querySelector('input[aria-label="Scenario track"]')?.value ?? null,
      enabled: [...document.querySelectorAll('input[aria-label="Scenario car"], input[aria-label="Scenario track"]')]
        .every(input => !input.disabled),
    })`)
    if (!metadataEditor.enabled || metadataEditor.car !== metadata.car || metadataEditor.track !== metadata.track) {
      throw new Error(`scenarios: custom car and track are not editable: ${JSON.stringify(metadataEditor)}`)
    }
    await client.evaluate(`[...document.querySelectorAll('button')].find(button => button.textContent === 'Save')?.click()`)
    await sleep(500)
    const persistedMetadata = await client.evaluate(`fetch('/api/brake-it/scenarios')
      .then(response => response.json())
      .then(rows => rows.some(row => row.carName === ${JSON.stringify(metadata.car)} && row.trackName === ${JSON.stringify(metadata.track)}))`)
    if (!persistedMetadata) throw new Error('scenarios: edited car and track did not persist')
    const drag = await client.evaluate(`(() => {
      const handle = document.querySelector('.handle-brake-rise');
      const field = [...document.querySelectorAll('.brake-range')]
        .find(label => label.textContent.includes('Target brake'));
      if (!handle || !field) return null;
      const rect = handle.getBoundingClientRect();
      return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2, before: field.textContent };
    })()`)
    if (!drag) throw new Error('scenarios: brake-rise handle is not measurable')
    await client.send('Input.dispatchMouseEvent', { type: 'mousePressed', x: drag.x, y: drag.y, button: 'left', buttons: 1, clickCount: 1 })
    await client.send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: drag.x, y: drag.y - 35, button: 'left', buttons: 1 })
    await client.send('Input.dispatchMouseEvent', { type: 'mouseReleased', x: drag.x, y: drag.y - 35, button: 'left', buttons: 0, clickCount: 1 })
    await sleep(200)
    const afterDrag = await client.evaluate(`[...document.querySelectorAll('.brake-range')]
      .find(label => label.textContent.includes('Target brake'))?.textContent ?? ''`)
    if (afterDrag === drag.before) {
      throw new Error(`scenarios: dragging the brake-rise handle did not change the target (${afterDrag})`)
    }
    await client.evaluate(`{
      const scenario = ${JSON.stringify(catalog)};
      [...document.querySelectorAll('.brake-list > button')]
        .find(button => button.textContent.includes(scenario.name) && !button.textContent.includes('Copy'))
        ?.click();
    }`)
    await sleep(300)
  }
  console.log(`  PASS scenario trace controls and ${state.sourceLinks.length} Garage61 citations`)
}

async function configureBrakingOnlyPractice(client) {
  const controls = await client.evaluate(`({
    audio: [...document.querySelectorAll('button')].some(button => button.textContent === 'Audio on'),
    target: [...document.querySelectorAll('button')].some(button => button.textContent === 'Target trace on'),
    visual: [...document.querySelectorAll('button')].some(button => button.textContent === 'Visual cues off'),
    acceleration: [...document.querySelectorAll('button')].some(button => button.textContent === 'Acceleration on'),
  })`)
  if (Object.values(controls).some(value => !value)) {
    throw new Error(`simulator: practice controls are incomplete: ${JSON.stringify(controls)}`)
  }
  await client.evaluate(`{
    [...document.querySelectorAll('button')].find(button => button.textContent === 'Target trace on').click();
    [...document.querySelectorAll('button')].find(button => button.textContent === 'Visual cues off').click();
    [...document.querySelectorAll('button')].find(button => button.textContent === 'Acceleration on').click();
  }`)
  await sleep(200)
  const state = await client.evaluate(`({
    targetTraces: document.querySelectorAll('.target-accelerator, .target-brake').length,
    targetTolerance: document.querySelectorAll('.brake-chart .tolerance').length,
    cue: document.querySelector('.brake-visual-cue')?.getAttribute('aria-label'),
    accelerationLabel: [...document.querySelectorAll('button')].find(button => button.textContent === 'Braking only')?.textContent,
    chartLabels: document.querySelector('.brake-chart')?.textContent ?? '',
  })`)
  if (state.targetTraces !== 0 || state.targetTolerance !== 0 || state.cue !== 'Approach: Get ready' || state.accelerationLabel !== 'Braking only' || state.chartLabels.includes('Accel')) {
    throw new Error(`simulator: braking-only visual mode is wrong: ${JSON.stringify(state)}`)
  }
  console.log('  PASS target-free visual cues and braking-only practice controls')
}

async function verifyLinkedScenarioSelectors(client) {
  const choices = await client.evaluate(`fetch('/api/brake-it/scenarios')
    .then(response => response.json())
    .then(rows => ({
      catalog: rows.find(item => item.carName && item.trackName) ?? null,
      general: rows.find(item => item.carName === null && item.trackName === null) ?? null,
    }))`)
  const selectorCount = await client.evaluate(`document.querySelectorAll(
    'select[aria-label="Practice car"], select[aria-label="Practice track"], select[aria-label="Practice scenario"]'
  ).length`)
  if (selectorCount !== 3) throw new Error(`simulator: expected three linked selectors, got ${selectorCount}`)
  const placeholders = await client.evaluate(`({
    car: (() => {
      const option = document.querySelector('select[aria-label="Practice car"] option[value="__lapdog_unassigned__"]');
      return option ? { text: option.textContent, disabled: option.disabled } : null;
    })(),
    track: (() => {
      const option = document.querySelector('select[aria-label="Practice track"] option[value="__lapdog_unassigned__"]');
      return option ? { text: option.textContent, disabled: option.disabled } : null;
    })(),
  })`)
  if (placeholders.car?.text !== 'Choose Car' || !placeholders.car.disabled || placeholders.track?.text !== 'Choose Track' || !placeholders.track.disabled) {
    throw new Error(`simulator: car and track placeholders are wrong: ${JSON.stringify(placeholders)}`)
  }
  if (!choices.catalog || !choices.general) {
    console.log('  PASS linked scenario selectors present; combo exercise skipped without both catalog and general scenarios')
    return
  }
  const choose = async (label, value) => {
    await client.evaluate(`{
      const select = document.querySelector('select[aria-label=${JSON.stringify(label)}]');
      select.value = ${JSON.stringify(value)};
      select.dispatchEvent(new Event('change', { bubbles: true }));
    }`)
    await sleep(300)
  }
  await choose('Practice car', choices.catalog.carName)
  await choose('Practice track', choices.catalog.trackName)
  const validCatalog = await client.evaluate(`({
    car: document.querySelector('select[aria-label="Practice car"]')?.value,
    track: document.querySelector('select[aria-label="Practice track"]')?.value,
    scenarioDisabled: document.querySelector('select[aria-label="Practice scenario"]')?.disabled,
    startDisabled: [...document.querySelectorAll('button')].find(button => button.textContent === 'Start')?.disabled,
  })`)
  if (validCatalog.car !== choices.catalog.carName || validCatalog.track !== choices.catalog.trackName || validCatalog.scenarioDisabled || validCatalog.startDisabled) {
    throw new Error(`simulator: valid catalog pair did not settle: ${JSON.stringify(validCatalog)}`)
  }

  // Make Car the active selector so its dependent Track list is constrained;
  // choosing the current value still exercises the same change handler.
  await choose('Practice car', choices.catalog.carName)
  await choose('Practice car', '__lapdog_unassigned__')
  const invalidCar = await client.evaluate(`({
    track: document.querySelector('select[aria-label="Practice track"]')?.value,
    trackText: document.querySelector('select[aria-label="Practice track"] option:checked')?.textContent,
    scenarioText: document.querySelector('select[aria-label="Practice scenario"] option:checked')?.textContent,
    startDisabled: [...document.querySelectorAll('button')].find(button => button.textContent === 'Start')?.disabled,
  })`)
  if (invalidCar.track !== choices.catalog.trackName || !invalidCar.trackText.includes('unavailable for Choose Car') || invalidCar.scenarioText !== 'No scenarios for this car and track' || !invalidCar.startDisabled) {
    throw new Error(`simulator: invalid car did not preserve and mark the track: ${JSON.stringify(invalidCar)}`)
  }

  await choose('Practice track', '__lapdog_unassigned__')
  await choose('Practice track', choices.catalog.trackName)
  const invalidTrack = await client.evaluate(`({
    car: document.querySelector('select[aria-label="Practice car"]')?.value,
    carText: document.querySelector('select[aria-label="Practice car"] option:checked')?.textContent,
    scenarioText: document.querySelector('select[aria-label="Practice scenario"] option:checked')?.textContent,
    startDisabled: [...document.querySelectorAll('button')].find(button => button.textContent === 'Start')?.disabled,
  })`)
  if (invalidTrack.car !== '__lapdog_unassigned__' || invalidTrack.carText !== 'Choose Car' || invalidTrack.scenarioText !== 'No scenarios for this car and track' || !invalidTrack.startDisabled) {
    throw new Error(`simulator: invalid track did not preserve and mark the car: ${JSON.stringify(invalidTrack)}`)
  }

  await choose('Practice car', choices.catalog.carName)
  const restored = await client.evaluate(`({
    track: document.querySelector('select[aria-label="Practice track"]')?.value,
    selectedScenario: document.querySelector('select[aria-label="Practice scenario"]')?.value,
    startDisabled: [...document.querySelectorAll('button')].find(button => button.textContent === 'Start')?.disabled,
  })`)
  if (restored.track !== choices.catalog.trackName || !restored.selectedScenario || restored.startDisabled) {
    throw new Error(`simulator: restoring a valid pair changed the track: ${JSON.stringify(restored)}`)
  }
  console.log('  PASS linked car and track selectors preserve valid pairs and expose invalid pairs')
}

async function cleanupVerifierScenarios(client) {
  for (const id of verifierScenarioIDs) {
    const status = await client.evaluate(`fetch('/api/brake-it/scenarios/${encodeURIComponent(id)}', {
      method: 'DELETE',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: '{}',
    }).then(response => response.status)`)
    if (status !== 204) throw new Error(`cleanup: deleting verifier scenario ${id} returned ${status}`)
  }
  verifierScenarioIDs.clear()
}

async function cleanupVerifierDevice(client) {
  await client.evaluate(`fetch('/api/brake-it/devices')
    .then(response => response.json())
    .then(rows => Promise.all(rows
      .filter(device => device.gamepadId === ${JSON.stringify(VIRTUAL_GAMEPAD_ID)})
      .map(device => fetch('/api/brake-it/devices/' + encodeURIComponent(device.id), {
        method: 'DELETE',
        headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
        body: '{}',
      }))))`)
}

async function verifyHIDPedalCalibration(client) {
  await client.send('Page.navigate', { url: `${BASE}/brake-it/devices` })
  await sleep(1400)
  const click = async (label) => client.evaluate(`{
    const button = [...document.querySelectorAll('button')].find(item => item.textContent === ${JSON.stringify(label)});
    if (!button || button.disabled) throw new Error(${JSON.stringify(label)} + ' is not available');
    button.click();
  }`)
  await click('Detect pedals')
  await click('Capture released pedals')
  await client.evaluate(`globalThis.__lapdogVerifierAxes[0] = -1`)
  await sleep(500)
  await click('Use this accelerator')
  await client.evaluate(`globalThis.__lapdogVerifierAxes[0] = 1`)
  await sleep(300)
  await click('Detect brake')
  await client.evaluate(`globalThis.__lapdogVerifierAxes[1] = -1`)
  await sleep(500)
  await click('Save pedal mapping')
  await sleep(700)
  const configured = await client.evaluate(`Promise.all([
    fetch('/api/brake-it/devices').then(response => response.json()),
    Promise.resolve(document.body.innerText),
  ]).then(([devices, text]) => ({
    count: devices.filter(device => device.gamepadId === ${JSON.stringify(VIRTUAL_GAMEPAD_ID)}).length,
    configured: text.includes('Pedals configured'),
    reconfigure: text.includes('Reconfigure'),
    remove: text.includes('Remove'),
  }))`)
  if (configured.count !== 1 || !configured.configured || !configured.reconfigure || !configured.remove) {
    throw new Error(`devices: calibration did not persist: ${JSON.stringify(configured)}`)
  }
  console.log('  PASS HID axes detected, calibrated, and persisted')
  console.log(`       ${await screenshot(client, 'devices-configured')}`)
  await click('Remove')
  await sleep(500)
  const remaining = await client.evaluate(`fetch('/api/brake-it/devices')
    .then(response => response.json())
    .then(rows => rows.filter(device => device.gamepadId === ${JSON.stringify(VIRTUAL_GAMEPAD_ID)}).length)`)
  if (remaining !== 0) throw new Error('devices: removed calibration remains in the database')
  console.log('  PASS HID pedal mapping can be removed and detected again')
}

async function verifyBrakeItIsUndiscoverable(client) {
  await client.send('Page.navigate', { url: `${BASE}/settings` })
  await sleep(1600)
  const state = await client.evaluate(`(async () => ({
    setting: document.body.textContent.includes('Enable Brake-It link'),
    link: Boolean(document.querySelector('a[href="/brake-it/simulator"]')),
    configField: await fetch('/api/settings').then(response => response.json()).then(config => Object.hasOwn(config, 'brakeItEnabled')),
  }))()`)
  if (state.setting || state.link || state.configField) {
    throw new Error(`settings: experimental Brake-It is discoverable: ${JSON.stringify(state)}`)
  }
  console.log('  PASS LapDog does not expose the experimental Brake-It module')
  await inspectRoute(client, 'simulator', 1440, 1000)
  console.log('  PASS direct Brake-It URL remains available')
}

async function main() {
  mkdirSync(OUT, { recursive: true })
  const chrome = await launch()
  const client = await connect()
  try {
    await client.send('Page.enable')
    await client.send('Runtime.enable')
    await client.send('Page.addScriptToEvaluateOnNewDocument', { source: `
      globalThis.__lapdogVerifierAxes = [1, 1, 0];
      Object.defineProperty(navigator, 'getGamepads', {
        configurable: true,
        value: () => [{
          id: ${JSON.stringify(VIRTUAL_GAMEPAD_ID)}, index: 0, connected: true,
          mapping: '', axes: [...globalThis.__lapdogVerifierAxes], buttons: [],
          timestamp: performance.now(), vibrationActuator: null,
        }],
      });
    ` })
    await verifyBrakeItIsUndiscoverable(client)
    await cleanupVerifierDevice(client)
    for (const route of ROUTES) {
      await inspectRoute(client, route, 1440, 1000)
      console.log(`  PASS desktop /brake-it/${route}`)
      if (route === 'devices') {
        const deviceCopy = await client.evaluate(`document.body.innerText`)
        if (!deviceCopy.includes('HID pedal support') || deviceCopy.includes('Web Serial')) {
          throw new Error('devices: HID calibration replaced by unsupported serial setup')
        }
        console.log(`       ${await screenshot(client, 'devices-desktop')}`)
      }
      if (route === 'simulator' || route === 'scenarios') {
        if (route === 'scenarios') await verifyScenarioParity(client)
        console.log(`       ${await screenshot(client, `${route}-desktop`)}`)
      }
    }
    await verifyHIDPedalCalibration(client)
    await inspectRoute(client, 'simulator', 390, 844)
    console.log('  PASS phone /brake-it/simulator')
    console.log(`       ${await screenshot(client, 'simulator-phone')}`)
    await inspectRoute(client, 'scenarios', 390, 844)
    console.log('  PASS phone /brake-it/scenarios')
    console.log(`       ${await screenshot(client, 'scenarios-phone')}`)

    await inspectRoute(client, 'simulator', 1440, 1000)
    await verifyLinkedScenarioSelectors(client)
    await configureBrakingOnlyPractice(client)
    console.log(`       ${await screenshot(client, 'simulator-braking-only')}`)
    const before = await client.evaluate(`fetch('/api/brake-it/results').then(r => r.json()).then(r => r.length)`)
    await client.evaluate(`[...document.querySelectorAll('button')].find(b => b.textContent === 'Audio on')?.click()`)
    await sleep(100)
    await client.evaluate(`[...document.querySelectorAll('button')].find(b => b.textContent === 'Start')?.click()`)
    let after = before
    const deadline = Date.now() + 30000
    while (after === before && Date.now() < deadline) {
      await sleep(500)
      after = await client.evaluate(`fetch('/api/brake-it/results').then(r => r.json()).then(r => r.length)`)
    }
    if (after !== before + 1) throw new Error(`completed run count changed from ${before} to ${after}`)
    const completed = await client.evaluate(`fetch('/api/brake-it/results').then(r => r.json()).then(rows => ({
      accelerationIncluded: rows[0]?.accelerationIncluded,
      acceleratorRampErrorPercent: rows[0]?.metrics?.acceleratorRampErrorPercent,
      liveTraces: document.querySelectorAll('.live-accelerator, .live-brake').length,
      targetTraces: document.querySelectorAll('.target-accelerator, .target-brake').length,
    }))`)
    if (completed.accelerationIncluded !== false || completed.acceleratorRampErrorPercent !== null || completed.liveTraces !== 2 || completed.targetTraces !== 0) {
      throw new Error(`braking-only run facts are wrong: ${JSON.stringify(completed)}`)
    }
    await inspectRoute(client, 'results', 1440, 1000)
    if (!await client.evaluate(`document.body.innerText.includes('1 completed run') || document.body.innerText.includes('${after} completed runs')`)) {
      throw new Error('saved run did not survive results-page reload')
    }
    if (!await client.evaluate(`document.body.innerText.includes('Braking only')`)) {
      throw new Error('results page does not identify the braking-only run')
    }
    console.log('  PASS completed run persisted through the API and route reload')

    await client.evaluate(`document.querySelector('.brake-back').click()`)
    await sleep(1800)
    if (!await client.evaluate(`location.pathname === '/dashboard'`)) {
      throw new Error('Back to LapDog did not reach /dashboard')
    }
    const lapdogLink = await client.evaluate(`{
      const link = [...document.querySelectorAll('a')].find(a => a.textContent.includes('Brake-It'));
      Boolean(link);
    }`)
    if (lapdogLink) throw new Error('LapDog unexpectedly contains a Brake-It link')
    console.log('  PASS Brake-It returns to LapDog without advertising a return link')

    await inspectRoute(client, 'simulator', 1440, 1000)
    await client.evaluate(`document.documentElement.dataset.theme = 'dark'`)
    console.log(`       ${await screenshot(client, 'simulator-dark')}`)
  } finally {
    try {
      await cleanupVerifierDevice(client)
      await cleanupVerifierScenarios(client)
    } finally {
      client.close()
      chrome.kill('SIGKILL')
    }
  }
}

main().catch((error) => {
  console.error('  FAIL:', error.message)
  process.exitCode = 1
})
