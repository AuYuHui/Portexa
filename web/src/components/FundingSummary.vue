<script setup lang="ts">
import { computed, ref, shallowRef, watch } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { RefreshCw } from "@lucide/vue";
import type { Account, FundEntry, FundsPage } from "../types";
import { api } from "../api";
import { refreshAccounts } from "../state";
import {
  money,
  signedMoney,
  date,
  wallTime,
  positive,
  fundingLabel,
  fundCategories,
  failureLabel,
} from "../format";
import Modal from "./Modal.vue";
import FundClassificationForm from "./FundClassificationForm.vue";

const props = defineProps<{ account: Account }>();
const funding = computed(() => props.account.funding!);
const id = computed(() => props.account.id);
const cursor = ref("");
const previous = ref<string[]>([]);
const unclassified = ref(false);
const { data, error, isPending, isFetching, refetch } = useQuery({
  queryKey: ["funds", id, cursor, unclassified],
  queryFn: () =>
    api<FundsPage>(
      `/accounts/${id.value}/funds?cursor=${encodeURIComponent(cursor.value)}&unclassified=${unclassified.value ? 1 : 0}`,
    ),
  refetchInterval: 10_000,
});
watch([id, unclassified], () => {
  cursor.value = "";
  previous.value = [];
});
const selected = shallowRef<FundEntry | null>(null);
const open = ref(false);
const busy = ref(false);
const message = ref("");
const actionError = ref("");
function edit(entry: FundEntry) {
  selected.value = entry;
  open.value = true;
}
function next() {
  if (!data.value?.next_cursor) return;
  previous.value.push(cursor.value);
  cursor.value = data.value.next_cursor;
}
function back() {
  cursor.value = previous.value.pop() || "";
}
function saved() {
  open.value = false;
  message.value = "资金分类已保存，累计金额已更新。";
}
async function sync() {
  busy.value = true;
  actionError.value = "";
  message.value = "";
  try {
    await api(`/accounts/${id.value}/history/sync`, "POST");
    message.value = "历史回填与重查已加入队列，资金快照优先采集。";
    await refreshAccounts();
  } catch (e) {
    actionError.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <section class="panel funding-panel" aria-label="累计出入金与资金流水">
    <div class="panel-heading">
      <div>
        <h2>累计出入金</h2>
        <p class="muted small">
          自动识别资金流水，按原币累计外部出入金
          <span
            class="badge"
            :class="
              funding.coverage_complete && !funding.stale
                ? 'badge-good'
                : 'badge-warning'
            "
            >{{ fundingLabel(funding.state)
            }}{{ funding.stale ? " · 过期" : "" }}</span
          >
        </p>
      </div>
      <button
        class="btn btn-small"
        :disabled="
          busy ||
          !account.sync_enabled ||
          funding.state === 'time_zone_unconfirmed'
        "
        @click="sync"
      >
        <RefreshCw :size="14" :class="{ spin: busy }" />回填 / 重查历史
      </button>
    </div>
    <div class="funding-metrics">
      <div>
        <div class="card-label">
          累计入金
          <span>{{
            funding.native_currency ||
            account.snapshot?.native_currency ||
            "原币"
          }}</span>
        </div>
        <div class="metric-value num" data-fund-total="deposits">
          {{ money(funding.deposits) }}
        </div>
        <p class="metric-note">自动识别外部入金，支持人工校正</p>
      </div>
      <div>
        <div class="card-label">
          累计出金
          <span>{{
            funding.native_currency ||
            account.snapshot?.native_currency ||
            "原币"
          }}</span>
        </div>
        <div class="metric-value num" data-fund-total="withdrawals">
          {{ money(funding.withdrawals) }}
        </div>
        <p class="metric-note">自动识别外部出金，展示绝对值</p>
      </div>
    </div>
    <p class="daily-note muted small">
      自动识别 {{ funding.automatic_count ?? 0 }} 笔 · 人工校正
      {{ funding.manual_count ?? funding.confirmed_count }} 笔 · 待识别
      {{ funding.unclassified_count }} 笔。
      <template v-if="!funding.coverage_complete"
        >历史尚未完整回填，当前金额为已识别部分。</template
      >
      <template v-if="funding.blocked_windows"
        >{{ funding.blocked_windows }} 个区间存在采集缺口。</template
      >
      根据流水备注识别入金、出金、返佣等；无法确定的单独保留。返佣、费用、内部转账及信用金不计入外部出入金。
    </p>
    <p
      v-if="funding.last_run?.error_code"
      class="daily-note warning-text small"
    >
      {{
        failureLabel(funding.last_run.error_code) || "历史暂未采集完成"
      }}，保留已取得的历史和确认金额。
    </p>
    <div class="funding-toolbar">
      <h3>资金流水</h3>
      <label class="checkbox-line"
        ><input v-model="unclassified" type="checkbox" />仅看待识别</label
      >
    </div>
    <p v-if="error" role="alert" class="daily-note negative">
      {{ error.message }}
      <button class="text-button" @click="refetch()">重试</button>
    </p>
    <p v-if="actionError" role="alert" class="daily-note negative">
      {{ actionError }}
    </p>
    <p v-if="message" role="status" class="daily-note positive">
      {{ message }}
    </p>
    <div class="table-scroll">
      <table class="data-table funds-table">
        <thead>
          <tr>
            <th>流水单号</th>
            <th>服务器时间</th>
            <th class="text-right">金额（原币）</th>
            <th>分类</th>
            <th>备注 / 识别依据</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="entry in data?.items || []" :key="entry.ticket">
            <td class="num">#{{ entry.ticket }}</td>
            <td class="small muted">{{ wallTime(entry.event_time_wall) }}</td>
            <td
              class="text-right num"
              :class="positive(entry.net_profit) ? 'positive' : 'negative'"
            >
              {{ signedMoney(entry.net_profit) }} {{ entry.native_currency }}
            </td>
            <td>
              <span
                class="badge"
                :class="
                  entry.command === 6 &&
                  (!entry.classification ||
                    entry.needs_review ||
                    entry.classification.category === 'unclassified')
                    ? 'badge-warning'
                    : 'badge-neutral'
                "
                >{{
                  entry.command === 7
                    ? "信用金（另计）"
                    : entry.needs_review
                      ? "待重新确认"
                      : fundCategories[
                          entry.classification?.category || "unclassified"
                        ]
                }}</span
              >
              <small
                v-if="entry.classification && !entry.needs_review"
                class="cell-note muted"
                >{{
                  entry.classification.source === "automatic"
                    ? "自动识别"
                    : "人工校正"
                }}</small
              >
            </td>
            <td class="fund-reason small muted">
              {{ entry.comment || "无备注" }}
              <small v-if="entry.classification" class="cell-note">{{
                entry.classification.reason
              }}</small>
            </td>
            <td>
              <button
                v-if="entry.command === 6"
                class="text-button"
                :aria-label="`分类流水 ${entry.ticket}`"
                @click="edit(entry)"
              >
                {{ entry.classification ? "校正" : "补充识别" }}</button
              ><span v-else class="muted small">单独记录</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="!error && !data?.items.length" class="empty-inline">
      {{
        isPending
          ? "正在加载资金流水…"
          : unclassified
            ? "当前没有待识别流水。"
            : funding.deposits == null
              ? "等待服务器历史，尚无可展示的资金流水。"
              : "已回填区间内没有资金流水。"
      }}
    </p>
    <div class="panel-footer">
      <span>最近历史采样 {{ date(funding.last_sync_at_ms) }}</span>
      <div class="funding-pagination">
        <button
          class="btn btn-small"
          :disabled="!previous.length || isFetching"
          @click="back"
        >
          上一页</button
        ><button
          class="btn btn-small"
          :disabled="!data?.next_cursor || isFetching"
          @click="next"
        >
          下一页
        </button>
      </div>
    </div>
    <Modal
      v-model:open="open"
      title="校正资金分类"
      description="仅需校正识别有误或无法识别的流水，修改会保留记录。"
      ><FundClassificationForm
        v-if="selected"
        :key="`${selected.ticket}-${selected.revision}`"
        :account-id="account.id"
        :entry="selected"
        @saved="saved"
    /></Modal>
  </section>
</template>
