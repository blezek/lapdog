import { useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useLocation, useNavigate } from 'react-router-dom'
import { brakeApi, type GarageCatalog, type GarageJob, type GarageQueueItem, type GarageRefresh, type MyDrivenCombination } from './api'
import type { Scenario } from './types'

export function Garage61({ scenarios, onReady }: { scenarios: Scenario[]; onReady: (scenarioId?: string) => Promise<void> }) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const location = useLocation()
  const [catalog, setCatalog] = useState<GarageCatalog | null>(null)
  const [refresh, setRefresh] = useState<GarageRefresh | null>(null)
  const [queue, setQueue] = useState<GarageQueueItem[] | null>(null)
  const [myCombos, setMyCombos] = useState<MyDrivenCombination[]>([])
  const [search, setSearch] = useState('')
  const [job, setJob] = useState<GarageJob>({ runId: 0, state: 'idle', message: '', carId: 0, trackId: 0, count: 0 })
  const [car, setCar] = useState('')
  const [track, setTrack] = useState('')
  const [carSearch, setCarSearch] = useState('')
  const [trackSearch, setTrackSearch] = useState('')
  const [selectedReady, setSelectedReady] = useState<Set<string>>(() => new Set())
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const ready = useRef(onReady)
  const handledRunID = useRef(0)
  ready.current = onReady

  useEffect(() => {
    let disposed = false
    let timer: ReturnType<typeof setTimeout>
    const poll = async () => {
      try {
        const [next, queued] = await Promise.all([brakeApi.garageJob(), brakeApi.garageQueue()])
        if (disposed) return
        setJob(next)
        setQueue(queued)
        if (next.runId > 0 && ['complete', 'partial', 'review', 'cancelled'].includes(next.state) && next.runId !== handledRunID.current) {
          handledRunID.current = next.runId
          await ready.current()
          const status = await brakeApi.garageRefresh()
          if (!disposed) setRefresh(status)
        }
      } catch (caught) { if (!disposed) setError(errorMessage(caught)) }
      if (!disposed) timer = setTimeout(() => void poll(), 1500)
    }
    void brakeApi.garageCatalog().then((value) => { if (!disposed) setCatalog(value) }).catch((caught) => { if (!disposed) setError(errorMessage(caught)) })
    void brakeApi.garageRefresh().then((value) => { if (!disposed) setRefresh(value) }).catch((caught) => { if (!disposed) setError(errorMessage(caught)) })
    void brakeApi.garageMyCombinations().then((value) => { if (!disposed) setMyCombos(value) }).catch((caught) => { if (!disposed) setError(errorMessage(caught)) })
    void poll()
    return () => { disposed = true; clearTimeout(timer) }
  }, [])

  useEffect(() => {
    if (!catalog?.configured) return
    const query = new URLSearchParams(location.search)
    const carID = query.get('carPlatformId')
    const trackID = query.get('trackPlatformId')
    if (!carID || !trackID) return
    const match = matchPlatformCombination(catalog, carID, trackID)
    if (match) { setCar(String(match.car.id)); setTrack(String(match.track.id)); setError(null) }
    else setError('That recorded combination has no unique match in Garage61. Choose its car and layout manually.')
  }, [catalog, location.search])

  useEffect(() => {
    if (!job.all && job.carId) setCar(String(job.carId))
    if (!job.all && job.trackId) setTrack(String(job.trackId))
  }, [job.all, job.carId, job.trackId])

  useEffect(() => {
    if (!refresh?.snoozeUntil) return
    const remaining = Date.parse(refresh.snoozeUntil) - Date.now()
    if (remaining <= 0) return
    const timer = window.setTimeout(() => {
      void brakeApi.garageRefresh().then((status) => {
        setRefresh(status)
        queryClient.setQueryData(['brake-it', 'garage61', 'refresh'], status)
      }).catch((caught) => setError(errorMessage(caught)))
    }, remaining + 100)
    return () => window.clearTimeout(timer)
  }, [refresh?.snoozeUntil, queryClient])

  const running = job.state === 'running'
  const carEntity = catalog?.cars.find((item) => String(item.id) === car)
  const trackEntity = catalog?.tracks.find((item) => String(item.id) === track)
  const visibleCars = catalog?.cars.filter((item) => item.name.toLocaleLowerCase().includes(carSearch.trim().toLocaleLowerCase()) || String(item.id) === car) ?? []
  const visibleTracks = catalog?.tracks.filter((item) => label(item).toLocaleLowerCase().includes(trackSearch.trim().toLocaleLowerCase()) || String(item.id) === track) ?? []
  const matching = scenarios.filter((item) => item.sourceProvider === 'garage61' && item.carName === carEntity?.name && item.trackName === (trackEntity ? label(trackEntity) : undefined))
  const combinations = [...new Map(scenarios.filter((item) => item.sourceProvider === 'garage61').map((item) => [combinationKey(item), item])).values()]
  const selectedCombinations = combinations.filter((item) => selectedReady.has(item.id) && garageIDs(item.id))
  const snoozed = Boolean(refresh?.snoozeUntil && Date.parse(refresh.snoozeUntil) > Date.now())
  const deletionBlocked = (item: Scenario) => {
    const ids = garageIDs(item.id)
    return busy || !ids || (running && (job.all || job.carId === ids.car && job.trackId === ids.track))
  }
  const deletableCombinations = combinations.filter((item) => !deletionBlocked(item))
  const selectedDeletionBlocked = selectedCombinations.some(deletionBlocked)
  const visibleMyCombos = myCombos.filter((item) => `${item.carName} ${item.trackName} ${item.trackConfig}`.toLocaleLowerCase().includes(search.toLocaleLowerCase()))

  const process = async () => {
    setBusy(true); setError(null)
    try { setQueue(await brakeApi.enqueueGarage(Number(car), Number(track))) }
    catch (caught) { setError(errorMessage(caught)) }
    finally { setBusy(false) }
  }
  const refreshAll = async () => {
    setBusy(true); setError(null)
    try { setQueue(await brakeApi.enqueueRefreshAllGarage()) }
    catch (caught) { setError(errorMessage(caught)) }
    finally { setBusy(false) }
  }
  const snooze = async () => {
    const until = new Date(Date.now() + 7 * 86400000)
    setBusy(true); setError(null)
    try {
      const status = await brakeApi.snoozeGarage(until.toISOString())
      setRefresh(status)
      queryClient.setQueryData(['brake-it', 'garage61', 'refresh'], status)
    }
    catch (caught) { setError(errorMessage(caught)) }
    finally { setBusy(false) }
  }
  const chooseMine = (item: MyDrivenCombination) => {
    if (!catalog) return
    const match = matchPlatformCombination(catalog, String(item.carPlatformId), String(item.trackPlatformId))
    if (match) { setCar(String(match.car.id)); setTrack(String(match.track.id)); setError(null) }
    else setError(`${item.carName} / ${item.trackName} has no unique Garage61 match. Use the full catalog picker.`)
  }
  const practice = async (id: string) => {
    try { await ready.current(id); navigate('/brake-it/simulator') }
    catch (caught) { setError(errorMessage(caught)) }
  }
  const cancel = async () => {
    setBusy(true)
    try { await brakeApi.cancelGarage() } catch (caught) { setError(errorMessage(caught)) }
    finally { setBusy(false) }
  }
  const retry = async (id: number) => {
    setBusy(true); setError(null)
    try { setQueue(await brakeApi.retryGarageQueue(id)) }
    catch (caught) { setError(errorMessage(caught)) }
    finally { setBusy(false) }
  }
  const deleteDownloaded = async (item: Scenario) => {
    const ids = garageIDs(item.id)
    if (!ids || !window.confirm(`Delete downloaded scenarios for ${item.carName} / ${item.trackName}? Saved lap citations will be removed; past practice results remain.`)) return
    setBusy(true); setError(null)
    try {
      await brakeApi.deleteGarageCombination(ids.car, ids.track)
      setSelectedReady((current) => { const next = new Set(current); next.delete(item.id); return next })
      await ready.current()
      setRefresh(await brakeApi.garageRefresh())
      setQueue(await brakeApi.garageQueue())
    } catch (caught) { setError(errorMessage(caught)) }
    finally { setBusy(false) }
  }
  const toggleReady = (id: string) => {
    setSelectedReady((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }
  const toggleAllReady = () => {
    const allSelected = deletableCombinations.length > 0 && deletableCombinations.every((item) => selectedReady.has(item.id))
    setSelectedReady(allSelected ? new Set() : new Set(deletableCombinations.map((item) => item.id)))
  }
  const deleteSelected = async () => {
    const pairs = selectedCombinations.map((item) => garageIDs(item.id)).filter((ids): ids is { car: number; track: number } => ids !== null)
    if (pairs.length === 0 || selectedDeletionBlocked || !window.confirm(`Delete downloaded scenarios for ${pairs.length} selected combination${pairs.length === 1 ? '' : 's'}? Saved lap citations will be removed; past practice results remain.`)) return
    setBusy(true); setError(null)
    try {
      await brakeApi.deleteGarageCombinations(pairs.map((ids) => ({ carId: ids.car, trackId: ids.track })))
      setSelectedReady(new Set())
      await ready.current()
      setRefresh(await brakeApi.garageRefresh())
      setQueue(await brakeApi.garageQueue())
    } catch (caught) { setError(errorMessage(caught)) }
    finally { setBusy(false) }
  }

  return <main className="brake-garage">
    <section className="brake-panel">
      <div className="brake-panel-head"><div><h1>Garage61 scenarios</h1><span>Prepare braking practice from lap telemetry</span></div></div>
      <p>Choose any iRacing car and track layout. Brake-It finds up to 12 viewable laps and prepares the braking zones supported by at least three laps.</p>
      {error && <p className="brake-error" role="alert">{error}</p>}
      {!catalog && !error && <p role="status">Loading Garage61 cars and tracks…</p>}
      {catalog && !catalog.configured && <p role="status">Set <code>GARAGE61_TOKEN</code> in the environment used to start LapDog, then restart it. Garage61 sign-in is not available yet. Your prepared scenarios remain available below.</p>}
      {catalog?.configured && <>
        <div className="brake-garage-selectors">
          <div className="brake-garage-picker"><label>Search cars<input type="search" aria-label="Search Garage61 cars" value={carSearch} onChange={(event) => setCarSearch(event.target.value)} placeholder="Filter cars" /></label><label>Car<select aria-label="Garage61 car" value={car} disabled={busy} onChange={(event) => setCar(event.target.value)}><option value="">Select a car</option>{visibleCars.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label></div>
          <div className="brake-garage-picker"><label>Search track layouts<input type="search" aria-label="Search Garage61 track layouts" value={trackSearch} onChange={(event) => setTrackSearch(event.target.value)} placeholder="Filter track layouts" /></label><label>Track layout<select aria-label="Garage61 track layout" value={track} disabled={busy} onChange={(event) => setTrack(event.target.value)}><option value="">Select a track layout</option>{visibleTracks.map((item) => <option key={item.id} value={item.id}>{label(item)}</option>)}</select></label></div>
        </div>
        <div className="brake-actions"><button className="primary" type="button" disabled={!car || !track || busy} onClick={() => void process()}>{matching.length ? 'Queue reprocessing' : 'Queue scenarios'}</button>{matching.length > 0 && <span>{matching.length} prepared zone{matching.length === 1 ? '' : 's'}. Reprocessing replaces this combination after it succeeds.</span>}</div>
      </>}
      {job.state !== 'idle' && <div className="brake-garage-progress" role="status"><strong>{job.message}</strong>{running && <><progress aria-label="Processing Garage61 telemetry" value={job.all ? job.index : undefined} max={job.all ? job.total : undefined} /><button type="button" disabled={busy} onClick={() => void cancel()}>Cancel processing</button></>}{!running && job.count > 0 && <span>{job.count} braking zone{job.count === 1 ? '' : 's'} saved locally.</span>}{(job.errors ?? []).length > 0 && <ul>{job.errors?.map((message) => <li key={message}>{message}</li>)}</ul>}</div>}
      <p className="brake-garage-note">Availability depends on the laps your Garage61 token can access. Processing may take several minutes when Garage61 limits requests. Prepared scenarios work offline.</p>
    </section>
    {myCombos.length > 0 && <details className="brake-panel brake-garage-mine"><summary>My raced and practiced combinations ({myCombos.length})</summary><p>Pick one to fill the selectors above, then click Queue scenarios. Nothing is queued from your driving history automatically.</p><label>Find a combination<input aria-label="Find recorded combination" value={search} onChange={(event) => setSearch(event.target.value)} /></label><div className="brake-list">{visibleMyCombos.map((item) => <button key={`${item.carPlatformId}/${item.trackPlatformId}`} type="button" disabled={!catalog?.configured || busy} onClick={() => chooseMine(item)}><strong>{item.carName}</strong><span>{item.trackName}{item.trackConfig ? ` · ${item.trackConfig}` : ''} · {item.drivingHours.toFixed(1)} h</span></button>)}{visibleMyCombos.length === 0 && <p>No recorded combinations match.</p>}</div></details>}
    <section className="brake-panel">
      <div className="brake-panel-head"><div><h2>Processing queue</h2><span>Requested combinations start processing when Garage61 is connected</span></div></div>
      {!queue ? <p>Loading queue…</p> : queue.length === 0 ? <p>No combinations have been queued yet.</p> : <>
        <p>{queue.filter((item) => item.state === 'queued').length} waiting · {queue.filter((item) => item.state === 'running').length} processing · {queue.filter((item) => item.state === 'done').length} prepared</p>
        <div className="brake-list">{queue.map((item) => <div className="brake-garage-combo brake-queue-item" key={item.id}><div><strong>{item.carName || `Car ${item.carPlatformId || item.garageCarId}`}</strong><span>{item.trackName || `Track ${item.trackPlatformId || item.garageTrackId}`} · {queueStateLabel(item.state)}{item.error ? ` · ${item.error}` : ''}</span></div>{['failed', 'unmatched', 'review', 'cancelled'].includes(item.state) && <button type="button" disabled={busy} onClick={() => void retry(item.id)}>Retry</button>}</div>)}</div>
      </>}
    </section>
    {refresh && !snoozed && refresh.combinations.length > 0 && <section className="brake-panel">
      <div className="brake-panel-head"><div><h2>Weekly refresh</h2><span>Review saved combinations every seven days</span></div></div>
      <>
        {refresh.reminderDue ? <p role="status"><strong>{refresh.dueCount} combination{refresh.dueCount === 1 ? '' : 's'} due for refresh.</strong> Rebuild targets and citations from currently accessible laps.</p> : <p role="status">{refresh.dueCount > 0 ? `Reminder delayed until ${dateLabel(refresh.snoozeUntil)}.` : 'Saved combinations are within the seven-day review window.'}</p>}
        <div className="brake-actions"><button type="button" disabled={busy} onClick={() => void snooze()}>Remind me in one week</button></div>
        <div className="brake-list">{refresh.combinations.map((item) => <div className="brake-garage-combo" key={item.carId && item.trackId ? `${item.carId}/${item.trackId}` : `${item.carName}/${item.trackName}`}><strong>{item.carName}</strong><span>{item.trackName} · Prepared {dateLabel(item.preparedAt)}{item.reviewNeeded ? ' · Needs review: no currently supported braking zones' : ''}</span></div>)}</div>
      </>
    </section>}
    <section className="brake-panel">
      <div className="brake-panel-head"><div><h2>Ready to practice</h2><span>Car and track combinations already processed</span></div></div>
      {refresh && refresh.combinations.length > 0 && <div className="brake-actions"><button type="button" disabled={!catalog?.configured || busy} onClick={() => void refreshAll()}>Queue refresh of all saved combinations</button></div>}
      {combinations.length === 0 ? <p>No Garage61 combinations have been prepared yet.</p> : <>
        <div className="brake-actions brake-ready-actions"><button type="button" disabled={deletableCombinations.length === 0 || busy} onClick={toggleAllReady}>{deletableCombinations.length > 0 && deletableCombinations.every((item) => selectedReady.has(item.id)) ? 'Clear selection' : 'Select all'}</button><button type="button" className="brake-delete-selected" disabled={selectedCombinations.length === 0 || selectedDeletionBlocked || busy} onClick={() => void deleteSelected()}>Delete selected ({selectedCombinations.length})</button></div>
        <div className="brake-list">{combinations.map((item) => <div className="brake-ready-combo" key={item.id}>{garageIDs(item.id) && <label className="brake-ready-select"><input type="checkbox" aria-label={`Select ${item.carName} / ${item.trackName}`} checked={selectedReady.has(item.id)} disabled={deletionBlocked(item)} onChange={() => toggleReady(item.id)} /><span>Select</span></label>}<button type="button" onClick={() => void practice(item.id)}><strong>{item.carName}</strong><span>{item.trackName}</span></button>{garageIDs(item.id) && <button type="button" className="brake-delete-combo" disabled={deletionBlocked(item)} onClick={() => void deleteDownloaded(item)}>Delete downloaded scenarios</button>}</div>)}</div>
      </>}
    </section>
  </main>
}

function label(entity: { name: string; variant?: string }) { return `${entity.name} ${entity.variant ?? ''}`.trim() }
function garageIDs(id: string) { const match = /^garage61-iracing-track-(\d+)-car-(\d+)-zone-\d+$/.exec(id); return match ? { track: Number(match[1]), car: Number(match[2]) } : null }
function combinationKey(item: Scenario) { const ids = garageIDs(item.id); return ids ? `${ids.car}/${ids.track}` : JSON.stringify([item.carName, item.trackName]) }
function queueStateLabel(state: GarageQueueItem['state']) { return ({ queued: 'Waiting', running: 'Processing', done: 'Prepared', review: 'Needs review', failed: 'Failed', unmatched: 'No unique catalog match', cancelled: 'Cancelled', deleted: 'Deleted locally' })[state] }
export function matchPlatformCombination(catalog: GarageCatalog, carPlatformId: string, trackPlatformId: string) {
  const cars = catalog.cars.filter((item) => item.platform_id === carPlatformId)
  const tracks = catalog.tracks.filter((item) => item.platform_id === trackPlatformId)
  return cars.length === 1 && tracks.length === 1 && cars[0] && tracks[0]
    ? { car: cars[0], track: tracks[0] }
    : null
}
function dateLabel(value: string | null) { return value ? new Date(value).toLocaleDateString() : 'later' }
function errorMessage(error: unknown) { return error instanceof Error ? error.message : 'Garage61 request failed' }
