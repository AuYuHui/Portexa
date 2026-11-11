<script setup lang="ts">
import { ref } from "vue";
import { RouterLink } from "vue-router";
import { RefreshCw } from "@lucide/vue";
import type { Account } from "../types";
import { api } from "../api";
import { refreshAccounts } from "../state";
import { money, date, positive, dailyLabel, failureLabel } from "../format";

const props = defineProps<{ account: Account }>();
const busy = ref(false);
const message = ref("");
const error = ref("");

async function refresh() {
  busy.value = true;
  error.value = "";
  message.value = "";
  try {
    await api(`/accounts/${props.account.id}/daily/sync`, "POST");
    message.value = "当日历史采集已加入队列，资金与持仓优先刷新。";
    await refreshAccounts();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <section class="panel daily-panel" aria-label="当日利润与结余">
    <div class="panel-heading">
      <div>
        <h2>当日利润 / 结余</h2>
        <p class="muted small">
          {{ account.daily.date || "按 MT4 服务器日期统计" }}
          <template v-if="account.daily.time_zone">
            · {{ account.daily.time_zone }}
          </template>
          <span
            class="badge"
            :class="
              account.daily.state === 'succeeded' && !account.daily.stale
                ? 'badge-good'
                : 'badge-warning'
            "
          >{{ dailyLabel(account.daily.state, account.daily.stale) }}</span>
        </p>
      </div>
      <button
        class="btn btn-small"
        :disabled="
          busy || !account.sync_enabled ||
          account.daily.state === 'time_zone_unconfirmed'
        "
        @click="refresh"
      ><RefreshCw :size="14" :class="{ spin: busy }" />刷新当日</button>
    </div>
    <div
      v-if="account.daily.state === 'time_zone_unconfirmed'"
      class="empty-inline"
    >
      系统正在通过新平仓交易自动校准时间，就绪后显示当日利润与结余，无需选择时区。
      <RouterLink to="/settings" class="text-button">查看服务器时间设置</RouterLink>
    </div>
    <template v-else>
      <div class="daily-metrics">
        <div>
          <div class="card-label">
            当日利润
            <span>{{
              account.daily.report?.native_currency ||
              account.snapshot?.native_currency || "原币"
            }}</span>
          </div>
          <div
            class="metric-value num"
            :class="account.daily.report
              ? (positive(account.daily.report.net_profit) ? 'positive' : 'negative')
              : ''"
          >
            {{ money(account.daily.report?.net_profit) }}
          </div>
          <p class="metric-note">已平仓净利润，含佣金、隔夜费及税费</p>
        </div>
        <div>
          <div class="card-label">
            当日结余
            <span>{{
              account.daily.report?.native_currency ||
              account.snapshot?.native_currency || "原币"
            }}</span>
          </div>
          <div
            class="metric-value num"
            :class="account.daily.report
              ? (positive(account.daily.report.balance_change) ? 'positive' : 'negative')
              : ''"
          >
            {{ money(account.daily.report?.balance_change) }}
          </div>
          <p class="metric-note">当日利润 + 余额调整，不含信用金</p>
        </div>
      </div>
      <p v-if="!account.daily.report" class="daily-note muted small">
        等待当日完整历史；尚未取得数据时不显示为零。
      </p>
      <p
        v-if="account.daily.error_code"
        class="daily-note warning-text small"
      >
        {{ failureLabel(account.daily.error_code) || "当日采集暂未完成" }}
        <template v-if="account.daily.raw_status != null">
          （服务端原码 {{ account.daily.raw_status }}）
        </template>
        {{ account.daily.report ? "，保留上次当日结果。" : "，尚无当日完整结果。" }}
      </p>
      <div v-if="account.daily.report" class="panel-footer">
        <span class="num">
          已平仓 {{ account.daily.report.closed_trades }} 笔 · 余额调整
          {{ money(account.daily.report.balance_adjustment) }} · 信用变动
          {{ money(account.daily.report.credit_change) }}
        </span>
        <span>
          采样完成 {{ date(account.daily.report.observed_to_ms) }} · 原币未换算
        </span>
      </div>
    </template>
    <p v-if="message" class="daily-note muted small" role="status">
      {{ message }}
    </p>
    <p v-if="error" class="daily-note warning-text small" role="alert">
      {{ error }}
    </p>
  </section>
</template>
