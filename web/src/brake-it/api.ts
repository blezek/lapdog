import type { BrakeSettings, RunResult, Scenario } from './types'

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

export const brakeApi = {
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
}
