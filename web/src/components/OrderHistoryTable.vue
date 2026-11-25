<script setup lang="ts">
import { computed, ref, shallowRef } from "vue";
import { useI18n } from "vue-i18n";
import { price, signedMoney, wallTime } from "../format";
import { profitTone } from "./analytics/performanceFormat";
import { trendDate } from "./charts/trendFormat";
import type { ClosedOrder } from "../types/orderHistory";
import Modal from "./Modal.vue";

const props = defineProps<{ items: ClosedOrder[]; currency: string; protocol: "mt4" | "mt5" }>();
const { t } = useI18n();
const selected = shallowRef<ClosedOrder>();
const open = ref(false);
function select(order: ClosedOrder) {
  selected.value = order;
  open.value = true;
}
function closeTime(order: ClosedOrder, exact = false) {
  if (order.close_time_ms == null) {
    return wallTime(Math.floor(order.close_time_wall_ms / 1000), order.close_time_wall_ms % 1000);
  }
  const display = trendDate(order.close_time_ms);
  return exact && props.protocol === "mt5"
    ? display + "." + String(order.close_time_ms % 1000).padStart(3, "0")
    : display;
}
const detailFields = computed(() => {
  const row = selected.value;
  if (!row) return [];
  const fields: [string, string | null | undefined][] = [
    [t("orderHistory.orderTicket"), row.order_ticket],
    [t("orderHistory.dealTicket"), row.deal_ticket],
    [t("orderHistory.side"), row.side === "buy" ? t("mt5.buy") : t("mt5.sell")],
    [t("positionTable.lots"), row.lots],
    [t("positionTable.openTimeServerWallClock"), row.open_time_wall > 0 ? wallTime(row.open_time_wall) : null],
    [t("positionTable.openPrice"), row.open_price != null ? price(row.open_price, row.digits) : null],
    [t("orderHistory.closedAt"), closeTime(row, true)],
    [props.protocol === "mt5" ? t("mt5.executionPrice") : t("orderHistory.closePrice"), price(row.close_price, row.digits)],
    [t("orderHistory.grossPL"), signedMoney(row.profit)],
    [t("mt5.commission"), signedMoney(row.commission)],
    [t("orderHistory.swap"), signedMoney(row.swap)],
    [t("mt5.tax"), props.protocol === "mt4" ? signedMoney(row.taxes) : null],
    [t("format.fee"), row.fee != null ? signedMoney(row.fee) : null],
  ];
  return fields.filter(([, value]) => value != null && value !== "");
});
</script>

<template>
  <div class="table-scroll">
    <table class="data-table history-table">
      <thead>
        <tr>
          <th scope="col">{{ t("positionTable.ticketSymbol") }}</th>
          <th scope="col" class="history-desktop">{{ t("orderHistory.side") }}</th>
          <th scope="col" class="history-desktop text-right">{{ t("positionTable.lots") }}</th>
          <th scope="col" class="history-desktop">{{ t("orderHistory.closedAt") }}</th>
          <th scope="col" class="text-right">{{ t("analytics.amount") }} · {{ currency }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="order in items" :key="order.deal_ticket || order.order_ticket" data-history-row @click="select(order)">
          <td>
            <button type="button" class="text-button history-symbol" :aria-label="t('orderHistory.viewRow', { symbol: order.symbol, ticket: order.deal_ticket || order.order_ticket })" @click.stop="select(order)">
              {{ order.symbol }}
            </button>
            <small v-if="order.order_ticket" class="cell-note num">#{{ order.order_ticket }}</small>
            <small v-if="order.deal_ticket" class="cell-note num">{{ t("analytics.dealNumber", { ticket: order.deal_ticket }) }}</small>
            <small class="history-mobile">{{ order.side === "buy" ? t("mt5.buy") : t("mt5.sell") }} · {{ t("analytics.tradeLots", { lots: order.lots }) }}</small>
          </td>
          <td class="history-desktop">{{ order.side === "buy" ? t("mt5.buy") : t("mt5.sell") }}</td>
          <td class="history-desktop text-right num">{{ order.lots }}</td>
          <td class="history-desktop num">{{ closeTime(order) }}</td>
          <td class="text-right">
            <strong class="num" :class="profitTone(order.net_profit)">{{ signedMoney(order.net_profit) }}</strong>
            <small class="history-mobile num">{{ closeTime(order) }}</small>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
  <Modal v-model:open="open" :title="selected?.symbol || ''" :description="protocol === 'mt5' ? t('orderHistory.closedDeal') : t('orderHistory.closedOrder')">
    <template v-if="selected">
      <div class="history-modal-profit">
        <span>{{ t("analytics.amount") }}</span>
        <strong class="num" :class="profitTone(selected.net_profit)">{{ signedMoney(selected.net_profit) }} {{ currency }}</strong>
      </div>
      <dl class="history-fields">
        <div v-for="([label, value]) in detailFields" :key="label">
          <dt>{{ label }}</dt>
          <dd class="num">{{ value }}</dd>
        </div>
      </dl>
    </template>
  </Modal>
</template>

<style scoped>
.history-table { min-width: 580px; }
.history-table tbody tr { cursor: pointer; }
.history-symbol { font-weight: 650; font-size: 13px; }
.history-mobile { display: none; }
.history-modal-profit, .history-fields > div {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 16px;
}
.history-modal-profit { font-size: 13px; color: var(--color-muted); margin-bottom: 18px; }
.history-modal-profit strong { font-size: 22px; color: var(--color-ink); }
.history-fields { margin: 0; }
.history-fields > div { padding: 7px 0; border-bottom: 1px solid var(--color-line-subtle); font-size: 12px; }
.history-fields dt { color: var(--color-muted); }
.history-fields dd { margin: 0; text-align: right; overflow-wrap: anywhere; }
.history-table strong.is-positive, .history-modal-profit .is-positive { color: var(--color-brand-700); }
.history-table strong.is-negative, .history-modal-profit .is-negative { color: #b4493d; }
@media (max-width: 760px) {
  .history-table { table-layout: fixed; min-width: 0; white-space: normal; }
  .history-table th, .history-table td { padding: 10px 14px; overflow-wrap: anywhere; }
  .history-table :is(th, td):last-child { width: 42%; }
  .history-desktop { display: none; }
  .history-mobile { display: block; margin-top: 3px; font-size: 10px; color: var(--color-muted); }
}
</style>
