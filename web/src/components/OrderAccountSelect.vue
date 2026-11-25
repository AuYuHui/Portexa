<script setup lang="ts">
import { useI18n } from "vue-i18n";
import type { Account } from "../types";

defineProps<{ accounts: Account[]; accountId: string }>();
defineEmits<{ select: [accountId: string] }>();
const { t } = useI18n();
</script>

<template>
  <label class="order-account-select">
    {{ t("analytics.account") }}
    <select :value="accountId" @change="$emit('select', ($event.target as HTMLSelectElement).value)">
      <option v-if="!accountId" disabled value="">{{ t("orders.selectAccount") }}</option>
      <option v-for="account in accounts" :key="account.id" :value="account.id">
        {{ account.label }} · {{ account.login }} · {{ account.protocol.toUpperCase() }}
      </option>
    </select>
  </label>
</template>

<style scoped>
.order-account-select {
  display: inline-grid;
  gap: 4px;
  max-width: 100%;
  color: var(--color-muted);
  font-size: 12px;
}
.order-account-select select {
  min-width: 0;
  max-width: 100%;
  border: 1px solid var(--color-line);
  border-radius: 7px;
  background: var(--color-surface);
  color: var(--color-ink);
  padding: 8px 28px 8px 10px;
  font: inherit;
}
</style>
