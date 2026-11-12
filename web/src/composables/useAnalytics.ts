import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useQuery } from "@tanstack/vue-query";
import { api } from "../api";
import { beijingInput, parseBeijingInput } from "./useEquitySeries";
import type { AnalyticsReport } from "../types/analytics";
import type { TrendRange } from "../types/trends";

const durations = {
  "24h": 86_400_000,
  "7d": 7 * 86_400_000,
  "30d": 30 * 86_400_000,
  "90d": 90 * 86_400_000,
};
export function useAnalytics() {
  const route = useRoute();
  const router = useRouter();
  const range = computed<TrendRange>(() =>
    ["24h", "7d", "30d", "90d", "custom"].includes(String(route.query.range))
      ? (route.query.range as TrendRange)
      : "30d",
  );
  const account = computed(() =>
    typeof route.query.account === "string" ? route.query.account : "",
  );
  const unit = computed(() =>
    account.value && route.query.unit === "native" ? "native" : "USD",
  );
  const from = computed(() => Number(route.query.from));
  const to = computed(() => Number(route.query.to));
  const customFrom = ref(beijingInput(Date.now() - durations["30d"]));
  const customTo = ref(beijingInput(Date.now()));
  const invalid = computed(
    () =>
      range.value === "custom" &&
      (!Number.isSafeInteger(from.value) ||
        !Number.isSafeInteger(to.value) ||
        from.value <= 0 ||
        to.value - from.value < 60_000 ||
        to.value - from.value > durations["90d"] ||
        to.value > Date.now()),
  );
  watch(
    [range, from, to],
    () => {
      if (range.value === "custom" && !invalid.value) {
        customFrom.value = beijingInput(from.value);
        customTo.value = beijingInput(to.value);
      }
    },
    { immediate: true },
  );
  const query = useQuery({
    queryKey: computed(() => [
      "analytics",
      account.value,
      unit.value,
      range.value,
      range.value === "custom" ? from.value : 0,
      range.value === "custom" ? to.value : 0,
    ]),
    enabled: computed(() => !invalid.value),
    queryFn: ({ signal }) => {
      const end =
        range.value === "custom"
          ? to.value
          : Math.floor(Date.now() / 60_000) * 60_000;
      const start =
        range.value === "custom" ? from.value : end - durations[range.value];
      const params = new URLSearchParams({
        from_ms: String(start),
        to_ms: String(end),
        unit: unit.value,
      });
      if (account.value) params.set("account_id", account.value);
      return api<AnalyticsReport>(
        `/analytics?${params}`,
        "GET",
        undefined,
        signal,
      );
    },
    staleTime: 30_000,
    refetchInterval: computed(() =>
      range.value === "custom" ? false : 60_000,
    ),
  });
  function update(values: Record<string, string>) {
    void router.replace({ query: { ...route.query, ...values } });
  }
  function selectRange(value: TrendRange) {
    update(
      value === "custom"
        ? {
            range: value,
            from: String(parseBeijingInput(customFrom.value)),
            to: String(parseBeijingInput(customTo.value)),
          }
        : { range: value },
    );
  }
  return {
    ...query,
    range,
    account,
    unit,
    customFrom,
    customTo,
    invalid,
    selectRange,
    selectAccount: (value: string) => update({ account: value, unit: "USD" }),
    selectUnit: (value: string) => update({ unit: value }),
    applyCustom: () => selectRange("custom"),
  };
}
