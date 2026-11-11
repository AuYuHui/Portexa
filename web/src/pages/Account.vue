<script setup lang="ts">
import { computed, ref } from "vue";
import { RouterLink, useRoute } from "vue-router";
import { useQuery } from "@tanstack/vue-query";
import {
  ArrowLeft,
  RefreshCw,
  Settings2,
  Pause,
  Play,
  Info,
  AlertTriangle,
} from "@lucide/vue";
import { api } from "../api";
import { refreshAccounts } from "../state";
import { money, date, failureLabel, positive } from "../format";
import type { Detail, Snapshot } from "../types";
import StatusBadge from "../components/StatusBadge.vue";
import Modal from "../components/Modal.vue";
import AccountForm from "../components/AccountForm.vue";
import PositionTable from "../components/PositionTable.vue";
const route = useRoute();
const id = computed(() => String(route.params.id));
const sort = ref("ticket");
const { data, error, isPending } = useQuery({
  queryKey: ["account", id, sort],
  queryFn: () => api<Detail>(`/accounts/${id.value}?sort=${sort.value}`),
  refetchInterval: 10_000,
});
const { data: snapshots, error: snapshotsError } = useQuery({
  queryKey: ["snapshots", id],
  queryFn: () => api<Snapshot[]>(`/accounts/${id.value}/snapshots?limit=10`),
  refetchInterval: 10_000,
});
const tab = ref("positions");
const open = ref(false);
const busy = ref(false);
const actionError = ref("");
const message = ref("");
const current = computed(() => data.value?.account.snapshot);
const positions = computed(
  () =>
    data.value?.positions.filter((p) =>
      tab.value === "positions" ? p.command <= 1 : p.command >= 2,
    ) || [],
);
async function requestSync() {
  busy.value = true;
  actionError.value = "";
  message.value = "";
  try {
    await api(`/accounts/${id.value}/sync`, "POST");
    message.value = "快照采集已加入队列。";
    await refreshAccounts();
  } catch (e) {
    actionError.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
async function toggleSync() {
  if (!data.value) return;
  busy.value = true;
  actionError.value = "";
  message.value = "";
  try {
    await api(`/accounts/${id.value}`, "PATCH", {
      config_version: data.value.account.config_version,
      sync_enabled: !data.value.account.sync_enabled,
    });
    await refreshAccounts();
  } catch (e) {
    actionError.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <div class="page-content">
    <RouterLink to="/" class="back-link"
      ><ArrowLeft :size="16" />全部账户</RouterLink
    >
    <p v-if="error" class="notice notice-error" role="alert">
      {{ error.message }}
    </p>
    <p v-if="isPending" class="empty-inline">正在加载账户…</p>
    <template v-if="data">
      <div class="page-title detail-title">
        <div>
          <span class="eyebrow">ACCOUNT DETAIL</span>
          <h1>{{ data.account.label }}<span class="title-dot">.</span></h1>
          <p class="muted num">
            {{ data.account.login }} <span class="divider-dot">/</span>
            {{ data.account.broker_name }} <span class="divider-dot">/</span>
            {{ data.account.server_name }}
          </p>
        </div>
        <div class="page-actions">
          <button class="btn" :disabled="busy" @click="open = true">
            <Settings2 :size="16" />账户配置</button
          ><button class="btn" :disabled="busy" @click="toggleSync">
            <Pause v-if="data.account.sync_enabled" :size="16" /><Play
              v-else
              :size="16"
            />{{ data.account.sync_enabled ? "暂停采集" : "恢复采集" }}</button
          ><button
            class="btn btn-primary"
            :disabled="busy || !data.account.sync_enabled"
            @click="requestSync"
          >
            <RefreshCw :size="16" :class="{ spin: busy }" />立即采集
          </button>
        </div>
      </div>
      <p v-if="actionError" role="alert" class="notice notice-error">
        {{ actionError }}
      </p>
      <p v-if="message" role="status" class="notice notice-subtle">
        {{ message }}
      </p>
      <div class="detail-status">
        <StatusBadge
          :state="data.account.sync_state"
          :stale="data.account.stale"
        /><span class="muted small">{{
          current
            ? `最近采样 ${date(current.observed_from_ms)} — ${date(current.observed_to_ms)}`
            : "尚未取得完整快照，资金与持仓暂不可用。"
        }}</span
        ><span class="unit-label"
          >{{
            current?.native_currency ||
            data.account.confirmed_currency ||
            "单位待确认"
          }}
          ·
          {{ data.account.unit_confirmed ? "单位已确认" : "未确认换算" }}</span
        >
      </div>
      <div
        v-if="
          data.account.last_run?.error_code &&
          data.account.last_run.state !== 'succeeded'
        "
        class="notice notice-warning"
      >
        <AlertTriangle :size="17" />
        <p>
          {{ failureLabel(data.account.last_run.error_code)
          }}{{
            data.account.last_run.raw_status !== null
              ? `（服务端原码 ${data.account.last_run.raw_status}，不据此推断密码错误）`
              : ""
          }}。{{ current ? "继续显示上次完整快照。" : "等待首次完整快照。" }}
        </p>
      </div>
      <section class="summary-grid detail-metrics">
        <article class="summary-card">
          <div class="card-label">
            账户净值 <span>{{ current?.native_currency || "原币" }}</span>
          </div>
          <div class="metric-value num">{{ money(current?.equity) }}</div>
          <p class="metric-note">含信用金，保留 SDK 口径</p>
        </article>
        <article class="summary-card">
          <div class="card-label">余额 / 信用金</div>
          <div class="metric-value num">{{ money(current?.balance) }}</div>
          <p class="metric-note num">信用金 {{ money(current?.credit) }}</p>
        </article>
        <article class="summary-card">
          <div class="card-label">浮动净盈亏</div>
          <div
            class="metric-value num"
            :class="{
              positive: current && positive(current.floating_net_profit),
              negative: current && !positive(current.floating_net_profit),
            }"
          >
            {{ money(current?.floating_net_profit) }}
          </div>
          <p class="metric-note">当前买卖持仓，含已报告费用</p>
        </article>
        <article class="summary-card">
          <div class="card-label">已用保证金</div>
          <div class="metric-value num">
            {{ current?.margin_available ? money(current.margin) : "—" }}
          </div>
          <p class="metric-note">
            {{
              current?.margin_available
                ? `杠杆 1:${current.leverage}`
                : "当前协议数据不足时不估造"
            }}
          </p>
        </article>
      </section>
      <section class="panel">
        <div class="panel-heading">
          <div class="tabs" aria-label="订单类型">
            <button
              :class="{ active: tab === 'positions' }"
              @click="tab = 'positions'"
            >
              当前持仓
              <span>{{
                data.positions.filter((p) => p.command <= 1).length
              }}</span></button
            ><button
              :class="{ active: tab === 'pending' }"
              @click="tab = 'pending'"
            >
              挂单
              <span>{{
                data.positions.filter((p) => p.command >= 2).length
              }}</span>
            </button>
          </div>
          <label class="sort-label"
            >排序<select v-model="sort" aria-label="订单排序">
              <option value="ticket">订单号</option>
              <option value="open_time_wall">服务器开仓时间</option>
            </select></label
          >
        </div>
        <PositionTable v-if="current" :positions="positions" />
        <p v-else class="empty-inline">
          尚无完整快照，不能将缺失持仓视为零持仓。
        </p>
        <div class="panel-footer">
          <span><Info :size="14" />开仓时间为服务器墙钟，时区尚未核验。</span
          ><span>金额按原币展示</span>
        </div>
      </section>
      <section class="panel snapshot-panel">
        <div class="panel-heading">
          <div>
            <h2>最近成功快照</h2>
            <p class="muted small">
              最近 10 次完整采集。失败的采集不会写成零值快照。
            </p>
          </div>
          <span class="badge badge-neutral">快照明细</span>
        </div>
        <p v-if="snapshotsError" class="error-message">
          {{ snapshotsError.message }}
        </p>
        <div class="table-scroll">
          <table class="data-table">
            <thead>
              <tr>
                <th>采样开始</th>
                <th>采样完成</th>
                <th class="text-right">余额</th>
                <th class="text-right">净值</th>
                <th class="text-right">浮动净盈亏</th>
                <th>单位</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="s in snapshots" :key="s.id">
                <td class="num muted small">{{ date(s.observed_from_ms) }}</td>
                <td class="num small">{{ date(s.observed_to_ms) }}</td>
                <td class="num text-right">{{ money(s.balance) }}</td>
                <td class="num text-right">{{ money(s.equity) }}</td>
                <td
                  class="num text-right"
                  :class="
                    positive(s.floating_net_profit) ? 'positive' : 'negative'
                  "
                >
                  {{ money(s.floating_net_profit) }}
                </td>
                <td>{{ s.native_currency }}</td>
              </tr>
            </tbody>
          </table>
          <p v-if="snapshots && !snapshots.length" class="empty-inline">
            首次完整采集后开始积累快照。
          </p>
        </div>
      </section>
      <Modal
        v-model:open="open"
        title="账户配置"
        description="修改配置会取消旧任务，发布前还会再次校验配置版本。"
      >
        <AccountForm
          :account="data.account"
          @saved="open = false"
          @cancel="open = false"
        />
      </Modal>
    </template>
  </div>
</template>
