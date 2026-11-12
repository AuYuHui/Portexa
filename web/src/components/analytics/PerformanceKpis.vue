<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { Wallet, TrendingUp, CircleCheck, Activity, Info } from "@lucide/vue";
import { signedMoney, percent } from "../../format";
import type { PerformanceResult } from "../../types/analytics";
import { performanceReasons } from "./performanceFormat";
import { trendDate } from "../charts/trendFormat";

const props = defineProps<{ result: PerformanceResult }>();
const { t } = useI18n();
const cards = computed(() => [
  {
    key: "net",
    label: t("analytics.netProfit"),
    metric: props.result.metrics.net_profit,
    icon: Wallet,
    description: t("analytics.netFormula"),
    percentage: false,
  },
  {
    key: "return",
    label: t("analytics.return"),
    metric: props.result.metrics.return_pct,
    icon: TrendingUp,
    description: t("analytics.returnFormula"),
    percentage: true,
  },
  {
    key: "realized",
    label: t("analytics.realized"),
    metric: props.result.metrics.realized,
    icon: CircleCheck,
    description: t("analytics.realizedFormula"),
    percentage: false,
  },
  {
    key: "drawdown",
    label: t("analytics.maxDrawdown"),
    metric: props.result.metrics.max_drawdown_pct,
    icon: Activity,
    description: t("analytics.drawdownFormula"),
    percentage: true,
  },
]);
</script>

<template>
  <section class="performance-kpis" :aria-label="t('analytics.kpis')">
    <article
      v-for="card in cards"
      :key="card.key"
      class="performance-kpi"
      :class="{ 'performance-kpi-primary': card.key === 'net' }"
    >
      <div class="performance-kpi-label">
        <span>{{ card.label }}</span
        ><component :is="card.icon" :size="18" />
      </div>
      <div
        class="performance-kpi-value num"
        :class="{ 'is-loss': card.metric.value?.startsWith('-') }"
        :data-kpi="card.key"
      >
        {{
          card.percentage
            ? percent(card.metric.value)
            : signedMoney(card.metric.value)
        }}
        <small v-if="!card.percentage">{{ result.currency }}</small>
      </div>
      <div class="performance-kpi-status">
        <span
          class="performance-dot"
          :class="{ 'is-unavailable': card.metric.value === null }"
        />
        {{
          card.metric.value === null
            ? t("analytics.insufficient")
            : card.key === "return" || card.key === "drawdown"
              ? t("analytics.sampleEstimate")
              : t("analytics.confirmedRange")
        }}
      </div>
      <details class="performance-kpi-explanation">
        <summary><Info :size="13" />{{ t("analytics.calculation") }}</summary>
        <p>{{ card.description }}</p>
        <p v-if="card.key === 'drawdown'">
          {{ t("analytics.currentDrawdown") }}
          {{ percent(result.metrics.current_drawdown_pct.value) }}<br />
          <template v-if="result.drawdown.peak_ms"
            >{{ t("analytics.peak") }} {{ trendDate(result.drawdown.peak_ms)
            }}<br />{{ t("analytics.trough") }}
            {{ trendDate(result.drawdown.trough_ms) }}<br />{{
              result.drawdown.recovered_ms
                ? `${t("analytics.recovered")} ${trendDate(result.drawdown.recovered_ms)}`
                : t("analytics.notRecovered")
            }}</template
          >
        </p>
        <p v-if="card.metric.reasons.length" class="warning-text">
          {{ performanceReasons(card.metric.reasons) }}
        </p>
      </details>
    </article>
  </section>
</template>
