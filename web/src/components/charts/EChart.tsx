import * as echarts from 'echarts/core'
import { BarChart, LineChart } from 'echarts/charts'
import { GridComponent, LegendComponent, MarkLineComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { useEffect, useRef } from 'react'
import type { ComposeOption } from 'echarts/core'
import type { BarSeriesOption, LineSeriesOption } from 'echarts/charts'
import type { GridComponentOption, LegendComponentOption, MarkLineComponentOption, TooltipComponentOption } from 'echarts/components'
import { cn } from '../../lib/utils'

echarts.use([LineChart, BarChart, GridComponent, TooltipComponent, LegendComponent, MarkLineComponent, CanvasRenderer])

export type ChartOption = ComposeOption<LineSeriesOption | BarSeriesOption | GridComponentOption | TooltipComponentOption | LegendComponentOption | MarkLineComponentOption>

export function EChart({ option, className, ariaLabel }: { option: ChartOption; className?: string; ariaLabel: string }) {
  const container = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!container.current) return undefined
    const chart = echarts.init(container.current, undefined, { renderer: 'canvas' })
    chart.setOption({ animationDuration: 450, ...option })
    const observer = new ResizeObserver(() => chart.resize())
    observer.observe(container.current)
    return () => {
      observer.disconnect()
      chart.dispose()
    }
  }, [option])
  return <div ref={container} className={cn('h-72 w-full', className)} role="img" aria-label={ariaLabel} />
}

export function chartTextColor(): string {
  return getComputedStyle(document.documentElement).getPropertyValue('--chart-text').trim() || '#94a3b8'
}

export function chartGridColor(): string {
  return getComputedStyle(document.documentElement).getPropertyValue('--chart-grid').trim() || 'rgba(148,163,184,.12)'
}
