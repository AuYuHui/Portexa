<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  init,
  use,
  graphic,
  type EChartsType,
  type ComposeOption,
} from "echarts/core";
import { LineChart, type LineSeriesOption } from "echarts/charts";
import {
  GridComponent,
  TooltipComponent,
  DataZoomComponent,
  MarkLineComponent,
  type GridComponentOption,
  type TooltipComponentOption,
  type DataZoomComponentOption,
} from "echarts/components";
import { CanvasRenderer } from "echarts/renderers";
import { Activity } from "@lucide/vue";
import { t, locale } from "../../i18n";
import { money, percent } from "../../format";
import { chartAmount, trendDate } from "../charts/trendFormat";
import type {
  PerformancePoint,
  PerformanceResult,
} from "../../types/analytics";

use([
  LineChart,
  GridComponent,
  TooltipComponent,
  DataZoomComponent,
  MarkLineComponent,
  CanvasRenderer,
]);
type Option = ComposeOption<
  | LineSeriesOption
  | GridComponentOption
  | TooltipComponentOption
  | DataZoomComponentOption
>;
const props = defineProps<{
  result: PerformanceResult;
  mode: "return" | "profit";
}>();
const emit = defineEmits<{ select: [point: PerformancePoint] }>();
const element = ref<HTMLDivElement>();
const visible = computed(() =>
  props.result.points.some(
    (p) => p.return_pct !== null || p.net_profit !== null,
  ),
);
let chart: EChartsType | undefined;
let observer: ResizeObserver | undefined;

