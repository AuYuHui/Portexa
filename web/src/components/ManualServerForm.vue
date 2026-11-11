<script setup lang="ts">
import { ref } from "vue";
import { LoaderCircle } from "@lucide/vue";
import { api } from "../api";
import { refreshAccounts } from "../state";
import type { Server } from "../types";
const props = defineProps<{ server?: Server }>();
const emit = defineEmits<{ cancel: []; saved: [] }>();
const broker = ref("");
const name = ref("");
const address = ref("");
const port = ref(443);
const zone = ref("");
const busy = ref(false);
const error = ref("");
async function submit() {
  busy.value = true;
  error.value = "";
  try {
    if (props.server) {
      await api(`/servers/${props.server.id}/endpoints`, "POST", {
        ip_address: address.value,
        port: port.value,
      });
    } else {
      await api("/servers", "POST", {
        broker_name: broker.value,
        server_name: name.value,
        ip_address: address.value,
        port: port.value,
        time_zone: zone.value,
      });
    }
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
    <template v-if="!server"
      ><label class="field"
        >经纪商<input
          v-model="broker"
          required
          maxlength="128"
          placeholder="经纪商名称" /></label
      ><label class="field"
        >准确的 MT4 服务器名称<input
          v-model="name"
          required
          maxlength="128"
          placeholder="例如：Synthetic-Live01"
        /><small>服务器名称用于协议配置，本身不能证明服务端身份。</small></label
      ></template
    >
    <p v-else class="notice notice-warning">
      创建新修订会停用旧地址，并暂停该服务器所有账户。新修订完成核验后，需在账户配置中明确重新绑定。
    </p>
    <div class="form-grid">
      <label class="field"
        >固定 IP 地址<input
          v-model="address"
          required
          placeholder="独立核验的公开 IP" /></label
      ><label class="field"
        >端口<input
          v-model.number="port"
          required
          type="number"
          min="1"
          max="65535"
      /></label>
    </div>
    <label v-if="!server" class="field"
      >服务器时区（可选）<input
        v-model="zone"
        placeholder="例如：Europe/Helsinki"
      /><small
        >仅登记线索，未核验前仍按服务器墙钟展示，不用于日报或月报。</small
      ></label
    >
    <p class="muted small">
      保存只登记配置，不发送账户凭据。下一步须登记独立核验依据并确认该修订的剩余风险。
    </p>
    <p v-if="error" role="alert" class="error-message">{{ error }}</p>
    <div class="dialog-actions">
      <button
        type="button"
        class="btn"
        :disabled="busy"
        @click="emit('cancel')"
      >
        取消
      </button>
      <button type="submit" class="btn btn-primary" :disabled="busy">
        <LoaderCircle v-if="busy" class="spin" :size="16" />
        {{
          busy
            ? "正在登记…"
            : server
              ? "建立新修订并暂停账户"
              : "登记服务器，继续核验"
        }}
      </button>
    </div>
  </form>
</template>
