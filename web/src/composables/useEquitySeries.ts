import { computed, ref, watch, type MaybeRefOrGetter, toValue } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useQuery } from "@tanstack/vue-query";
import { api } from "../api";
import type { TrendMetric, TrendRange, TrendSeries, TrendUnit } from "../types/trends";

const day = 86_400_000;
const durations = { "24h": day, "7d": 7 * day, "30d": 30 * day, "90d": 90 * day };
const ranges: TrendRange[] = ["24h", "7d", "30d", "90d", "custom"];

function single(value: unknown): string {
  return typeof value === "string" ? value : "";
}

export function beijingInput(stamp: number): string {
  return new Date(stamp + 8 * 3_600_000).toISOString().slice(0, 16);
}

export function parseBeijingInput(value: string): number {
  if (!/^\d{4}-\d\d-\d\dT\d\d:\d\d$/.test(value)) return NaN;
  const stamp = Date.parse(value + "+08:00");
  return Number.isFinite(stamp) && beijingInput(stamp) === value ? stamp : NaN;
}

export function useEquitySeries(accountId: MaybeRefOrGetter<string | undefined>) {
  const route = useRoute();
  const router = useRouter();
  const range = computed<TrendRange>(() => {
    const value = single(route.query.trange) as TrendRange;
    return ranges.includes(value) ? value : "7d";
  });
  const metric = computed<TrendMetric>(() => {
    const value = single(route.query.tmetric);
    return value === "balance" || value === "equity" ? value : "both";
  });
  const unit = computed<TrendUnit>(() =>
    !toValue(accountId) || route.query.tunit === "USD" ? "USD" : "native",
  );
  const from = computed(() => Number(single(route.query.tfrom)));
  const to = computed(() => Number(single(route.query.tto)));
  const customFrom = ref(beijingInput(Date.now() - 7 * day));
  const customTo = ref(beijingInput(Date.now()));
  const invalid = computed(() => range.value === "custom" && (
    !Number.isSafeInteger(from.value) || !Number.isSafeInteger(to.value) ||
    from.value <= 0 || to.value <= from.value || to.value - from.value > 90 * day ||
    to.value > Date.now() + 60_000
  ));
  watch([range, from, to], () => {
    if (range.value === "custom" && !invalid.value) {
      customFrom.value = beijingInput(from.value);
      customTo.value = beijingInput(to.value);
    }
  }, { immediate: true });

  const query = useQuery({
    queryKey: computed(() => ["equity-history", toValue(accountId) || "portfolio", range.value, unit.value,
      range.value === "custom" ? from.value : 0, range.value === "custom" ? to.value : 0]),
    enabled: computed(() => !invalid.value),
    queryFn: ({ signal }) => {
      const end = range.value === "custom" ? to.value : Date.now();
      const start = range.value === "custom" ? from.value : end - durations[range.value];
      const path = toValue(accountId) ? `/accounts/${encodeURIComponent(toValue(accountId)!)}/equity-history` : "/portfolio/equity-history";
      const params = new URLSearchParams({ from_ms: String(start), to_ms: String(end), unit: unit.value });
      return api<TrendSeries>(`${path}?${params}`, "GET", undefined, signal);
    },
    staleTime: 30_000,
    refetchInterval: computed(() => range.value === "custom" ? false : 60_000),
  });

  function update(values: Record<string, string>) {
    void router.replace({ query: { ...route.query, ...values } });
  }
  function selectRange(value: TrendRange) {
    update(value === "custom" ? {
      trange: value, tfrom: String(parseBeijingInput(customFrom.value)), tto: String(parseBeijingInput(customTo.value)),
    } : { trange: value });
  }
  function applyCustom() {
    selectRange("custom");
  }
  return { ...query, range, metric, unit, customFrom, customTo, invalid,
    selectRange, applyCustom, selectMetric: (value: TrendMetric) => update({ tmetric: value }),
    selectUnit: (value: TrendUnit) => update({ tunit: value }) };
}
