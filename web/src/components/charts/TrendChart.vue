<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, watch } from "vue";
import { init, use, type EChartsType, type ComposeOption } from "echarts/core";
import { LineChart, type LineSeriesOption } from "echarts/charts";
import {
  GridComponent, TooltipComponent, DataZoomComponent, ToolboxComponent, MarkLineComponent,
  type GridComponentOption, type TooltipComponentOption, type DataZoomComponentOption,
  type ToolboxComponentOption,
} from "echarts/components";
import { CanvasRenderer } from "echarts/renderers";
import { t, locale } from "../../i18n";
import { money } from "../../format";
import type { TrendEvent, TrendMetric, TrendPoint } from "../../types/trends";
import { chartAmount, eventName, pointQuality, trendDate } from "./trendFormat";

use([LineChart, GridComponent, TooltipComponent, DataZoomComponent, ToolboxComponent, MarkLineComponent, CanvasRenderer]);
type Option = ComposeOption<LineSeriesOption | GridComponentOption | TooltipComponentOption | DataZoomComponentOption | ToolboxComponentOption>;
const props = defineProps<{
  points: TrendPoint[];
  events: TrendEvent[];
  metric: TrendMetric;
  currency: string;
  from: number;
  to: number;
  resetKey: string;
}>();
const emit = defineEmits<{ select: [point: TrendPoint] }>();
const element = ref<HTMLDivElement>();
let chart: EChartsType | undefined;
let observer: ResizeObserver | undefined;
let zoom = { start: 0, end: 100 };

function pointIndex(data: unknown): number | undefined {
  if (data && typeof data === "object" && "pointIndex" in data && typeof data.pointIndex === "number") return data.pointIndex;
}

function draw() {
  if (!chart) return;
  const metrics: ("equity" | "balance")[] = props.metric === "both" ? ["equity", "balance"] : [props.metric];
  const series: LineSeriesOption[] = metrics.map((metric, seriesIndex) => {
    const data: { value: [number, number | null]; pointIndex: number; symbolSize: number }[] = [];
    let previous: TrendPoint | undefined;
    props.points.forEach((point, index) => {
      if (previous && previous.segment !== point.segment) {
        data.push({ value: [point.time_ms, null], pointIndex: index, symbolSize: 0 });
      }
      const next = props.points[index + 1];
      const isolated = (!previous || previous.segment !== point.segment) && (!next || next.segment !== point.segment);
      data.push({ value: [point.time_ms, chartAmount(point[metric])], pointIndex: index, symbolSize: isolated ? 5 : 0 });
      previous = point;
    });
    return {
      id: metric, type: "line", name: metric === "equity" ? t("account.equity") : t("account.balance"),
      data, connectNulls: false, smooth: false, showSymbol: true, showAllSymbol: true,
      lineStyle: { width: metric === "equity" ? 2.4 : 1.6, type: metric === "balance" ? "dashed" : "solid" },
      itemStyle: { color: metric === "equity" ? "#059669" : "#64748b" },
      emphasis: { scale: true }, animation: false,
      ...(seriesIndex === 0 ? {
        markLine: {
          symbol: ["none", "none"], animation: false,
          lineStyle: { color: "#94a3b8", type: "dashed", width: 1 },
          label: { show: false },
          data: props.events.map((event) => ({ xAxis: event.time_ms, name: eventName(event) })),
        },
      } : {}),
    };
  });
  const option: Option = {
    animation: false,
    textStyle: { fontFamily: "-apple-system, BlinkMacSystemFont, Segoe UI, sans-serif" },
    grid: { left: 16, right: 24, top: 40, bottom: 65, containLabel: true },
    xAxis: { type: "time", min: props.from, max: props.to,
      axisLine: { lineStyle: { color: "#e2e8f0" } }, axisTick: { show: false },
      splitLine: { show: false }, axisLabel: { color: "#64748b", hideOverlap: true, fontSize: 10, formatter: (value: number) => trendDate(value, true) } },
    yAxis: { type: "value", scale: true, axisLabel: { color: "#64748b", fontSize: 10 },
      splitLine: { lineStyle: { color: "#f1f5f9", type: "dashed" } } },
    tooltip: { trigger: "axis", renderMode: "richText", confine: true,
      backgroundColor: "#ffffff", borderColor: "#e2e8f0", textStyle: { color: "#334155", fontSize: 12 },
      axisPointer: { type: "line", lineStyle: { color: "#94a3b8", type: "dashed" } },
      formatter: (params) => {
        const items = Array.isArray(params) ? params : [params];
        const index = items.map((item) => pointIndex(item.data)).find((value) => value !== undefined);
        const point = index === undefined ? undefined : props.points[index];
        if (!point) return "";
        return [
          trendDate(point.time_ms),
          `${t("account.equity")}  ${money(point.equity)} ${point.currency}`,
          `${t("account.balance")}  ${money(point.balance)} ${point.currency}`,
          `${t("trend.credit")}  ${money(point.credit)} ${point.currency}`,
          `${t("account.floatingNetPL")}  ${money(point.floating_net_profit)} ${point.currency}`,
          pointQuality(point),
          t("trend.accountCoverage", { included: point.included_accounts, expected: point.expected_accounts }),
        ].join("\n");
      } },
    dataZoom: [
      { type: "inside", ...zoom, zoomOnMouseWheel: "ctrl", moveOnMouseMove: true, preventDefaultMouseMove: false },
      { type: "slider", ...zoom, bottom: 10, height: 18, borderColor: "#e2e8f0",
        fillerColor: "rgba(5,150,105,0.08)", showDetail: false },
    ],
    toolbox: { right: 20, top: 2, feature: { dataZoom: { yAxisIndex: "none",
      title: { zoom: t("trend.dragZoom"), back: t("trend.resetZoom") } } } },
    series,
  };
  chart.setOption(option, { notMerge: true });
}

onMounted(() => {
  if (!element.value) return;
  chart = init(element.value, undefined, { renderer: "canvas" });
  chart.on("click", (event) => {
    const index = pointIndex(event.data);
    const point = index === undefined ? undefined : props.points[index];
    if (point) emit("select", point);
  });
  chart.getZr().on("click", (event) => {
    const position = [event.offsetX, event.offsetY];
    if (!chart?.containPixel({ gridIndex: 0 }, position)) return;
    const value: unknown = chart.convertFromPixel({ gridIndex: 0 }, position);
    if (!Array.isArray(value) || typeof value[0] !== "number") return;
    const stamp = value[0];
    const point = props.points.reduce<TrendPoint | undefined>((nearest, candidate) =>
      !nearest || Math.abs(candidate.time_ms - stamp) < Math.abs(nearest.time_ms - stamp) ? candidate : nearest, undefined);
    if (point) emit("select", point);
  });
  chart.on("datazoom", (event: unknown) => {
    if (event && typeof event === "object") {
      const data = "batch" in event && Array.isArray(event.batch) ? event.batch[0] : event;
      if (data && "start" in data && "end" in data && typeof data.start === "number" && typeof data.end === "number") {
        zoom = { start: data.start, end: data.end };
      }
    }
  });
  observer = new ResizeObserver(() => chart?.resize());
  observer.observe(element.value);
  draw();
});
watch(() => props.resetKey, () => { zoom = { start: 0, end: 100 }; });
watch([() => props.points, () => props.events, () => props.metric, () => props.currency, locale], draw, { flush: "post" });
onBeforeUnmount(() => { observer?.disconnect(); chart?.dispose(); chart = undefined; });
</script>

<template>
  <div ref="element" class="trend-chart" role="img" :aria-label="t('trend.chartDescription', { currency })" />
</template>
