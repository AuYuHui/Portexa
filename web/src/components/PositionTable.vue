<script setup lang="ts">
import { useI18n } from "vue-i18n";
import type { Position } from "../types";
import { money, signedMoney, price, positive, wallTime } from "../format";

const { t } = useI18n();
defineProps<{ positions: Position[] }>();
const commands = [
  "buy",
  "sell",
  "buy limit",
  "sell limit",
  "buy stop",
  "sell stop",
];
</script>
<template>
  <div class="table-scroll">
    <table class="data-table positions-table">
      <thead>
        <tr>
          <th>{{ t('positionTable.ticketSymbol') }}</th>
          <th>{{ t('positionTable.type') }}</th>
          <th class="text-right">{{ t('positionTable.lots') }}</th>
          <th class="text-right">{{ t('positionTable.openPrice') }}</th>
          <th class="text-right">{{ t('positionTable.currentPrice') }}</th>
          <th class="text-right">{{ t('account.floatingNetPL') }}</th>
          <th>{{ t('positionTable.openTimeServerWallClock') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="p in positions" :key="p.ticket">
          <td>
            <strong>{{ p.symbol }}</strong
            ><small class="cell-note num">#{{ p.ticket }}</small>
          </td>
          <td>
            <span
              class="badge"
              :class="p.command % 2 === 0 ? 'badge-info' : 'badge-danger'"
              >{{ commands[p.command] }}</span
            >
          </td>
          <td class="text-right num">{{ p.lots }}</td>
          <td class="text-right num">{{ price(p.open_price, p.digits) }}</td>
          <td class="text-right num">{{ price(p.current_price, p.digits) }}</td>
          <td class="text-right num">
            <strong :class="positive(p.net_profit) ? 'positive' : 'negative'">{{
              p.command <= 1 ? signedMoney(p.net_profit) : "—"
            }}</strong
            ><small
              v-if="p.command <= 1"
              class="cell-note muted"
              :title="t('positionTable.commissionSwapTaxes', { value1: p.commission, value2: p.swap, value3: p.taxes })"
              >{{ t('positionTable.feesIncluded') }}</small
            >
          </td>
          <td class="small muted num">{{ wallTime(p.open_time_wall) }}</td>
        </tr>
      </tbody>
    </table>
    <p v-if="!positions.length" class="empty-inline">
      {{ t('positionTable.noOrdersOfThisTypeInTheCurrent') }}
    </p>
  </div>
</template>
