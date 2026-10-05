import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderToStaticMarkup } from 'react-dom/server'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { App } from './App'
import type { Config } from './api'
import { Brand } from './components/Brand'
import { SplashScreen } from './components/SplashScreen'

const config: Config = {
  pollIntervalSeconds: 1,
  minSessionSeconds: 30,
  captureEnabled: true,
  captureMaxBytes: 2 * 1024 * 1024 * 1024,
  port: 47047,
  startWithWindows: true,
  units: 'metric',
  theme: 'system',
  debug: true,
}

function renderApp(initialEntry = '/settings'): string {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.setQueryData(['settings'], config)
  return renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <App />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('Brand', () => {
  it('uses the bundled LapDog artwork without repeating the adjacent name', () => {
    const html = renderToStaticMarkup(<Brand />)

    expect(html).toContain('class="brand-logo"')
    expect(html).toContain('src="/src/assets/lapdog-icon.png"')
    expect(html).toContain('width="32" height="32"')
    expect(html).toContain('aria-hidden="true"')
    expect(html).toContain('LapDog')
  })

  it('appears in the application sidebar', () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const html = renderToStaticMarkup(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <App />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expect(html).toContain('class="brand-logo"')
  })
})

describe('SplashScreen', () => {
  it('renders the bundled artwork with its intrinsic square dimensions', () => {
    const html = renderToStaticMarkup(<SplashScreen visible />)

    expect(html).toContain('class="splash-screen"')
    expect(html).toContain('src="/src/assets/lapdog-icon.png"')
    expect(html).toContain('width="1024" height="1024"')
    expect(html).toContain('A racing dog driving a red number one race car')
  })

  it('hides the retained overlay after startup so it can fade out', () => {
    const html = renderToStaticMarkup(<SplashScreen visible={false} />)

    expect(html).toContain('class="splash-screen splash-hidden"')
    expect(html).toContain('aria-hidden="true"')
  })
})

describe('Brake-It discovery', () => {
  it('does not expose its experimental setting or LapDog navigation link', () => {
    const html = renderApp()

    expect(html).not.toContain('Enable Brake-It link')
    expect(html).not.toContain('href="/brake-it/simulator"')
  })

  it('keeps the direct Brake-It route available', () => {
    expect(renderApp('/brake-it/simulator')).toContain('Loading Brake-it…')
  })
})
