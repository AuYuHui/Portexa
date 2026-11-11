<script setup lang="ts">
import type { Position } from "../types";
import { money, positive, wallTime } from "../format";
defineProps<{ positions: Position[] }>();
const commands = [
  "买入",
  "卖出",
  "Buy Limit",
  "Sell Limit",
  "Buy Stop",
  "Sell Stop",
];
</script>
<template>
  <div class="table-scroll">
    <table class="data-table positions-table">
      <thead>
        <tr>
          <th>订单号 / 品种</th>
          <th>方向</th>
          <th class="text-right">手数</th>
          <th class="text-right">开仓价</th>
          <th class="text-right">当前价</th>
          <th class="text-right">浮动净盈亏</th>
          <th>开仓时间 · 服务器墙钟</th>
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
              :class="p.command % 2 === 0 ? 'badge-good' : 'badge-info'"
              >{{ commands[p.command] }}</span
            >
          </td>
          <td class="text-right num">{{ p.lots }}</td>
          <td class="text-right num">{{ p.open_price }}</td>
          <td class="text-right num">{{ p.current_price }}</td>
          <td class="text-right num">
            <strong :class="positive(p.net_profit) ? 'positive' : 'negative'">{{
              p.command <= 1 ? money(p.net_profit) : "—"
            }}</strong
            ><small
              v-if="p.command <= 1"
              class="cell-note muted"
              :title="`佣金 ${p.commission} / 隔夜费 ${p.swap} / 税费 ${p.taxes}`"
              >已含费用</small
            >
          </td>
          <td class="small muted num">{{ wallTime(p.open_time_wall) }}</td>
        </tr>
      </tbody>
    </table>
    <p v-if="!positions.length" class="empty-inline">
      当前完整快照中没有此类订单。
    </p>
  </div>
</template>
