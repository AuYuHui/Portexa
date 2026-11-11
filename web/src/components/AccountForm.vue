<script setup lang="ts">
import { computed, ref } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { ShieldCheck, LoaderCircle } from "@lucide/vue";
import type { Account, Server } from "../types";
import { api } from "../api";
import { refreshAccounts } from "../state";
const props = defineProps<{ account?: Account }>();
const emit = defineEmits<{ saved: [] }>();
const { data: servers, error: loadError } = useQuery({
  queryKey: ["servers"],
  queryFn: () => api<Server[]>("/servers"),
});
const serverID = ref(props.account?.server_id || "");
const endpointID = ref(props.account?.endpoint_id || "");
const label = ref(props.account?.label || "");
const login = ref(props.account?.login?.toString() || "");
const password = ref("");
const currency = ref(props.account?.confirmed_currency || "USD");
const confirmed = ref(props.account?.configured_unit_confirmed || false);
const enabled = ref(props.account?.sync_enabled ?? true);
const busy = ref(false);
const error = ref("");
const server = computed(() =>
  servers.value?.find((s) => s.id === serverID.value),
);
const endpoints = computed(
  () =>
    server.value?.endpoints.filter(
      (e) => e.state === "reviewed" && e.risk_accepted_at_ms !== null,
    ) || [],
);
function selectServer() {
  endpointID.value = endpoints.value[0]?.id || "";
}
async function submit() {
  error.value = "";
  if (
    (!props.account || password.value) &&
    (new TextEncoder().encode(password.value).length > 16 || !password.value)
  ) {
    error.value = "投资者密码需要 1 至 16 个 UTF-8 字节。";
    return;
  }
  busy.value = true;
  try {
    const values = {
      label: label.value,
      endpoint_id: endpointID.value,
      confirmed_currency: currency.value.toUpperCase(),
      confirm_unit: confirmed.value,
      sync_enabled: enabled.value,
    };
    if (props.account) {
      await api(`/accounts/${props.account.id}`, "PATCH", {
        ...values,
        config_version: props.account.config_version,
        ...(password.value ? { password: password.value } : {}),
      });
    } else {
      await api("/accounts", "POST", {
        ...values,
        server_id: serverID.value,
        login: Number(login.value),
        password: password.value,
      });
    }
    password.value = "";
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
    <p v-if="loadError" role="alert" class="error-message">
      {{ loadError.message }}
    </p>
    <p v-if="servers && !servers.length" class="notice notice-warning">
      请先关闭此窗口，在“接入与设置”中建立服务器并完成独立核验。
    </p>
    <label class="field"
      >账户名称<input
        v-model="label"
        required
        maxlength="96"
        placeholder="例如：主账户 · 稳健策略"
    /></label>
    <div class="form-grid">
      <label class="field"
        >服务器<select
          v-model="serverID"
          aria-label="服务器"
          required
          :disabled="!!account"
          @change="selectServer"
        >
          <option disabled value="">选择已登记服务器</option>
          <option v-for="s in servers" :key="s.id" :value="s.id">
            {{ s.broker_name }} / {{ s.server_name }}
          </option>
        </select></label
      ><label class="field"
        >MT4 账号<input
          v-model="login"
          inputmode="numeric"
          pattern="[0-9]+"
          required
          :disabled="!!account"
          placeholder="交易登录号"
      /></label>
    </div>
    <label class="field"
      >固定接入点<select v-model="endpointID" required aria-label="固定接入点">
        <option disabled value="">请选择核验并接受风险的修订</option>
        <option
          v-if="
            account && !endpoints.some((e) => e.id === account?.endpoint_id)
          "
          disabled
          :value="account.endpoint_id"
        >
          旧接入点已停用，请重新绑定
        </option>
        <option v-for="e in endpoints" :key="e.id" :value="e.id">
          修订 {{ e.revision }} · {{ e.ip_address }}:{{ e.port }}
        </option></select
      ><small
        >仅允许完成核验和风险确认的修订。重新绑定将重加密凭据。</small
      ></label
    >
    <label class="field"
      >投资者密码 {{ account ? "（留空保留原密码）" : ""
      }}<input
        v-model="password"
        type="password"
        :required="!account"
        autocomplete="new-password"
        maxlength="16"
        placeholder="只使用只读投资者密码"
      /><small
        >与 Portexa 系统登录密码不同，按 SDK 限制最多 16 个 UTF-8 字节。</small
      ></label
    >
    <label class="field"
      >账户单位<input
        v-model="currency"
        required
        maxlength="12"
        placeholder="USD / USC / EUR…"
    /></label>
    <label class="checkbox-row"
      ><input v-model="confirmed" type="checkbox" /><span
        >我已核对该账户的币种与金额单位。<small
          >USD 按 1:1、USC 按 100:1
          折算美元；其他币种单列。与采集结果不符时停止换算。</small
        ></span
      ></label
    >
    <label class="checkbox-row"
      ><input v-model="enabled" type="checkbox" /><span
        >启用每 60 秒的快照采集<small
          >首次完整成功后展示资金与持仓；接入前的历史净值不补造。</small
        ></span
      ></label
    >
    <div class="notice notice-subtle">
      <ShieldCheck :size="17" />投资者凭据加密保存，接口不会返回密码或密文。
    </div>
    <p v-if="error" class="error-message" role="alert">{{ error }}</p>
    <button
      class="btn btn-primary full-width"
      :disabled="
        busy || !endpointID || !endpoints.some((e) => e.id === endpointID)
      "
    >
      <LoaderCircle v-if="busy" class="spin" :size="17" />{{
        busy ? "正在保存" : account ? "保存账户配置" : "保存并接入账户"
      }}
    </button>
  </form>
</template>
