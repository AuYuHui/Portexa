<script setup lang="ts">
import { computed, ref } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { ShieldCheck, LoaderCircle } from "@lucide/vue";
import type { Account, Server } from "../types";
import { api } from "../api";
import { refreshAccounts } from "../state";
const props = defineProps<{ account?: Account }>();
const emit = defineEmits<{ cancel: []; saved: [] }>();
const { data: servers, error: loadError } = useQuery({
  queryKey: ["servers"],
  queryFn: () => api<Server[]>("/servers"),
});
const serverID = ref(props.account?.server_id || "");
const endpointID = ref(props.account?.endpoint_id || "");
const label = ref(props.account?.label || "");
const login = ref(props.account?.login?.toString() || "");
const password = ref("");
const PRESET_CURRENCIES = [
  "USD",
  "USC",
  "EUR",
  "GBP",
  "JPY",
  "AUD",
  "CAD",
  "CHF",
  "HKD",
  "CNH",
  "SGD",
  "NZD",
];
const initialCurrency = (props.account?.confirmed_currency || "USD").toUpperCase();
const isPreset = PRESET_CURRENCIES.includes(initialCurrency);

const selectedCurrency = ref(isPreset ? initialCurrency : "CUSTOM");
const customCurrency = ref(isPreset ? "" : initialCurrency);
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
    const finalCurrency = (
      selectedCurrency.value === "CUSTOM"
        ? customCurrency.value
        : selectedCurrency.value
    )
      .trim()
      .toUpperCase();
    if (!finalCurrency) {
      error.value = "请选择或输入有效的账户币种单位。";
      busy.value = false;
      return;
    }
    const values = {
      label: label.value,
      endpoint_id: endpointID.value,
      confirmed_currency: finalCurrency,
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
    <div class="field">
      <label for="account-currency">账户单位</label>
      <div :class="{ 'currency-select-group': selectedCurrency === 'CUSTOM' }">
        <select
          id="account-currency"
          v-model="selectedCurrency"
          required
          aria-label="账户单位"
        >
          <optgroup label="主流折算单位">
            <option value="USD">USD · 美元（标准折算 1:1）</option>
            <option value="USC">USC · 美元美分（美分折算 100:1）</option>
          </optgroup>
          <optgroup label="常见主要原币">
            <option value="EUR">EUR · 欧元</option>
            <option value="GBP">GBP · 英镑</option>
            <option value="JPY">JPY · 日元</option>
            <option value="AUD">AUD · 澳元</option>
            <option value="CAD">CAD · 加元</option>
            <option value="CHF">CHF · 瑞士法郎</option>
            <option value="HKD">HKD · 港币</option>
            <option value="CNH">CNH · 离岸人民币</option>
            <option value="SGD">SGD · 新加坡元</option>
            <option value="NZD">NZD · 新西兰元</option>
          </optgroup>
          <option value="CUSTOM">其他币种（手动输入代码）…</option>
        </select>
        <input
          v-if="selectedCurrency === 'CUSTOM'"
          v-model="customCurrency"
          type="text"
          required
          maxlength="12"
          placeholder="输入币种代码（如 SEK）"
          class="custom-currency-input"
          aria-label="自定义币种代码"
        />
      </div>
      <small v-if="selectedCurrency === 'USD'">
        标准美元账户，按 1:1 计入美元资金总计。
      </small>
      <small v-else-if="selectedCurrency === 'USC'">
        美分账户（Cent），按 100:1 换算折算计入美元总计。
      </small>
      <small v-else-if="selectedCurrency === 'CUSTOM'">
        请输入自定义货币代码（如 SEK、THB 等），金额将独立单列展示。
      </small>
      <small v-else>
        原币账户，金额将独立单列展示，不折算混入美元总计。
      </small>
    </div>
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
    <div class="dialog-actions">
      <button
        type="button"
        class="btn"
        :disabled="busy"
        @click="emit('cancel')"
      >
        取消
      </button>
      <button
        type="submit"
        class="btn btn-primary"
        :disabled="
          busy || !endpointID || !endpoints.some((e) => e.id === endpointID)
        "
      >
        <LoaderCircle v-if="busy" class="spin" :size="16" />
        {{ busy ? "正在保存…" : account ? "保存账户配置" : "保存并接入账户" }}
      </button>
    </div>
  </form>
</template>
