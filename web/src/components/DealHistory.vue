<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { useI18n } from "vue-i18n";
import { api } from "../api";
import { localizeMessage } from "../i18n";
import { price, signedMoney, wallTime } from "../format";
import type { Deal, DealsPage } from "../types";

const { t } = useI18n();
const props = defineProps<{ accountId: string; hasSnapshot: boolean }>();
const account = computed(() => props.accountId);
const cursor = ref("");
const previous = ref<string[]>([]);
const { data, error, isPending, isFetching } = useQuery({
  queryKey: ["deals", account, cursor],
  enabled: computed(() => props.hasSnapshot),
  queryFn: () => api<DealsPage>(`/accounts/${account.value}/deals?cursor=${encodeURIComponent(cursor.value)}`),
  refetchInterval: 10_000,
});
watch(account, () => {
  cursor.value = "";
  previous.value = [];
});

const dealTypes = computed(() => [
  t("mt5.buy"), t("mt5.sell"), t("mt5.balance"), t("mt5.credit"),
  t("mt5.charge"), t("mt5.correction"), t("mt5.bonus"), t("mt5.commission"),
  t("mt5.commissionDaily"), t("mt5.commissionMonthly"), t("mt5.agentCommissionDaily"),
  t("mt5.agentCommissionMonthly"), t("mt5.interest"), t("mt5.buyCanceled"),
  t("mt5.sellCanceled"), t("mt5.dividend"), t("mt5.frankedDividend"), t("mt5.tax"),
]);
const entries = computed(() => [t("mt5.entryIn"), t("mt5.entryOut"), t("mt5.entryInOut"), t("mt5.entryOutBy")]);

function tradeDeal(deal: Deal) {
  return deal.mt5_deal_type <= 1 || deal.mt5_deal_type === 13 || deal.mt5_deal_type === 14;
}
function next() {
  if (!data.value?.next_cursor) return;
  previous.value.push(cursor.value);
  cursor.value = data.value.next_cursor;
}
function back() {
  cursor.value = previous.value.pop() || "";
}
</script>

<template>
  <section class="panel deal-history-panel" :aria-label="t('mt5.dealHistory')">
    <div class="panel-heading">
      <div>
        <h2>{{ t("mt5.dealHistory") }}</h2>
        <p class="muted small">{{ t("mt5.dealHistoryBasis") }}</p>
      </div>
    </div>
    <p v-if="error" class="notice notice-error" role="alert">{{ localizeMessage(error.message) }}</p>
    <div v-else-if="data?.items.length" class="table-scroll">
      <table class="data-table">
        <thead>
          <tr>
            <th>{{ t("mt5.dealTicket") }}</th>
            <th>{{ t("positionTable.type") }}</th>
            <th>{{ t("mt5.dealTime") }}</th>
            <th class="text-right">{{ t("positionTable.lots") }}</th>
            <th class="text-right">{{ t("mt5.executionPrice") }}</th>
            <th class="text-right">{{ t("mt5.dealNet") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="deal in data.items" :key="deal.ticket">
            <td class="num">
              <strong>{{ deal.symbol || "#" + deal.ticket }}</strong>
              <small v-if="deal.symbol" class="cell-note">#{{ deal.ticket }}</small>
              <small v-if="deal.mt5_order_ticket" class="cell-note muted">
                {{ t("mt5.originatingOrder", { ticket: deal.mt5_order_ticket }) }}
              </small>
            </td>
            <td>
              {{ dealTypes[deal.mt5_deal_type] ?? t("mt5.otherDeal", { type: deal.mt5_deal_type }) }}
              <small v-if="tradeDeal(deal)" class="cell-note muted">{{ entries[deal.mt5_entry] }}</small>
              <small v-if="deal.comment" class="cell-note muted">{{ deal.comment }}</small>
            </td>
            <td class="small num">{{ wallTime(deal.event_time_wall, deal.mt5_wall_time_ms % 1000) }}</td>
            <td class="text-right num">{{ tradeDeal(deal) ? deal.exact_lots : "—" }}</td>
            <td class="text-right num">{{ tradeDeal(deal) ? price(deal.close_price, deal.digits) : "—" }}</td>
            <td class="text-right num" :title="t('mt5.costBreakdown', { commission: deal.commission, swap: deal.swap, fee: deal.fee })">
              {{ signedMoney(deal.net_profit) }} {{ deal.native_currency }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-else class="empty-inline">
      {{ isPending && hasSnapshot ? t("mt5.loadingDeals") : t("mt5.noDeals") }}
    </p>
    <div v-if="previous.length || data?.next_cursor" class="panel-footer">
      <button type="button" class="btn btn-small" :disabled="!previous.length || isFetching" @click="back">
        {{ t("mt5.previousPage") }}
      </button>
      <button type="button" class="btn btn-small" :disabled="!data?.next_cursor || isFetching" @click="next">
        {{ t("mt5.nextPage") }}
      </button>
    </div>
  </section>
</template>
