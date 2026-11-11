<script setup lang="ts">
import { ref } from "vue";
import { AlertTriangle } from "@lucide/vue";
import type { Endpoint } from "../types";
import { api } from "../api";
import { refreshAccounts } from "../state";
const props = defineProps<{ endpoint: Endpoint }>();
const emit = defineEmits<{ saved: [] }>();
const reference = ref(props.endpoint.evidence_ref || "");
const digest = ref("");
const risk = ref(false);
const busy = ref(false);
const error = ref("");
async function hashFile(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0];
  if (!file) return;
  if (file.size > 10 * 1024 * 1024) {
    error.value = "核验文件请控制在 10 MB 以内。";
    return;
  }
  try {
    const hash = await crypto.subtle.digest(
      "SHA-256",
      await file.arrayBuffer(),
    );
    digest.value = Array.from(new Uint8Array(hash), (b) =>
      b.toString(16).padStart(2, "0"),
    ).join("");
  } catch {
    error.value = "无法计算摘要，请手动填写 SHA-256。";
  }
}
async function submit() {
  busy.value = true;
  error.value = "";
  try {
    await api(`/endpoints/${props.endpoint.id}/review`, "POST", {
      evidence_ref: reference.value,
      evidence_sha256: digest.value,
      accept_risk: risk.value,
    });
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
  <form class="form-stack" @submit.prevent="submit">
    <div class="endpoint-summary num">
      修订 {{ endpoint.revision }}
      <strong>{{ endpoint.ip_address }}:{{ endpoint.port }}</strong>
    </div>
    <label class="field"
      >独立核验依据<input
        v-model="reference"
        required
        maxlength="512"
        placeholder="例如：经纪商官方登记文件 / 本地证据引用"
        :readonly="endpoint.state === 'reviewed'"
      /><small
        >应通过经纪商官方渠道或已知可信配置独立核验，不能仅凭 MT4
        返回的名称。</small
      ></label
    >
    <label class="field"
      >证据 SHA-256<input
        v-model="digest"
        required
        pattern="[a-fA-F0-9]{64}"
        minlength="64"
        maxlength="64"
        placeholder="64 位十六进制摘要"
        class="mono"
    /></label>
    <label class="field"
      >或在本机计算证据文件摘要<input type="file" @change="hashFile" /><small
        >文件只在浏览器本机计算摘要，不上传文件内容。已核验修订需提供原始证据摘要。</small
      ></label
    >
    <div class="notice notice-warning risk-notice">
      <AlertTriangle :size="20" />
      <p>
        冻结 SDK
        会在服务端身份确认前发送密码派生材料。固定接入点与独立核验只能降低误连风险，无法提供密码学服务端身份认证，也无法消除网络路径劫持。
      </p>
    </div>
    <label class="checkbox-row"
      ><input v-model="risk" required type="checkbox" /><span
        >我已独立核验此 IP、端口和服务器，并接受上述剩余风险。<small
          >仅对当前修订有效；修改地址或核验来源须重新核验。</small
        ></span
      ></label
    >
    <p v-if="error" role="alert" class="error-message">{{ error }}</p>
    <button class="btn btn-primary full-width" :disabled="busy || !risk">
      {{ busy ? "正在保存" : "保存核验与风险确认" }}
    </button>
  </form>
</template>
