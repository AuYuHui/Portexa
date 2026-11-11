<script setup lang="ts">
import { useI18n } from "vue-i18n";
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

const { t } = useI18n();
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
    header: () => t('accountTable.account'),
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
    header: () => t('accountTable.nativeCurrencyUnit'),
    cell: ({ row: { original: a } }) =>
      h("span", {}, [
        h(
          "strong",
          { class: "currency-label" },
          a.snapshot?.native_currency || a.confirmed_currency || t('accountTable.unconfirmed'),
        ),
        !a.unit_confirmed
          ? h("small", { class: "cell-note warning-text" }, t('account.conversionUnconfirmed'))
          : null,
      ]),
  },
  {
    id: "balance",
    header: () => t('account.balance'),
    meta: { numeric: true },
    cell: ({ row: { original: a } }) => money(a.snapshot?.balance),
  },
  {
    id: "equity",
    header: () => t('account.accountEquity'),
    meta: { numeric: true },
    cell: ({ row: { original: a } }) =>
      h("strong", {}, money(a.snapshot?.equity)),
  },
  {
    id: "profit",
    header: () => t('account.floatingNetPL'),
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
    header: () => t('accountTable.todaySProfit'),
    cell: ({ row: { original: a } }) => dailyCell(a, "net_profit"),
  },
  {
    id: "daily_balance",
    header: () => t('accountTable.todaySBalanceChange'),
    cell: ({ row: { original: a } }) => dailyCell(a, "balance_change"),
  },
  {
    id: "daily_trades",
    header: () => t('accountTable.todaySClosedTradesWinRate'),
    cell: ({ row: { original: a } }) => {
      const report = a.daily?.report;
      return h(
        "span",
        { title: t('accountTable.byServerDateNetWinningTradesAllClosed') },
        [
          h(
            "strong",
            {},
            report
              ? t('accountTable.trades', { value1: report.closed_trades, value2: percent(report.win_rate) })
              : "—",
          ),
          h(
            "small",
            { class: "cell-note muted" },
            report?.winning_trades != null
              ? t('accountTable.wonLostBreakeven', { value1: report.winning_trades, value2: report.losing_trades, value3: report.breakeven_trades })
              : a.daily
                ? dailyLabel(a.daily.state, a.daily.stale)
                : t('accountTable.waitingForHistory'),
          ),
        ],
      );
    },
  },
  {
    id: "daily_gains",
    header: () => t('accountTable.todaySGainsLosses'),
    cell: ({ row: { original: a } }) =>
      h(
        "span",
        { title: t('accountTable.netGainsAndLossesInNativeCurrencyIncluding') },
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
    header: () => t('account.marginLevel'),
    cell: ({ row: { original: a } }) =>
      h(
        "span",
        { title: t('account.accountEquityUsedMargin') },
        marginLevel(a.snapshot),
      ),
  },
  {
    id: "funding",
    header: () => t('accountTable.totalDepositsWithdrawals'),
    cell: ({ row: { original: a } }) =>
      h(
        "span",
        { title: t('accountTable.externalDepositsAndWithdrawalsDetectedFromCommentsWith') },
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
              ? `${a.funding.coverage_complete && a.funding.state === "ready" ? t('accountTable.automaticallyCalculated') : fundingLabel(a.funding.state)}${a.funding.unclassified_count ? t('accountTable.unclassified', { value1: a.funding.unclassified_count }) : ""}${a.funding.stale ? t('accountTable.stale') : ""}`
              : t('accountTable.waitingForHistory'),
          ),
        ],
      ),
  },
  {
    id: "state",
    header: () => t('accountTable.syncStatus'),
    cell: ({ row: { original: a } }) =>
      h(StatusBadge, { state: a.sync_state, stale: a.stale }),
  },
  {
    id: "time",
    header: () => t('accountTable.latestSample'),
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
          "aria-label": t('accountTable.view', { value1: a.label }),
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
        <p class="empty-table-title">{{ t('accountTable.noConnectedAccountsYet') }}</p>
        <p class="muted small">
          {{ t('accountTable.registerAndVerifyAServerThenConnectRead') }}
        </p>
        <RouterLink
          to="/settings?add=account"
          class="btn btn-primary btn-small"
        >
          <Plus :size="15" />{{ t('accountTable.connectYourFirstAccount') }}
        </RouterLink>
      </template>
      <template v-else>
        <p class="empty-inline">
          {{ t('accountTable.noMatchingAccountsTryAnotherNameLoginOr') }}
        </p>
      </template>
    </div>
  </div>
</template>
