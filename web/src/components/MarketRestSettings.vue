<script setup lang="ts">
import { useQuery } from "@tanstack/vue-query";
import { useI18n } from "vue-i18n";
import { api } from "../api";
import type { Account } from "../types";

const { t } = useI18n();
defineProps<{ disabled: boolean }>();
const template = defineModel<string>("template", { required: true });
const excluded = defineModel<string[]>("excluded", { required: true });
const { data: accounts } = useQuery({
  queryKey: ["accounts-for-collection"],
  queryFn: ({ signal }) => api<Account[]>("/accounts", "GET", undefined, signal),
});
</script>

<template>
  <fieldset class="market-rest-settings" :disabled="disabled">
    <legend>{{ t("collectionSettings.marketRest") }}</legend>
    <label class="field">
      {{ t("collectionSettings.marketRestTemplate") }}
      <select v-model="template" aria-describedby="market-rest-description">
        <option value="off">{{ t("collectionSettings.marketRestOff") }}</option>
        <option value="fx_metals_weekend_v1">{{ t("collectionSettings.marketRestWeekend") }}</option>
      </select>
    </label>
    <p id="market-rest-description" class="muted small">
      {{ t("collectionSettings.marketRestWindow") }}
      {{ t("collectionSettings.marketRestIntervals") }}
    </p>
    <p class="muted small">{{ t("collectionSettings.marketRestSafety") }}</p>
    <details v-if="accounts?.length" class="market-rest-exceptions">
      <summary>{{ t("collectionSettings.marketRestExceptions") }}</summary>
      <label v-for="account in accounts" :key="account.id" class="market-rest-account">
        <input v-model="excluded" type="checkbox" :value="account.id" />
        <span>{{ account.label }}</span>
      </label>
    </details>
  </fieldset>
</template>

<style scoped>
.market-rest-settings { border: 0; border-top: 1px solid var(--color-border); margin: 20px 0 0; padding: 18px 0 0; min-width: 0; }
.market-rest-settings legend { font-size: 14px; font-weight: 600; padding-right: 12px; }
.market-rest-settings .field { max-width: 360px; }
.market-rest-settings p { margin: 12px 0 0; line-height: 1.7; }
.market-rest-exceptions { margin-top: 16px; max-height: 240px; overflow: auto; }
.market-rest-exceptions summary { cursor: pointer; font-size: 12px; color: var(--color-text-muted); }
.market-rest-account { display: flex; align-items: center; gap: 10px; padding: 8px 0; font-size: 13px; }
.market-rest-account input { width: 16px; height: 16px; }
</style>
