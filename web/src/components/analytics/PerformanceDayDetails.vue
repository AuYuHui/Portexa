<script setup lang="ts">
import { computed, ref } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import { api } from "../../api";
import { signedMoney } from "../../format";
import { localizeMessage } from "../../i18n";
import { trendDate } from "../charts/trendFormat";
import { profitTone } from "./performanceFormat";
import type {
  PerformanceActivity,
  PerformanceDay,
} from "../../types/analytics";
const props = defineProps<{
  day: PerformanceDay;
  accountId: string;
  accountNames: Record<string, string>;
  unit: string;
  currency: string;
}>();
const { t } = useI18n();
const offset = ref(0);
const { data, error, isPending, refetch } = useQuery({
  queryKey: computed(() => [
    "analytics-activity",
    "trades",
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
      kind: "trades",
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
const tone = computed(() => profitTone(props.day.realized.value));
const outcome = computed(() => ({
  "is-positive": t("analytics.profit"),
  "is-negative": t("analytics.loss"),
  "is-flat": t("analytics.flat"),
  "is-missing": t("analytics.dayDetails"),
})[tone.value]);
const sides = computed(() => ({
  buy: t("mt5.buy"),
  sell: t("mt5.sell"),
  buy_canceled: t("mt5.buyCanceled"),
  sell_canceled: t("mt5.sellCanceled"),
}));
const clock = new Intl.DateTimeFormat("en-GB", {
  timeZone: "Asia/Shanghai", hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23",
});
</script>

<template>
  <div class="performance-day-details">
    <dl class="performance-day-profit" data-day-metric="realized">
      <dt>{{ outcome }}</dt>
      <dd class="num" :class="tone">
        <template v-if="day.realized.value !== null">
          {{ signedMoney(day.realized.value) }} <small>{{ currency }}</small>
        </template>
        <template v-else>{{ t("analytics.calendarUnavailable") }}</template>
      </dd>
    </dl>
    <p v-if="day.partial" class="muted small" data-day-window>
      {{ trendDate(day.from_ms, true) }} – {{ trendDate(day.to_ms, true) }} · {{ t("trend.beijing") }}
    </p>
    <h3>{{ t("analytics.activity") }}</h3>
    <p v-if="error" role="alert" class="notice notice-error">
      {{ localizeMessage(error.message) }}
      <button class="text-button" @click="refetch()">{{ t("overview.retry") }}</button>
    </p>
    <p v-else-if="isPending" role="status">{{ t("analytics.loadingTrades") }}</p>
    <template v-else-if="data">
      <div v-if="data.items.length" class="performance-orders-scroll" tabindex="0" role="region" :aria-label="t('analytics.activity')">
        <table class="performance-orders">
          <thead>
            <tr>
              <th scope="col">{{ t("positionTable.ticketSymbol") }}</th>
              <th scope="col">{{ t("analytics.amount") }} · {{ data.currency }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="event in data.items" :key="`${event.account_id}:${event.order.deal_ticket || event.order.order_ticket}`">
              <td>
                <strong>{{ event.symbol }}</strong>
                {{ sides[event.order.side] }} · {{ t("analytics.tradeLots", { lots: event.order.lots }) }}
                <small v-if="event.order.order_ticket">#{{ event.order.order_ticket }}</small>
                <small v-if="event.order.deal_ticket">{{ t("analytics.dealNumber", { ticket: event.order.deal_ticket }) }}</small>
                <RouterLink v-if="!accountId && accountNames[event.account_id]" :to="`/accounts/${event.account_id}`" class="text-button">
                  {{ accountNames[event.account_id] }}
                </RouterLink>
              </td>
              <td>
                <strong class="num" :class="profitTone(event.amount)">{{ signedMoney(event.amount) }}</strong>
                <small class="num">{{ clock.format(event.time_ms) }}</small>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else class="muted small">
        {{ day.realized.value === null ? t("analytics.noVerifiedOrders") : t("analytics.noActivity") }}
      </p>
      <div v-if="data.total > 50" class="performance-pagination">
        <button class="btn btn-small" :disabled="offset === 0" @click="offset -= 50">
          {{ t("trend.previous") }}
        </button>
        <span>{{ Math.floor(offset / 50) + 1 }} / {{ Math.ceil(data.total / 50) }}</span>
        <button class="btn btn-small" :disabled="offset + 50 >= data.total" @click="offset += 50">
          {{ t("trend.next") }}
        </button>
      </div>
    </template>
  </div>
</template>
