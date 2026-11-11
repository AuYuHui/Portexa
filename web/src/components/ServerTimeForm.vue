<script setup lang="ts">
import { localizeMessage } from "../i18n";
import { useI18n } from "vue-i18n";
import { computed, ref } from "vue";
import { Clock, LoaderCircle } from "@lucide/vue";
import { api } from "../api";
import { refreshAccounts } from "../state";
import { date } from "../format";
import type { Server } from "../types";

const { t } = useI18n();

const props = defineProps<{ server: Server }>();
const emit = defineEmits<{ cancel: []; saved: [] }>();
const automatic = computed(() => props.server.automatic_time_zone);
const zone = ref(props.server.time_mode === "manual" || !props.server.time_mode ? props.server.time_zone || "" : "");
const busy = ref(false);
const error = ref("");

async function save(mode: "automatic" | "manual") {
  busy.value = true;
  error.value = "";
  try {
    const previous = { previous_ruleset_id: props.server.time_ruleset_id };
    const body = automatic.value
      ? { ...previous, mode, time_zone: mode === "manual" ? zone.value : undefined }
      : { ...previous, time_zone: zone.value, evidence_ref: "用户通过服务器时间设置手动校正", confirm: true };
    await api(`/servers/${props.server.id}/time-zone`, "PUT", body);
    await refreshAccounts();
    emit("saved");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="form-stack">
    <p class="muted small">{{ server.broker_name }} / {{ server.server_name }}</p>
    <p v-if="server.time_mode === 'manual'" class="muted small">{{ t('serverTimeForm.currentManualZone', { zone: server.time_zone }) }}</p>
    <div v-if="automatic?.available" class="server-time-result">
      <span class="icon-box subtle"><Clock :size="20" /></span>
      <div>
        <strong>{{ automatic.source === "events" ? t('serverTimeForm.tradeTimeCalibrated') : t('settings.automaticDetection') }} · {{ automatic.time_zone }}</strong>
        <p class="muted small">{{ localizeMessage(automatic.description) }}</p>
        <p v-if="automatic.source === 'events'" class="muted small">
          {{ t('serverTimeForm.verifiedTrades', { trades: automatic.evidence_count, rounds: automatic.event_window_count }) }}
          <br />{{ t('serverTimeForm.lastCalibrated') }} {{ date(automatic.observed_at_ms) }}
        </p>
        <p v-else-if="automatic.required_events" class="muted small">
          {{ t('serverTimeForm.calibrationProgress', { count: Math.min(automatic.evidence_count || 0, automatic.required_events), required: automatic.required_events }) }}
        </p>
        <a
          v-if="automatic.source_url"
          :href="automatic.source_url" target="_blank" rel="noopener noreferrer"
          class="text-button small"
        >{{ t('serverTimeForm.officialTimeDocumentation') }}</a>
      </div>
    </div>
    <div v-else class="server-time-result">
      <span class="icon-box subtle"><Clock :size="20" /></span>
      <div>
        <strong>{{ t('serverTimeForm.calibratingTradeTimeAutomatically') }}</strong>
        <p class="muted small">{{ localizeMessage(automatic?.description || t('serverTimeForm.waitingForNewlyClosedTradesToIdentifyServer')) }}</p>
        <p class="muted small">{{ t('serverTimeForm.verifiedProgress', { count: Math.min(automatic?.evidence_count || 0, automatic?.required_events || 3), required: automatic?.required_events || 3 }) }}</p>
        <p class="muted small">{{ t('serverTimeForm.roundProgress', { count: Math.min(automatic?.event_window_count || 0, 2) }) }}</p>
      </div>
    </div>
    <details class="server-time-advanced">
      <summary>{{ t('serverTimeForm.advancedManualCorrection') }}</summary>
      <form class="form-stack" @submit.prevent="save('manual')">
        <label class="field">
          {{ t('serverTimeForm.serverTimeZone') }}
          <input
            v-model="zone" required maxlength="64" list="server-time-zones"
            :placeholder="t('serverTimeForm.exampleUtcOrEuropeHelsinki')"
          />
          <small>{{ t('serverTimeForm.useBrokerProvidedRulesOnlyWhenAutomaticDetection') }}</small>
        </label>
        <datalist id="server-time-zones">
          <option value="Europe/Helsinki" />
          <option value="Europe/Athens" />
          <option value="Etc/GMT-2" />
          <option value="Etc/GMT-3" />
          <option value="UTC" />
        </datalist>
        <button type="submit" class="btn" :disabled="busy || !zone.trim()">
          {{ t('serverTimeForm.saveManualCorrection') }}
        </button>
      </form>
    </details>
    <p v-if="error" class="error-message" role="alert">{{ localizeMessage(error) }}</p>
    <div class="dialog-actions">
      <button type="button" class="btn" :disabled="busy" @click="emit('cancel')">{{ t('modal.close') }}</button>
      <button
        v-if="automatic && server.time_mode === 'manual'"
        type="button" class="btn btn-primary" :disabled="busy"
        @click="save('automatic')"
      >
        <LoaderCircle v-if="busy" :size="16" class="spin" />{{ t('serverTimeForm.useAutomaticDetection') }}
      </button>
    </div>
  </div>
</template>
