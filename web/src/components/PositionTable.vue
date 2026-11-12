<script setup lang="ts">
import { computed, shallowRef, useId } from "vue";
import { useI18n } from "vue-i18n";
import { ChevronDown, ChevronUp } from "@lucide/vue";
import type { Position } from "../types";
import { signedMoney, price, positive, wallTime } from "../format";

const { t } = useI18n();
const props = defineProps<{ positions: Position[] }>();
const tableId = useId();
const collapsedLimit = 10;
const expanded = shallowRef(false);
const visiblePositions = computed(() =>
  expanded.value ? props.positions : props.positions.slice(0, collapsedLimit),
);
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
    <table :id="tableId" class="data-table positions-table">
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
        <tr v-for="p in visiblePositions" :key="p.ticket">
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
  <div v-if="positions.length > collapsedLimit" class="panel-footer">
    <span>{{ t('positionTable.showingCount', { shown: visiblePositions.length, total: positions.length }) }}</span>
    <button
      type="button"
      class="btn btn-small"
      :aria-expanded="expanded"
      :aria-controls="tableId"
      @click="expanded = !expanded"
    >
      {{ expanded ? t('positionTable.showLess') : t('positionTable.showAll') }}
      <ChevronUp v-if="expanded" :size="14" aria-hidden="true" />
      <ChevronDown v-else :size="14" aria-hidden="true" />
    </button>
  </div>
</template>
