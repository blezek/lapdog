import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'

import type { ComboCell } from './api'
import { hours, labelForKey } from './format'
import { ComboHeatmap } from './pages/Dashboard'
import type { Theme } from './theme'

const capture = vi.hoisted(() => vi.fn())
vi.mock('./components/Chart', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./components/Chart')>()),
  Chart: (props: unknown) => { capture(props); return null },
}))

interface HeatmapOption {
  xAxis: { data: string[] }
  yAxis: { data: string[] }
  series: { data: [number, number, number][] }[]
  tooltip: { formatter: (point: { value: [number, number, number] }) => string }
}

describe('car-and-track heatmap', () => {
  it.each([1, 3, 4])('matches row labels and values for %i pairings', (count) => {
    const cells: ComboCell[] = Array.from({ length: count }, (_, i) => [
      { combo: `Car ${i} at Track ${i}`, category: 'Race', hours: 10 - i, comboHours: 12 - i, carPlatformId: i+1, trackPlatformId: i+10 },
      { combo: `Car ${i} at Track ${i}`, category: 'Practice', hours: 2, comboHours: 12 - i, carPlatformId: i+1, trackPlatformId: i+10 },
    ]).flat()
    renderToStaticMarkup(<ComboHeatmap cells={cells} theme={{ seq: [] } as unknown as Theme} />)
    const { option } = capture.mock.lastCall![0] as { option: HeatmapOption }

    for (const value of option.series[0]!.data) {
      const [x, y, amount] = value
      const row = option.yAxis.data[y]!
      const column = option.xAxis.data[x]!
      const source = cells.find((cell) => cell.combo === row && labelForKey(cell.category) === column)!
      expect({ amount, tooltip: option.tooltip.formatter({ value }) }).toEqual({
        amount: source.hours,
        tooltip: `${row}<br/>${column}<br/><strong>${hours(source.hours)}</strong> driving`,
      })
    }
  })
})
