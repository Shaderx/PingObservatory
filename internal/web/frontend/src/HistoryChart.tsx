import { useEffect, useRef } from 'react';
import { init, use } from 'echarts/core';
import { LineChart } from 'echarts/charts';
import { GridComponent, TooltipComponent, AriaComponent } from 'echarts/components';
import { SVGRenderer } from 'echarts/renderers';
import type { History } from './types';
import { historyWindows } from './history-windows';
use([LineChart, GridComponent, TooltipComponent, AriaComponent, SVGRenderer]);
export const colors = ['#c5ef69', '#78c9dc', '#d7a3df', '#edb775'];

export function createHistoryChartOption(histories: History[], labels: string[], theme: string) {
  const light = theme === 'light';
  const reducedMotion = typeof matchMedia !== 'undefined' && matchMedia('(prefers-reduced-motion: reduce)').matches;
  const long = histories.some(history => historyWindows[history.window].long);
  const starts = histories.map(history => Date.parse(history.start));
  const ends = histories.map(history => Date.parse(history.end));
  return {
    animation: !reducedMotion,
    color: light ? ['#507a09', '#177e9a', '#9552a0', '#a36917'] : colors,
    aria: { enabled: true, description: 'Timing histories from one station. Gaps mark missing observations. Use the history summary or JSON download to read the same data.' },
    grid: { left: 52, right: 20, top: 25, bottom: 32 },
    tooltip: { trigger: 'axis' as const, renderMode: 'richText', backgroundColor: light ? '#fff' : '#1d2820', borderColor: '#58634c', textStyle: { color: light ? '#182416' : '#eff3e9' }, confine: true },
    xAxis: { type: 'time' as const, ...(long && starts.every(Number.isFinite) && ends.every(Number.isFinite) ? { min: Math.min(...starts), max: Math.max(...ends) } : {}), axisLabel: { hideOverlap: true, color: light ? '#58624f' : '#8d9b8d', formatter: (v: number) => long ? new Date(v).toLocaleDateString([], { month: 'short', day: 'numeric' }) : new Date(v).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) }, axisLine: { lineStyle: { color: '#52604b' } }, splitLine: { show: false } },
    yAxis: { type: 'value' as const, name: 'ms', nameTextStyle: { color: '#8d9b8d' }, axisLabel: { color: light ? '#58624f' : '#8d9b8d' }, splitLine: { lineStyle: { color: light ? '#dae0d3' : '#263127', type: 'dashed' } } },
    series: histories.map((h, i) => ({ name: labels[i], type: 'line' as const, showSymbol: true, symbol: 'circle', symbolSize: 5, smooth: false, connectNulls: false, lineStyle: { width: 2 }, data: h.points.map(p => [p.at, p.median_ms]), areaStyle: histories.length === 1 ? { opacity: .06 } : undefined })),
  };
}

export function HistoryChart({ histories, labels, theme }: { histories: History[]; labels: string[]; theme: string }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!ref.current || !histories.length) return;
    const chart = init(ref.current, undefined, { renderer: 'svg' });
    chart.setOption(createHistoryChartOption(histories, labels, theme));
    const observer = new ResizeObserver(() => chart.resize()); observer.observe(ref.current);
    return () => { observer.disconnect(); chart.dispose(); };
  }, [histories, labels, theme]);
  return <div ref={ref} className="history-chart" role="img" aria-label="Timing history chart. Gaps mark missing observations. The history summary and JSON download follow the chart." />;
}