function draw() {
  if (!chart) return;
  const percentage = props.mode === "return";
  const build = (
    field: "return_pct" | "net_profit" | "drawdown_pct",
    negative = false,
  ) => {
    const data: { value: [number, number | null]; pointIndex: number }[] = [];
    props.result.points.forEach((point, index) => {
      if (index && point.segment !== props.result.points[index - 1]!.segment)
        data.push({ value: [point.time_ms, null], pointIndex: index });
      const value = chartAmount(point[field]);
      data.push({
        value: [
          point.time_ms,
          value === null ? null : negative ? -value : value,
        ],
        pointIndex: index,
      });
    });
    return data;
  };
  const option: Option = {
    useUTC: true,
    animation: false,
    textStyle: {
      fontFamily: "-apple-system, BlinkMacSystemFont, Segoe UI, sans-serif",
    },
    grid: [
      { left: 18, right: 24, top: 20, height: "53%", containLabel: true },
      { left: 18, right: 24, top: "69%", height: "18%", containLabel: true },
    ],
    xAxis: [0, 1].map((gridIndex) => ({
      type: "time" as const,
      gridIndex,
      min: props.result.from_ms,
      max: props.result.to_ms,
      axisLine: { lineStyle: { color: "#e2e8f0" } },
      axisTick: { show: false },
      splitLine: { show: false },
      axisLabel: {
        show: gridIndex === 1,
        color: "#94a3b8",
        fontSize: 10,
        hideOverlap: true,
        formatter: (value: number) => trendDate(value, true),
      },
    })),
    yAxis: [
      {
        type: "value",
        scale: true,
        axisLabel: {
          color: "#64748b",
          fontSize: 10,
          formatter: (value: number) =>
            percentage ? `${value}%` : String(value),
        },
        splitLine: { lineStyle: { color: "#f1f5f9", type: "dashed" } },
      },
      {
        type: "value",
        gridIndex: 1,
        max: 0,
        axisLabel: { color: "#94a3b8", fontSize: 10, formatter: "{value}%" },
        splitLine: { lineStyle: { color: "#f1f5f9" } },
      },
    ],
    axisPointer: { link: [{ xAxisIndex: "all" }] },
    tooltip: {
      trigger: "axis",
      renderMode: "richText",
      confine: true,
      backgroundColor: "#fff",
      borderColor: "#e2e8f0",
      textStyle: { color: "#334155", fontSize: 12 },
      axisPointer: {
        type: "line",
        lineStyle: { color: "#94a3b8", type: "dashed" },
      },
      formatter: (params) => {
        const items = Array.isArray(params) ? params : [params];
        const item = items.find(
          (p) => p.data && typeof p.data === "object" && "pointIndex" in p.data,
        );
        const index =
          item?.data &&
          typeof item.data === "object" &&
          "pointIndex" in item.data
            ? Number(item.data.pointIndex)
            : -1;
        const point = props.result.points[index];
        if (!point) return "";
        return [
          trendDate(point.time_ms),
          `${t("analytics.netProfit")}  ${money(point.net_profit)} ${props.result.currency}`,
          `${t("analytics.return")}  ${percent(point.return_pct)}`,
          `${t("analytics.currentDrawdown")}  ${percent(point.drawdown_pct)}`,
          `${t("analytics.statisticalEquity")}  ${money(point.statistical_equity)} ${props.result.currency}`,
        ].join("\n");
      },
    },
    dataZoom: [
      {
        type: "inside",
        xAxisIndex: [0, 1],
        zoomOnMouseWheel: "ctrl",
        preventDefaultMouseMove: false,
      },
      {
        type: "slider",
        xAxisIndex: [0, 1],
        bottom: 0,
        height: 18,
        showDetail: false,
        borderColor: "#e2e8f0",
        fillerColor: "rgba(5,150,105,.07)",
      },
    ],
    series: [
      {
        type: "line",
        data: build(percentage ? "return_pct" : "net_profit"),
        connectNulls: false,
        smooth: false,
        symbol: "circle",
        symbolSize: 3,
        showSymbol: false,
        lineStyle: { color: "#059669", width: 2.2 },
        itemStyle: { color: "#059669" },
        areaStyle: {
          color: new graphic.LinearGradient(0, 0, 0, 1, [
            { offset: 0, color: "rgba(16,185,129,.16)" },
            { offset: 1, color: "rgba(16,185,129,.01)" },
          ]),
        },
        markLine: {
          symbol: "none",
          silent: true,
          label: { show: false },
          lineStyle: { color: "#cbd5e1", type: "dashed", width: 1 },
          data: [{ yAxis: 0 }],
        },
      },
      {
        type: "line",
        xAxisIndex: 1,
        yAxisIndex: 1,
        data: build("drawdown_pct", true),
        connectNulls: false,
        smooth: false,
        showSymbol: false,
        lineStyle: { color: "#f59e0b", width: 1.3 },
        itemStyle: { color: "#f59e0b" },
        areaStyle: { color: "rgba(245,158,11,.12)" },
      },
    ],
  };
  chart.setOption(option, { notMerge: true });
}
onMounted(() => {
  if (!element.value) return;
  chart = init(element.value, undefined, { renderer: "canvas" });
  chart.getZr().on("click", (event) => {
    if (!chart?.containPixel({ gridIndex: 0 }, [event.offsetX, event.offsetY]))
      return;
    const value: unknown = chart.convertFromPixel({ gridIndex: 0 }, [
      event.offsetX,
      event.offsetY,
    ]);
    if (!Array.isArray(value) || typeof value[0] !== "number") return;
    const stamp = value[0];
    const point = props.result.points.reduce<PerformancePoint | undefined>(
      (nearest, candidate) =>
        !nearest ||
        Math.abs(candidate.time_ms - stamp) < Math.abs(nearest.time_ms - stamp)
          ? candidate
          : nearest,
      undefined,
    );
    if (point) emit("select", point);
  });
  observer = new ResizeObserver(() => chart?.resize());
  observer.observe(element.value);
  draw();
});
watch([() => props.result, () => props.mode, locale], draw, { flush: "post" });
onBeforeUnmount(() => {
  observer?.disconnect();
  chart?.dispose();
});
</script>

<template>
  <div class="performance-plot-wrap">
    <div
      ref="element"
      class="performance-plot"
      role="img"
      :aria-label="t('analytics.chartDescription')"
    />
    <div v-if="!visible" class="performance-plot-empty">
      <Activity :size="28" /><strong>{{ t("analytics.noCurve") }}</strong>
      <p>{{ t("analytics.noCurveHint") }}</p>
    </div>
    <span v-if="visible" class="performance-drawdown-label">{{
      t("analytics.drawdownCurve")
    }}</span>
  </div>
</template>
