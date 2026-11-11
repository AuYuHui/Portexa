<script setup lang="ts">
import { computed, ref } from "vue";
import { Clock, LoaderCircle } from "@lucide/vue";
import { api } from "../api";
import { refreshAccounts } from "../state";
import { date } from "../format";
import type { Server } from "../types";

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
    <p v-if="server.time_mode === 'manual'" class="muted small">当前使用手动校正：{{ server.time_zone }}</p>
    <div v-if="automatic?.available" class="server-time-result">
      <span class="icon-box subtle"><Clock :size="20" /></span>
      <div>
        <strong>{{ automatic.source === "events" ? "交易时间已校准" : "自动识别" }} · {{ automatic.time_zone }}</strong>
        <p class="muted small">{{ automatic.description }}</p>
        <p v-if="automatic.source === 'events'" class="muted small">
          已核对 {{ automatic.evidence_count }} 笔新平仓 · 覆盖 {{ automatic.event_window_count }} 轮采集
          <br />最近校准 {{ date(automatic.observed_at_ms) }}
        </p>
        <p v-else-if="automatic.required_events" class="muted small">
          交易校准进度 {{ Math.min(automatic.evidence_count || 0, automatic.required_events) }} / {{ automatic.required_events }} 笔新平仓
        </p>
        <a
          v-if="automatic.source_url"
          :href="automatic.source_url" target="_blank" rel="noopener noreferrer"
          class="text-button small"
        >官方时间说明</a>
      </div>
    </div>
    <div v-else class="server-time-result">
      <span class="icon-box subtle"><Clock :size="20" /></span>
      <div>
        <strong>交易时间自动校准中</strong>
        <p class="muted small">{{ automatic?.description || "等待新平仓交易自动识别服务器时间。" }}</p>
        <p class="muted small">已核对 {{ Math.min(automatic?.evidence_count || 0, automatic?.required_events || 3) }} / {{ automatic?.required_events || 3 }} 笔新平仓</p>
        <p class="muted small">已覆盖 {{ Math.min(automatic?.event_window_count || 0, 2) }} / 2 轮采集</p>
      </div>
    </div>
    <details class="server-time-advanced">
      <summary>高级：手动校正</summary>
      <form class="form-stack" @submit.prevent="save('manual')">
        <label class="field">
          服务器时区
          <input
            v-model="zone" required maxlength="64" list="server-time-zones"
            placeholder="例如：UTC 或 Europe/Helsinki"
          />
          <small>仅在自动识别不适用时，按经纪商提供的规则校正。</small>
        </label>
        <datalist id="server-time-zones">
          <option value="Europe/Helsinki" />
          <option value="Europe/Athens" />
          <option value="Etc/GMT-2" />
          <option value="Etc/GMT-3" />
          <option value="UTC" />
        </datalist>
        <button type="submit" class="btn" :disabled="busy || !zone.trim()">
          保存手动校正
        </button>
      </form>
    </details>
    <p v-if="error" class="error-message" role="alert">{{ error }}</p>
    <div class="dialog-actions">
      <button type="button" class="btn" :disabled="busy" @click="emit('cancel')">关闭</button>
      <button
        v-if="automatic && server.time_mode === 'manual'"
        type="button" class="btn btn-primary" :disabled="busy"
        @click="save('automatic')"
      >
        <LoaderCircle v-if="busy" :size="16" class="spin" />使用自动识别
      </button>
    </div>
  </div>
</template>
