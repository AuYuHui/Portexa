<script setup lang="ts">
import { computed, h } from "vue";
import { RouterLink } from "vue-router";
import {
  FlexRender,
  getCoreRowModel,
  useVueTable,
  type ColumnDef,
} from "@tanstack/vue-table";
import { ArrowUpRight, Wallet, Plus } from "@lucide/vue";
import type { Account } from "../types";
import {
  money,
  signedMoney,
  date,
  positive,
  dailyLabel,
  percent,
  marginLevel,
  fundingLabel,
} from "../format";
import StatusBadge from "./StatusBadge.vue";
const props = defineProps<{ accounts: Account[]; search: string }>();
const filtered = computed(() =>
  props.accounts.filter((a) =>
    `${a.label} ${a.login} ${a.broker_name} ${a.server_name}`
      .toLowerCase()
      .includes(props.search.toLowerCase()),
  ),
);
function dailyCell(account: Account, field: "net_profit" | "balance_change") {
  const daily = account.daily;
  if (!daily) return "—";
  const value = daily.report?.[field];
  const label = dailyLabel(daily.state, daily.stale);
  return h("span", {}, [
    h(
      "strong",
      { class: value ? (positive(value) ? "positive" : "negative") : "" },
      signedMoney(value),
    ),
    h(
      "small",
      { class: "cell-note muted", title: `${daily.date} ${daily.time_zone}` },
      daily.report
        ? `${daily.date}${daily.state !== "succeeded" || daily.stale ? ` · ${label}` : ""}`
        : label,
    ),
  ]);
}
const columns: ColumnDef<Account>[] = [
  {
    id: "account",
    header: "账户",
    cell: ({ row: { original: a } }) =>
      h(RouterLink, { to: `/accounts/${a.id}`, class: "account-cell" }, () => [
        h("span", { class: "account-avatar" }, a.label.slice(0, 1)),
        h("span", {}, [
          h("strong", {}, a.label),
          h("small", {}, `${a.login} · ${a.broker_name}`),
        ]),
      ]),
  },
  {
    id: "currency",
    header: "原币 / 单位",
    cell: ({ row: { original: a } }) =>
      h("span", {}, [
        h(
          "strong",
          { class: "currency-label" },
          a.snapshot?.native_currency || a.confirmed_currency || "待确认",
        ),
        !a.unit_confirmed
          ? h("small", { class: "cell-note warning-text" }, "未确认换算")
          : null,
      ]),
  },
  {
    id: "balance",
    header: "余额",
    meta: { numeric: true },
    cell: ({ row: { original: a } }) => money(a.snapshot?.balance),
  },
  {
    id: "equity",
    header: "账户净值",
    meta: { numeric: true },
    cell: ({ row: { original: a } }) =>
      h("strong", {}, money(a.snapshot?.equity)),
  },
  {
    id: "profit",
    header: "浮动净盈亏",
    meta: { numeric: true },
    cell: ({ row: { original: a } }) =>
      h(
        "span",
        {
          class: a.snapshot
            ? positive(a.snapshot.floating_net_profit)
              ? "positive"
              : "negative"
            : "",
        },
        signedMoney(a.snapshot?.floating_net_profit),
      ),
  },
  {
    id: "daily_profit",
    header: "当日利润",
    cell: ({ row: { original: a } }) => dailyCell(a, "net_profit"),
  },
  {
    id: "daily_balance",
    header: "当日结余",
    cell: ({ row: { original: a } }) => dailyCell(a, "balance_change"),
  },
  {
    id: "daily_trades",
    header: "当日平仓 / 胜率",
    cell: ({ row: { original: a } }) => {
      const report = a.daily?.report;
      return h(
        "span",
        { title: "按服务器日期统计；净盈利笔数 ÷ 全部平仓笔数，含持平单" },
        [
          h(
            "strong",
            {},
            report
              ? `${report.closed_trades} 笔 · ${percent(report.win_rate)}`
              : "—",
          ),
          h(
            "small",
            { class: "cell-note muted" },
            report?.winning_trades != null
              ? `盈 ${report.winning_trades} / 亏 ${report.losing_trades} / 平 ${report.breakeven_trades}`
              : a.daily
                ? dailyLabel(a.daily.state, a.daily.stale)
                : "等待历史采集",
          ),
        ],
      );
    },
  },
  {
    id: "daily_gains",
    header: "当日盈利 / 亏损",
    cell: ({ row: { original: a } }) =>
      h(
        "span",
        { title: "原币净盈亏分别汇总，亏损展示绝对值，均含已报告费用" },
        [
          h(
            "strong",
            { class: "positive" },
            money(a.daily?.report?.gross_profit),
          ),
          h(
            "small",
            { class: "cell-note negative" },
            money(a.daily?.report?.gross_loss),
          ),
        ],
      ),
  },
  {
    id: "margin_level",
    header: "保证金水平",
    cell: ({ row: { original: a } }) =>
      h(
        "span",
        { title: "账户净值 ÷ 已用保证金 × 100%" },
        marginLevel(a.snapshot),
      ),
  },
  {
    id: "funding",
    header: "累计入金 / 出金",
    cell: ({ row: { original: a } }) =>
      h(
        "span",
        { title: "根据备注自动识别外部出入金，支持人工校正；其他资金不计入" },
        [
          h(
            "strong",
            {},
            `${money(a.funding?.deposits)} / ${money(a.funding?.withdrawals)}`,
          ),
          h(
            "small",
            { class: "cell-note muted" },
            a.funding
              ? `${a.funding.coverage_complete && a.funding.state === "ready" ? "自动统计" : fundingLabel(a.funding.state)}${a.funding.unclassified_count ? ` · ${a.funding.unclassified_count} 笔待识别` : ""}${a.funding.stale ? " · 过期" : ""}`
              : "等待历史采集",
          ),
        ],
      ),
  },
  {
    id: "state",
    header: "同步状态",
    cell: ({ row: { original: a } }) =>
      h(StatusBadge, { state: a.sync_state, stale: a.stale }),
  },
  {
    id: "time",
    header: "最近采样",
    cell: ({ row: { original: a } }) =>
      h("span", { class: "small muted num" }, date(a.snapshot?.observed_to_ms)),
  },
  {
    id: "action",
    header: "",
    cell: ({ row: { original: a } }) =>
      h(
        RouterLink,
        {
          to: `/accounts/${a.id}`,
          class: "icon-button",
          "aria-label": `查看${a.label}`,
        },
        () => h(ArrowUpRight, { size: 17 }),
      ),
  },
];
const numericColumns = [
  "balance",
  "equity",
  "profit",
  "daily_profit",
  "daily_balance",
  "daily_trades",
  "daily_gains",
  "margin_level",
  "funding",
];
const table = useVueTable({
  get data() {
    return filtered.value;
  },
  columns,
  getCoreRowModel: getCoreRowModel(),
});
</script>
<template>
  <div class="table-scroll">
    <table class="data-table accounts-table">
      <thead>
        <tr v-for="group in table.getHeaderGroups()" :key="group.id">
          <th
            v-for="header in group.headers"
            :key="header.id"
            :class="{
              'text-right': numericColumns.includes(header.column.id),
            }"
          >
            <FlexRender
              :render="header.column.columnDef.header"
              :props="header.getContext()"
            />
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in table.getRowModel().rows" :key="row.id">
          <td
            v-for="cell in row.getVisibleCells()"
            :key="cell.id"
            :class="{
              'text-right num': numericColumns.includes(cell.column.id),
            }"
          >
            <FlexRender
              :render="cell.column.columnDef.cell"
              :props="cell.getContext()"
            />
          </td>
        </tr>
      </tbody>
    </table>
    <div v-if="!filtered.length" class="empty-table-state">
      <template v-if="!accounts.length">
        <span class="icon-box subtle"><Wallet :size="24" /></span>
        <p class="empty-table-title">暂无已接入账户</p>
        <p class="muted small">
          完成服务器登记与核验后，在此接入只读投资者凭据并开始数据聚合。
        </p>
        <RouterLink
          to="/settings?add=account"
          class="btn btn-primary btn-small"
        >
          <Plus :size="15" />接入首个账户
        </RouterLink>
      </template>
      <template v-else>
        <p class="empty-inline">
          未找到匹配的账户，请尝试搜索其他名称、账号或经纪商。
        </p>
      </template>
    </div>
  </div>
</template>
