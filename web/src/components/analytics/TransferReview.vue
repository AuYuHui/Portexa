<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { api } from "../../api";
import { refreshAccounts } from "../../state";
import { signedMoney } from "../../format";
import { localizeMessage } from "../../i18n";
import type { PerformanceEvent } from "../../types/analytics";
import { trendDate } from "../charts/trendFormat";
const props = defineProps<{
  events: PerformanceEvent[];
  names: Record<string, string>;
}>();
const { t } = useI18n();
const outKey = ref("");
const inKey = ref("");
const reason = ref("");
const busy = ref(false);
const error = ref("");
const out = computed(() =>
  props.events.filter((event) => event.kind === "transfer_out"),
);
const incoming = computed(() =>
  props.events.filter((event) => event.kind === "transfer_in"),
);
const key = (event: PerformanceEvent) =>
  `${event.account_id}:${event.ticket}:${event.revision}`;
async function save() {
  const outgoing = out.value.find((event) => key(event) === outKey.value);
  const entry = incoming.value.find((event) => key(event) === inKey.value);
  if (!outgoing || !entry) return;
  busy.value = true;
  error.value = "";
  try {
    await api("/analytics/transfers", "POST", {
      out_account_id: outgoing.account_id,
      out_ticket: outgoing.ticket,
      out_revision: outgoing.revision,
      in_account_id: entry.account_id,
      in_ticket: entry.ticket,
      in_revision: entry.revision,
      reason: reason.value,
    });
    outKey.value = "";
    inKey.value = "";
    reason.value = "";
    await refreshAccounts();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <form class="performance-transfer-review form-stack" @submit.prevent="save">
    <h3>{{ t("analytics.transferReview") }}</h3>
    <p class="muted small">{{ t("analytics.transferHint") }}</p>
    <div class="performance-transfer-fields">
      <div class="field">
        <label for="analytics-transfer-out">{{
          t("format.internalTransferOut")
        }}</label
        ><select id="analytics-transfer-out" v-model="outKey" required>
          <option value="">{{ t("analytics.selectEntry") }}</option>
          <option v-for="event in out" :key="key(event)" :value="key(event)">
            {{ names[event.account_id] || event.account_id.slice(0, 8) }} · #{{
              event.ticket
            }}
            · {{ signedMoney(event.amount) }} USD ·
            {{ trendDate(event.time_ms) }}
          </option>
        </select>
      </div>
      <div class="field">
        <label for="analytics-transfer-in">{{
          t("format.internalTransferIn")
        }}</label
        ><select id="analytics-transfer-in" v-model="inKey" required>
          <option value="">{{ t("analytics.selectEntry") }}</option>
          <option
            v-for="event in incoming"
            :key="key(event)"
            :value="key(event)"
          >
            {{ names[event.account_id] || event.account_id.slice(0, 8) }} · #{{
              event.ticket
            }}
            · {{ signedMoney(event.amount) }} USD ·
            {{ trendDate(event.time_ms) }}
          </option>
        </select>
      </div>
    </div>
    <div class="field">
      <label for="analytics-transfer-reason">{{
        t("analytics.reviewEvidence")
      }}</label
      ><input
        id="analytics-transfer-reason"
        v-model="reason"
        required
        maxlength="500"
      />
    </div>
    <p v-if="error" role="alert" class="notice notice-error">
      {{ localizeMessage(error) }}
    </p>
    <div>
      <button
        class="btn btn-primary"
        :disabled="busy || !outKey || !inKey || !reason.trim()"
      >
        {{ t("analytics.confirmTransfer") }}
      </button>
    </div>
  </form>
</template>
