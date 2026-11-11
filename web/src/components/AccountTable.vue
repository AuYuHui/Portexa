<script setup lang="ts">
import { computed, h } from "vue";
import { RouterLink } from "vue-router";
import {
  FlexRender,
  getCoreRowModel,
  useVueTable,
  type ColumnDef,
} from "@tanstack/vue-table";
import { ArrowUpRight } from "@lucide/vue";
import type { Account } from "../types";
import { money, date, positive } from "../format";
import StatusBadge from "./StatusBadge.vue";
const props = defineProps<{ accounts: Account[]; search: string }>();
const filtered = computed(() =>
  props.accounts.filter((a) =>
    `${a.label} ${a.login} ${a.broker_name} ${a.server_name}`
      .toLowerCase()
      .includes(props.search.toLowerCase()),
  ),
);
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
        money(a.snapshot?.floating_net_profit),
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
              'text-right': ['balance', 'equity', 'profit'].includes(
                header.column.id,
              ),
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
              'text-right num': ['balance', 'equity', 'profit'].includes(
                cell.column.id,
              ),
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
    <p v-if="!filtered.length" class="empty-inline">
      {{
        accounts.length
          ? "没有匹配的账户，试试其他名称或账号。"
          : "还没有接入账户。完成服务器核验后，即可开始采集。"
      }}
    </p>
  </div>
</template>
