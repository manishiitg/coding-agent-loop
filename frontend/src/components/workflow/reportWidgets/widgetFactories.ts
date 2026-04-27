// Synthetic widget builders used by the API-driven widgets (CostsWidget,
// EvalsWidget, RunsWidget) when they internally render a TableWidget or
// ChartWidget over data fetched from the workflow API. Centralised here so
// each API widget can build the same shape without re-declaring defaults.

import type { ReportFormatterName, ReportWidget } from '../../../services/api-types'

export function makeSyntheticTableWidget(options: {
  formats?: Record<string, ReportFormatterName>
  defaultSort?: { field: string; direction: 'asc' | 'desc' }
  colorBy?: string
  colorMap?: Record<string, string>
  hideColumns?: string[]
} = {}): ReportWidget {
  return {
    kind: 'table',
    source: '',
    path: '',
    enableSearch: true,
    pageSize: 10,
    ...options,
  }
}

export function makeSyntheticChartWidget(options: {
  chartType?: 'bar' | 'line' | 'area'
  xAxis?: string
  yAxis?: string
  series?: string[]
  stacked?: boolean
  format?: ReportFormatterName
} = {}): ReportWidget {
  return {
    kind: 'chart',
    source: '',
    path: '',
    chartType: options.chartType ?? 'bar',
    xAxis: options.xAxis,
    yAxis: options.yAxis,
    series: options.series,
    stacked: options.stacked,
    format: options.format,
    showValues: false,
    height: 260,
  }
}
