<script setup lang="ts">
import { shallowRef } from "vue";
import { useI18n } from "vue-i18n";
import { ChevronDown } from "@lucide/vue";
import { price, signedMoney, wallTime } from "../format";
import { profitTone } from "./analytics/performanceFormat";
import type { Position } from "../types";

defineProps<{ positions: Position[]; protocol: "mt4" | "mt5"; currency: string }>();
const { t } = useI18n();
const expanded = shallowRef<number>();
function toggle(ticket: number) {
  expanded.value = expanded.value === ticket ? undefined : ticket;
}
</script>

<template>
  <div class="table-scroll">
    <table class="data-table live-rows">
      <thead>
        <tr>
          <th scope="col">{{ protocol === "mt5" ? t("livePositions.positionSymbol") : t("positionTable.ticketSymbol") }}</th>
          <th scope="col">{{ t("livePositions.sideLots") }}</th>
          <th scope="col" class="live-desktop">{{ t("positionTable.openPrice") }}</th>
          <th scope="col" class="live-desktop">{{ t("positionTable.currentPrice") }}</th>
          <th scope="col" class="text-right">{{ t("account.floatingNetPL") }} · {{ currency }}</th>
        </tr>
      </thead>
      <tbody>
        <template v-for="p in positions" :key="p.ticket">
          <tr data-live-position>
            <td>
              <button type="button" class="live-expand" :aria-expanded="expanded === p.ticket" :aria-label="t('livePositions.showDetails', { ticket: p.ticket.toString() })" @click="toggle(p.ticket)">
                <ChevronDown :size="14" :class="{ expanded: expanded === p.ticket }" />
                <span><strong>{{ p.symbol }}</strong><small class="num">#{{ p.ticket }}</small></span>
              </button>
            </td>
            <td>{{ p.command === 0 ? t("mt5.buy") : t("mt5.sell") }}<small>{{ t("analytics.tradeLots", { lots: p.lots }) }}</small></td>
            <td class="num live-desktop">{{ price(p.open_price, p.digits) }}</td>
            <td class="num live-desktop">{{ price(p.current_price, p.digits) }}</td>
            <td class="text-right num"><strong :class="profitTone(p.net_profit)">{{ signedMoney(p.net_profit) }}</strong></td>
          </tr>
          <tr v-if="expanded === p.ticket" class="live-expanded">
            <td colspan="5">
              <dl>
                <div><dt>{{ t("positionTable.openTimeServerWallClock") }}</dt><dd>{{ wallTime(p.open_time_wall) }}</dd></div>
                <div class="live-mobile"><dt>{{ t("positionTable.openPrice") }}</dt><dd>{{ price(p.open_price, p.digits) }}</dd></div>
                <div class="live-mobile"><dt>{{ t("positionTable.currentPrice") }}</dt><dd>{{ price(p.current_price, p.digits) }}</dd></div>
                <div><dt>{{ t("livePositions.stopLoss") }}</dt><dd>{{ /^0(?:\.0+)?$/.test(p.sl) ? '—' : price(p.sl, p.digits) }}</dd></div>
                <div><dt>{{ t("livePositions.takeProfit") }}</dt><dd>{{ /^0(?:\.0+)?$/.test(p.tp) ? '—' : price(p.tp, p.digits) }}</dd></div>
                <div><dt>{{ t("mt5.commission") }}</dt><dd>{{ signedMoney(p.commission) }} {{ currency }}</dd></div>
                <div><dt>{{ t("livePositions.swap") }}</dt><dd>{{ signedMoney(p.swap) }} {{ currency }}</dd></div>
                <div v-if="protocol === 'mt4'"><dt>{{ t("livePositions.taxes") }}</dt><dd>{{ signedMoney(p.taxes) }} {{ currency }}</dd></div>
              </dl>
            </td>
          </tr>
        </template>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.live-rows small {
  display: block;
  color: var(--color-muted);
  font-size: 11px;
}
.live-expand {
  display: inline-flex;
  align-items: center;
  text-align: left;
  max-width: 100%;
  gap: 6px;
  background: transparent;
  border: 0;
  padding: 0;
}
.live-expand svg { color: var(--color-muted); flex-shrink: 0; }
.live-expand .expanded { transform: rotate(180deg); }
.live-rows strong.is-positive { color: var(--color-brand-700); }
.live-rows strong.is-negative { color: #b4493d; }
.live-expanded td { background: var(--color-canvas); }
.live-expanded dl {
  margin: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 10px 24px;
}
.live-expanded dt { font-size: 11px; color: var(--color-muted); }
.live-expanded dd { margin: 4px 0 0; font-variant-numeric: tabular-nums; }
.live-mobile { display: none; }
@media (max-width: 720px) {
  .live-desktop { display: none; }
  .live-mobile { display: block; }
  .live-rows { table-layout: fixed; white-space: normal; overflow-wrap: anywhere; }
  .live-rows th:first-child { width: 40%; }
  .live-rows th:nth-child(2) { width: 24%; }
  .live-rows :is(th, td) { padding: 10px 6px; }
  .live-rows :is(th, td):first-child { padding-left: 10px; }
  .live-rows :is(th, td):last-child { padding-right: 10px; }
}
</style>
