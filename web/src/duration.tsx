import { createContext, useContext, useMemo, type ReactNode } from 'react'

import type { Config } from './api'
import { clockDuration, hm, hms, hours } from './format'

type Preferences = Pick<Config, 'durationFormat' | 'showDurationSeconds'>

const defaults: Preferences = { durationFormat: 'current', showDurationSeconds: true }
const DurationContext = createContext<Preferences>(defaults)

export function DurationProvider({ config, children }: { config: Preferences | undefined; children: ReactNode }) {
  return <DurationContext.Provider value={config ?? defaults}>{children}</DurationContext.Provider>
}

/** useDuration formats elapsed totals while preserving each page's current default. */
export function useDuration() {
  const { durationFormat, showDurationSeconds } = useContext(DurationContext)
  return useMemo(() => ({
    isClock: durationFormat === 'clock',
    seconds: (value: number, current: 'hm' | 'hms' = 'hm') => {
      if (durationFormat === 'clock') return clockDuration(value, showDurationSeconds)
      return current === 'hms' ? hms(value) : hm(value)
    },
    hours: (value: number, digits = 1, unit = true) => {
      if (durationFormat === 'clock') return clockDuration(value * 3600, showDurationSeconds)
      return unit ? hours(value, digits) : value.toFixed(digits)
    },
  }), [durationFormat, showDurationSeconds])
}
