import { useEffect, useRef, useState } from 'react'

import { clamp } from './scenario'
import type { PedalInput } from './types'

export function useKeyboardController(active: boolean): PedalInput {
  const keys = useRef(new Set<string>())
  const value = useRef<PedalInput>({ accelerator: 0, brake: 0, source: 'keyboard' })
  const [input, setInput] = useState(value.current)

  useEffect(() => {
    if (!active) {
      keys.current.clear()
      return
    }
    const down = (event: KeyboardEvent) => {
      if (!event.key.startsWith('Arrow')) return
      event.preventDefault()
      keys.current.add(event.key)
    }
    const up = (event: KeyboardEvent) => {
      if (!event.key.startsWith('Arrow')) return
      event.preventDefault()
      keys.current.delete(event.key)
    }
    window.addEventListener('keydown', down, { passive: false })
    window.addEventListener('keyup', up, { passive: false })
    return () => {
      window.removeEventListener('keydown', down)
      window.removeEventListener('keyup', up)
      keys.current.clear()
    }
  }, [active])

  useEffect(() => {
    if (!active) return
    let frame = 0
    let last = performance.now()
    const tick = (now: number) => {
      const dt = Math.min(0.05, (now - last) / 1000)
      last = now
      let accelerator = value.current.accelerator
      let brake = value.current.brake
      accelerator += keys.current.has('ArrowUp') ? 260 * dt : -360 * dt
      brake += keys.current.has('ArrowDown') ? 220 * dt : -130 * dt
      if (keys.current.has('ArrowRight')) brake += 55 * dt
      if (keys.current.has('ArrowLeft')) brake -= 55 * dt
      value.current = {
        accelerator: clamp(accelerator, 0, 100),
        brake: clamp(brake, 0, 100),
        source: 'keyboard',
      }
      setInput(value.current)
      frame = requestAnimationFrame(tick)
    }
    frame = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(frame)
  }, [active])

  return input
}
