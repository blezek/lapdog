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

import { useEffect, useRef } from 'react'
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

  useEffect(() => {
    const instance = chart.current
    if (!instance) return
    // notMerge, because a filter change can remove series entirely and a merged
    // update would leave the departed ones on screen.
    instance.setOption(option, { notMerge: true })
  }, [option])

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
