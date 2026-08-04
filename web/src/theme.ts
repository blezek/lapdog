/*
 * Theme access for charts.
 *
 * ECharts needs concrete colour values rather than CSS variables, so the tokens
 * are read back off the document. Reading them rather than duplicating the hex
 * values means there is one source of truth, and a chart cannot drift from the
 * rest of the interface or from the validated palette.
 */

import { useEffect, useState } from 'react'

export interface Theme {
  dark: boolean
  surface: string
  surface2: string
  textPrimary: string
  textSecondary: string
  textMuted: string
  line: string
  baseline: string
  accent: string
  deEmphasis: string
  /** series is the fixed categorical order. Never cycled. */
  series: string[]
  /** seq is the single-hue sequential ramp, light to dark. */
  seq: string[]
  statusGood: string
  statusWarning: string
  statusCritical: string
}

function readVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

function readTheme(): Theme {
  return {
    dark: isDark(),
    surface: readVar('--surface-1'),
    surface2: readVar('--surface-2'),
    textPrimary: readVar('--text-primary'),
    textSecondary: readVar('--text-secondary'),
    textMuted: readVar('--text-muted'),
    line: readVar('--line'),
    baseline: readVar('--baseline'),
    accent: readVar('--accent'),
    deEmphasis: readVar('--de-emphasis'),
    series: [1, 2, 3, 4, 5, 6, 7, 8].map((i) => readVar(`--series-${i}`)),
    seq: [0, 1, 2, 3, 4, 5, 6].map((i) => readVar(`--seq-${i}`)),
    statusGood: readVar('--status-good'),
    statusWarning: readVar('--status-warning'),
    statusCritical: readVar('--status-critical'),
  }
}

function isDark(): boolean {
  const stamp = document.documentElement.dataset.theme
  if (stamp === 'dark') return true
  if (stamp === 'light') return false
  return window.matchMedia('(prefers-color-scheme: dark)').matches
}

/**
 * useTheme returns the current token values, re-reading them when the theme
 * changes so charts recolour rather than keeping stale hexes.
 */
export function useTheme(): Theme {
  const [theme, setTheme] = useState<Theme>(() => readTheme())

  useEffect(() => {
    const refresh = () => setTheme(readTheme())

    const media = window.matchMedia('(prefers-color-scheme: dark)')
    media.addEventListener('change', refresh)

    // The theme toggle writes data-theme on the root element.
    const observer = new MutationObserver(refresh)
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['data-theme'],
    })

    return () => {
      media.removeEventListener('change', refresh)
      observer.disconnect()
    }
  }, [])

  return theme
}

/** applyTheme stamps the chosen theme onto the document. */
export function applyTheme(theme: 'system' | 'light' | 'dark'): void {
  if (theme === 'system') {
    delete document.documentElement.dataset.theme
    return
  }
  document.documentElement.dataset.theme = theme
}

/**
 * seriesColour returns the colour for a categorical slot.
 *
 * Past the eighth slot it returns the de-emphasis grey rather than generating a
 * new hue: a generated ninth colour is indistinguishable from an existing one
 * under colour-vision deficiency and would break every palette check. Callers
 * with more than eight categories should fold the tail into "Other".
 */
export function seriesColour(theme: Theme, index: number): string {
  return theme.series[index] ?? theme.deEmphasis
}
