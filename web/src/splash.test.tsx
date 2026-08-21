import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { SplashScreen } from './components/SplashScreen'

describe('SplashScreen', () => {
  it('renders the bundled artwork with its intrinsic square dimensions', () => {
    const html = renderToStaticMarkup(<SplashScreen visible />)

    expect(html).toContain('class="splash-screen"')
    expect(html).toContain('src="/src/assets/lapdog-icon.png"')
    expect(html).toContain('width="1024" height="1024"')
    expect(html).toContain('A racing dog driving a red number one race car')
  })

  it('leaves no overlay after startup', () => {
    expect(renderToStaticMarkup(<SplashScreen visible={false} />)).toBe('')
  })
})
