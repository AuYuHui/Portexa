<script setup lang="ts">
import { computed, ref } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { api } from "../../api";
import { signedMoney } from "../../format";
import { localizeMessage } from "../../i18n";
import { trendDate } from "../charts/trendFormat";
import { performanceEvent, performanceReasons } from "./performanceFormat";
import type {
  PerformanceActivity,
  PerformanceDay,
} from "../../types/analytics";
const props = defineProps<{
  day: PerformanceDay;
  accountId: string;
  unit: string;
  currency: string;
}>();
const { t } = useI18n();
const offset = ref(0);
const { data, error, isPending, refetch } = useQuery({
  queryKey: computed(() => [
    "analytics-activity",
    props.accountId,
    props.unit,
    props.day.from_ms,
    props.day.to_ms,
    offset.value,
  ]),
  queryFn: ({ signal }) => {
    const params = new URLSearchParams({
      from_ms: String(props.day.from_ms),
      to_ms: String(props.day.to_ms),
      unit: props.unit,
      offset: String(offset.value),
    });
    if (props.accountId) params.set("account_id", props.accountId);
    return api<PerformanceActivity>(
      `/analytics/activity?${params}`,
      "GET",
      undefined,
      signal,
    );
  },
});
const breakdown = computed(() => [
  { label: t("analytics.netProfit"), metric: props.day.net_profit },
  { label: t("analytics.realized"), metric: props.day.realized },
  { label: t("analytics.floatingChange"), metric: props.day.floating_change },
  { label: t("analytics.otherIncome"), metric: props.day.other_income },
  { label: t("analytics.costs"), metric: props.day.costs },
]);
</script>

<template>
  <div class="performance-day-details">
    <p class="muted small">
      {{ trendDate(day.from_ms) }} — {{ trendDate(day.to_ms) }} ·
      {{ t("trend.beijing") }}
    </p>
    <dl class="performance-breakdown">
      <div v-for="item in breakdown" :key="item.label">
        <dt>{{ item.label }}</dt>
        <dd class="num">{{ signedMoney(item.metric.value) }} {{ currency }}</dd>
      </div>
    </dl>
    <p v-if="day.net_profit.reasons.length" class="notice notice-warning">
      {{ performanceReasons(day.net_profit.reasons) }}
    </p>
    <h3>{{ t("analytics.activity") }}</h3>
    <p class="muted small">{{ t("analytics.activityHint") }}</p>
    <p v-if="error" role="alert" class="notice notice-error">
      {{ localizeMessage(error.message)
      }}<button class="text-button" @click="refetch()">
        {{ t("overview.retry") }}
      </button>
    </p>
    <p v-if="isPending" role="status">{{ t("analytics.loading") }}</p>
    <div v-if="data" class="table-scroll">
      <table class="data-table">
        <thead>
          <tr>
            <th>{{ t("analytics.time") }}</th>
            <th>{{ t("analytics.type") }}</th>
            <th>{{ t("analytics.ticket") }}</th>
            <th class="text-right">{{ t("analytics.amount") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="event in data.items"
            :key="`${event.account_id}:${event.ticket}:${event.revision}`"
          >
            <td class="num">{{ trendDate(event.time_ms) }}</td>
            <td>
              {{ performanceEvent(event.kind)
              }}<small v-if="event.symbol">{{ event.symbol }}</small>
            </td>
            <td>
              <RouterLink
                :to="`/accounts/${event.account_id}`"
                class="text-button"
                >#{{ event.ticket }}</RouterLink
              >
            </td>
            <td class="text-right num">
              {{ signedMoney(event.amount) }} {{ data.currency }}
            </td>
          </tr>
          <tr v-if="!data.items.length">
            <td colspan="4" class="muted">{{ t("analytics.noActivity") }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-if="data && data.total > 50" class="performance-pagination">
      <button
        class="btn btn-small"
        :disabled="offset === 0"
        @click="offset -= 50"
      >
        {{ t("trend.previous") }}</button
      ><span
        >{{ Math.floor(offset / 50) + 1 }} /
        {{ Math.ceil(data.total / 50) }}</span
      ><button
        class="btn btn-small"
        :disabled="offset + 50 >= data.total"
        @click="offset += 50"
      >
        {{ t("trend.next") }}
      </button>
    </div>
  </div>
</template>
