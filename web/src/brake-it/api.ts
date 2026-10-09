import type { BrakeDevice, BrakeSettings, RunResult, Scenario } from './types'

async function response<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`
    try {
      const body = (await res.json()) as { error?: string }
      if (body.error) message = body.error
    } catch {
      // Keep the status text when the response is not JSON.
    }
    throw new Error(message)
  }
  return (await res.json()) as T
}

async function mutate<T>(url: string, method: string, body: unknown): Promise<T> {
  return response<T>(
    await fetch(url, {
      method,
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  )
}

export type GarageEntity = { id: number; name: string; variant?: string; platform: string; platform_id?: string }
export type GarageCatalog = { configured: boolean; cars: GarageEntity[]; tracks: GarageEntity[] }
export type GarageTokenStatus = { configured: boolean; source: 'file' | 'environment' | 'none' }
export type GarageJob = { runId: number; queueItemId?: number; state: 'idle' | 'running' | 'complete' | 'partial' | 'review' | 'failed' | 'cancelled' | 'unmatched' | 'deleted'; message: string; carId: number; trackId: number; count: number; scenarioId?: string; all?: boolean; index?: number; total?: number; succeeded?: number; reviewNeeded?: number; errors?: string[] }
export type GarageQueueItem = { id: number; carPlatformId: number; trackPlatformId: number; garageCarId: number; garageTrackId: number; carName: string; trackName: string; source: 'history' | 'manual' | 'refresh'; state: 'queued' | 'running' | 'done' | 'review' | 'failed' | 'unmatched' | 'cancelled' | 'deleted'; queuedAt: string; startedAt?: string; finishedAt?: string; attemptCount: number; error?: string }
export type GarageSavedCombination = { carName: string; trackName: string; preparedAt: string; carId?: number; trackId?: number; reviewNeeded: boolean }
export type GarageRefresh = { combinations: GarageSavedCombination[]; dueCount: number; reminderDue: boolean; snoozeUntil: string | null }
export type MyDrivenCombination = { carPlatformId: number; trackPlatformId: number; carName: string; trackName: string; trackConfig: string; drivingHours: number }

export const brakeApi = {
  garageCatalog: () => fetch('/api/brake-it/garage61/catalog').then(response<GarageCatalog>),
  garageTokenStatus: () => fetch('/api/brake-it/garage61/token', { cache: 'no-store' }).then(response<GarageTokenStatus>),
  saveGarageToken: (token: string) => mutate<GarageTokenStatus>('/api/brake-it/garage61/token', 'PUT', { token }),
  deleteGarageToken: () => mutate<GarageTokenStatus>('/api/brake-it/garage61/token', 'DELETE', {}),
  garageRefresh: () => fetch('/api/brake-it/garage61/refresh').then(response<GarageRefresh>),
  garageMyCombinations: () => fetch('/api/brake-it/garage61/my-combinations').then(response<MyDrivenCombination[]>),
  snoozeGarage: (until: string) => mutate<GarageRefresh>('/api/brake-it/garage61/snooze', 'PUT', { until }),
  garageJob: () => fetch('/api/brake-it/garage61/job').then(response<GarageJob>),
  garageQueue: () => fetch('/api/brake-it/garage61/queue').then(response<GarageQueueItem[]>),
  enqueueGarage: (carId: number, trackId: number) => mutate<GarageQueueItem[]>('/api/brake-it/garage61/queue', 'POST', { carId, trackId }),
  enqueueRefreshAllGarage: () => mutate<GarageQueueItem[]>('/api/brake-it/garage61/queue', 'POST', { all: true }),
  retryGarageQueue: (id: number) => mutate<GarageQueueItem[]>(`/api/brake-it/garage61/queue/${id}/retry`, 'POST', {}),
  deleteGarageCombination: (carId: number, trackId: number) => mutate<{ deleted: number }>(`/api/brake-it/garage61/combinations/${carId}/${trackId}`, 'DELETE', {}),
  deleteGarageCombinations: (combinations: { carId: number; trackId: number }[]) => mutate<{ deleted: number }>('/api/brake-it/garage61/combinations/delete', 'POST', { combinations }),
  processGarage: (carId: number, trackId: number) => mutate<GarageJob>('/api/brake-it/garage61/job', 'POST', { carId, trackId }),
  refreshAllGarage: () => mutate<GarageJob>('/api/brake-it/garage61/job', 'POST', { all: true }),
  cancelGarage: () => mutate<{ ok: boolean }>('/api/brake-it/garage61/job', 'DELETE', {}),
  scenarios: () => fetch('/api/brake-it/scenarios').then(response<Scenario[]>),
  createScenario: (scenario: Scenario) =>
    mutate<Scenario>('/api/brake-it/scenarios', 'POST', scenario),
  updateScenario: (scenario: Scenario) =>
    mutate<Scenario>(`/api/brake-it/scenarios/${encodeURIComponent(scenario.id)}`, 'PUT', scenario),
  deleteScenario: async (id: string): Promise<void> => {
    const res = await fetch(`/api/brake-it/scenarios/${encodeURIComponent(id)}`, {
      method: 'DELETE',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: '{}',
    })
    if (!res.ok) await response<never>(res)
  },
  results: () => fetch('/api/brake-it/results').then(response<RunResult[]>),
  saveResult: (result: RunResult) =>
    mutate<RunResult>('/api/brake-it/results', 'POST', result),
  settings: () => fetch('/api/brake-it/settings').then(response<BrakeSettings>),
  saveSettings: (settings: BrakeSettings) =>
    mutate<BrakeSettings>('/api/brake-it/settings', 'PUT', settings),
  devices: () => fetch('/api/brake-it/devices').then(response<BrakeDevice[]>),
  createDevice: (device: BrakeDevice) =>
    mutate<BrakeDevice>('/api/brake-it/devices', 'POST', device),
  updateDevice: (device: BrakeDevice) =>
    mutate<BrakeDevice>(`/api/brake-it/devices/${encodeURIComponent(device.id)}`, 'PUT', device),
  deleteDevice: async (id: string): Promise<void> => {
    const res = await fetch(`/api/brake-it/devices/${encodeURIComponent(id)}`, {
      method: 'DELETE',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: '{}',
    })
    if (!res.ok) await response<never>(res)
  },
}
