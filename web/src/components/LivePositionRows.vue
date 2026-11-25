<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { price, signedMoney, wallTime } from "../format";
import { profitTone } from "./analytics/performanceFormat";
import type { Position } from "../types";

defineProps<{ positions: Position[]; protocol: "mt4" | "mt5"; currency: string }>();
const { t } = useI18n();
</script>

<template>
  <div class="table-scroll" tabindex="0" role="group" :aria-label="t('account.openPositions')">
    <table class="data-table live-rows">
      <thead>
        <tr>
          <th scope="col">{{ protocol === "mt5" ? t("livePositions.positionSymbol") : t("positionTable.ticketSymbol") }}</th>
          <th scope="col">{{ t("livePositions.sideLots") }}</th>
          <th scope="col" class="text-right">{{ t("positionTable.openPrice") }}</th>
          <th scope="col" class="text-right">{{ t("positionTable.currentPrice") }}</th>
          <th scope="col" class="text-right">{{ t("account.floatingNetPL") }} · {{ currency }}</th>
          <th scope="col">{{ t("positionTable.openTimeServerWallClock") }}</th>
          <th scope="col" class="text-right">{{ t("livePositions.stopLoss") }}</th>
          <th scope="col" class="text-right">{{ t("livePositions.takeProfit") }}</th>
          <th scope="col" class="text-right">{{ t("mt5.commission") }}</th>
          <th scope="col" class="text-right">{{ t("livePositions.swap") }}</th>
          <th v-if="protocol === 'mt4'" scope="col" class="text-right">{{ t("livePositions.taxes") }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="p in positions" :key="p.ticket" data-live-position>
          <td>
            <strong>{{ p.symbol }}</strong>
            <small class="cell-note num">#{{ p.ticket }}</small>
          </td>
          <td>
            {{ p.command === 0 ? t("mt5.buy") : t("mt5.sell") }}
            <small class="cell-note">{{ t("analytics.tradeLots", { lots: p.lots }) }}</small>
          </td>
          <td class="text-right num">{{ price(p.open_price, p.digits) }}</td>
          <td class="text-right num">{{ price(p.current_price, p.digits) }}</td>
          <td class="text-right num"><strong :class="profitTone(p.net_profit)">{{ signedMoney(p.net_profit) }}</strong></td>
          <td class="num">{{ wallTime(p.open_time_wall) }}</td>
          <td class="text-right num">{{ /^0(?:\.0+)?$/.test(p.sl) ? "—" : price(p.sl, p.digits) }}</td>
          <td class="text-right num">{{ /^0(?:\.0+)?$/.test(p.tp) ? "—" : price(p.tp, p.digits) }}</td>
          <td class="text-right num">{{ signedMoney(p.commission) }} {{ currency }}</td>
          <td class="text-right num">{{ signedMoney(p.swap) }} {{ currency }}</td>
          <td v-if="protocol === 'mt4'" class="text-right num">{{ signedMoney(p.taxes) }} {{ currency }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.live-rows :is(th, td) { padding-inline: 10px; }
.live-rows :is(th, td):first-child { padding-left: 16px; }
.live-rows :is(th, td):last-child { padding-right: 16px; }
.live-rows small { color: var(--color-muted); }
.live-rows strong.is-positive { color: var(--color-brand-700); }
.live-rows strong.is-negative { color: #b4493d; }
</style>
