/*
 * ECharts wrapper.
 *
 * echarts is used directly rather than through a React binding: the bindings lag
 * React releases, and the whole surface needed here is "render this option object
 * into this div and resize with it".
 *
 * Only the chart types actually used are imported, so tree shaking keeps the
 * bundle to what the interface draws.
 */

import { useEffect, useRef, useState } from 'react'
import type { RefObject } from 'react'
import * as echarts from 'echarts/core'
import { BarChart, LineChart, HeatmapChart, CustomChart } from 'echarts/charts'
import {
  GridComponent,
  TooltipComponent,
  DataZoomComponent,
  CalendarComponent,
  MarkLineComponent,
  VisualMapComponent,
} from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { UniversalTransition } from 'echarts/features'

echarts.use([
  BarChart,
  LineChart,
  HeatmapChart,
  CustomChart,
  GridComponent,
  TooltipComponent,
  DataZoomComponent,
  CalendarComponent,
  MarkLineComponent,
  VisualMapComponent,
  CanvasRenderer,
  // UniversalTransition lets a series match its data across updates by identity
  // rather than by array index. Charts whose rows are sorted by value need it: when
  // the order changes, index matching would animate a bar from one category's slot
  // into another's, which reads as the wrong category moving.
  UniversalTransition,
])

export interface ChartProps {
  /** option is an ECharts option object. */
  option: echarts.EChartsCoreOption
  className?: string
  /** onEvent subscribes to a chart event, used for click-through navigation. */
  onEvent?: { type: string; handler: (params: unknown) => void }
  /** ariaLabel describes the chart for assistive technology. */
  ariaLabel: string
}

export function Chart({ option, className = 'chart', onEvent, ariaLabel }: ChartProps) {
  const host = useRef<HTMLDivElement | null>(null)
  const chart = useRef<echarts.ECharts | null>(null)

  useEffect(() => {
    if (!host.current) return
    const instance = echarts.init(host.current, undefined, { renderer: 'canvas' })
    chart.current = instance

    // Charts sit in a flexible grid, so they must follow their container rather
    // than only the window.
    const observer = new ResizeObserver(() => instance.resize())
    observer.observe(host.current)

    return () => {
      observer.disconnect()
      instance.dispose()
      chart.current = null
    }
  }, [])

  const reducedMotion = usePrefersReducedMotion()

  useEffect(() => {
    const instance = chart.current
    if (!instance) return

    // Animation defaults are applied here rather than per chart, so every chart
    // transitions the same way. A chart can still override them, since the caller's
    // option is spread last.
    // The cast is needed because spreading the caller's option widens the easing
    // fields to plain strings; the values themselves are valid easing names.
    const withMotion = {
      animation: !reducedMotion,
      animationDuration: reducedMotion ? 0 : 400,
      animationDurationUpdate: reducedMotion ? 0 : 400,
      animationEasing: 'cubicOut',
      animationEasingUpdate: 'cubicOut',
      ...option,
    } as echarts.EChartsCoreOption

    // replaceMerge rather than notMerge.
    //
    // notMerge disposes and rebuilds the series, so a filter change had nothing to
    // transition from and the chart simply snapped to its new shape. replaceMerge
    // swaps the listed components wholesale — so a series that no longer exists is
    // still removed rather than lingering — while letting ECharts diff the values
    // it can and animate between them.
    //
    // tooltip is in the list for correctness, not appearance: the formatters close
    // over the data arrays, so leaving the old tooltip in place would show numbers
    // from the previous filter on hover.
    instance.setOption(withMotion, {
      replaceMerge: [
        'series',
        'xAxis',
        'yAxis',
        'tooltip',
        'grid',
        'visualMap',
        'calendar',
        'dataZoom',
      ],
    })
  }, [option, reducedMotion])

  useEffect(() => {
    const instance = chart.current
    if (!instance || !onEvent) return
    instance.on(onEvent.type, onEvent.handler)
    return () => {
      instance.off(onEvent.type, onEvent.handler)
    }
  }, [onEvent])

  return <div ref={host} className={className} role="img" aria-label={ariaLabel} />
}

/**
 * useElementWidth reports an element's content width and follows resizes.
 *
 * The calendar needs it. That chart's natural width is a function of how many weeks
 * are in range, so whether the grid fits — and therefore whether its cells can stay
 * square — can only be decided against the width actually available. Assuming a width
 * clipped two years of data off both ends of the card.
 */
export function useElementWidth<T extends HTMLElement>(): [RefObject<T | null>, number] {
  const ref = useRef<T | null>(null)
  const [width, setWidth] = useState(0)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    setWidth(el.getBoundingClientRect().width)
    const observer = new ResizeObserver((entries) => {
      // The observer is documented to deliver at least one entry, but the array is
      // still typed as possibly sparse, and reading the element directly is both
      // honest about that and unaffected by a batched delivery.
      const entry = entries[0]
      if (entry) setWidth(entry.contentRect.width)
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  return [ref, width]
}

/**
 * usePrefersReducedMotion reports the operating system's reduced-motion setting.
 *
 * Someone who has asked their system to reduce motion has asked for a reason, and a
 * chart that slides and grows on every filter change is exactly the kind of motion
 * they turned off. Animation is skipped entirely for them rather than shortened.
 */
export function usePrefersReducedMotion(): boolean {
  const [reduced, setReduced] = useState(
    () => window.matchMedia('(prefers-reduced-motion: reduce)').matches,
  )
  useEffect(() => {
    const media = window.matchMedia('(prefers-reduced-motion: reduce)')
    const update = () => setReduced(media.matches)
    media.addEventListener('change', update)
    return () => media.removeEventListener('change', update)
  }, [])
  return reduced
}

/**
 * baseGrid is the plot area every chart shares.
 *
 * Generous left padding keeps axis labels off the edge without needing per-chart
 * tuning, and containLabel lets ECharts size around whatever the labels are.
 */
export const baseGrid = { left: 8, right: 14, top: 18, bottom: 8, containLabel: true }

/** axisLine returns a recessive axis in the current theme. */
export function axisStyle(colour: string, lineColour: string) {
  return {
    axisLine: { lineStyle: { color: lineColour } },
    axisTick: { show: false },
    axisLabel: { color: colour, fontSize: 11 },
    splitLine: { show: false },
  }
}

/** valueAxisStyle returns a value axis with a hairline grid and no axis line. */
export function valueAxisStyle(colour: string, lineColour: string) {
  return {
    axisLine: { show: false },
    axisTick: { show: false },
    axisLabel: { color: colour, fontSize: 11 },
    splitLine: { lineStyle: { color: lineColour, width: 1 } },
  }
}

/** tooltipStyle returns the shared tooltip chrome. */
export function tooltipStyle(surface: string, text: string, line: string) {
  return {
    backgroundColor: surface,
    borderColor: line,
    borderWidth: 1,
    textStyle: { color: text, fontSize: 12 },
    extraCssText: 'box-shadow: 0 4px 14px rgba(0,0,0,.12); border-radius: 6px;',
  }
}
