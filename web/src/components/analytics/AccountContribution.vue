<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { ArrowUpRight, ArrowDownUp } from "@lucide/vue";
import { signedMoney, percent } from "../../format";
import type { PerformanceResult } from "../../types/analytics";
import { performanceReasons } from "./performanceFormat";
const props = defineProps<{ accounts: PerformanceResult[] }>();
const { t } = useI18n();
const sort = ref<"return_pct" | "net_profit" | "max_drawdown_pct">(
  "return_pct",
);
function compare(a: string, b: string): number {
  const [wholeA, fractionA = ""] = a.split(".");
  const [wholeB, fractionB = ""] = b.split(".");
  const digits = Math.max(fractionA.length, fractionB.length);
  const x = BigInt(`${wholeA}${fractionA.padEnd(digits, "0")}`);
  const y = BigInt(`${wholeB}${fractionB.padEnd(digits, "0")}`);
  return x < y ? -1 : x > y ? 1 : 0;
}
const rows = computed(() =>
  [...props.accounts].sort((a, b) => {
    const x = a.metrics[sort.value].value,
      y = b.metrics[sort.value].value;
    if (x === null) return y === null ? 0 : 1;
    if (y === null) return -1;
    return compare(x, y) * (sort.value === "max_drawdown_pct" ? 1 : -1);
  }),
);
</script>

<template>
  <section
    class="panel performance-contributions"
    :aria-label="t('analytics.contribution')"
  >
    <div class="performance-panel-heading">
      <div>
        <span class="performance-section-no">03 /</span>
        <h2>{{ t("analytics.contribution") }}</h2>
        <p>{{ t("analytics.comparisonHint") }}</p>
      </div>
      <span class="badge badge-neutral">USD</span>
    </div>
    <div class="table-scroll">
      <table class="data-table performance-account-table">
        <thead>
          <tr>
            <th>{{ t("analytics.account") }}</th>
            <th class="text-right">
              <button @click="sort = 'net_profit'">
                {{ t("analytics.netProfit") }}<ArrowDownUp :size="12" />
              </button>
            </th>
            <th class="text-right">
              <button @click="sort = 'return_pct'">
                {{ t("analytics.return") }}<ArrowDownUp :size="12" />
              </button>
            </th>
            <th class="text-right">
              <button @click="sort = 'max_drawdown_pct'">
                {{ t("analytics.maxDrawdown") }}<ArrowDownUp :size="12" />
              </button>
            </th>
            <th class="text-right">{{ t("analytics.currentDrawdown") }}</th>
            <th>{{ t("analytics.dataQuality") }}</th>
            <th />
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rows" :key="row.account_id">
            <td>
              <RouterLink
                :to="`/accounts/${row.account_id}`"
                class="performance-account-name"
                >{{ row.label }}</RouterLink
              ><small>{{
                t("analytics.observations", {
                  valid: row.quality.valid_points,
                  total: row.quality.expected_points,
                })
              }}</small>
            </td>
            <td
              class="text-right num"
              :class="
                row.metrics.net_profit.value?.startsWith('-')
                  ? 'negative'
                  : 'positive'
              "
            >
              {{ signedMoney(row.metrics.net_profit.value) }}
            </td>
            <td class="text-right num">
              {{ percent(row.metrics.return_pct.value) }}
            </td>
            <td class="text-right num">
              {{ percent(row.metrics.max_drawdown_pct.value) }}
            </td>
            <td class="text-right num">
              {{ percent(row.metrics.current_drawdown_pct.value) }}
            </td>
            <td>
              <span
                class="badge"
                :class="
                  row.quality.reasons.length ? 'badge-warning' : 'badge-good'
                "
                >{{
                  row.quality.reasons.length
                    ? t("analytics.insufficient")
                    : t("analytics.confirmedRange")
                }}</span
              >
              <small
                v-if="row.quality.reasons.length"
                :title="performanceReasons(row.quality.reasons)"
                >{{
                  performanceReasons(row.quality.reasons.slice(0, 1))
                }}</small
              >
            </td>
            <td>
              <RouterLink
                :to="`/accounts/${row.account_id}`"
                class="icon-button"
                :aria-label="t('analytics.openAccount', { name: row.label })"
                ><ArrowUpRight :size="16"
              /></RouterLink>
            </td>
          </tr>
          <tr v-if="!rows.length">
            <td colspan="7" class="muted">{{ t("analytics.noAccounts") }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
