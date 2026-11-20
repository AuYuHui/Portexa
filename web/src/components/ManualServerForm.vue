<script setup lang="ts">
import { localizeMessage } from "../i18n";
import { useI18n } from "vue-i18n";
import { computed, ref } from "vue";
import { LoaderCircle } from "@lucide/vue";
import { api } from "../api";
import { refreshAccounts } from "../state";
import type { DirectoryServerName, Server } from "../types";

const { t } = useI18n();
const props = defineProps<{ server?: Server; mt5Selection?: DirectoryServerName }>();
const emit = defineEmits<{ cancel: []; saved: [] }>();
const broker = ref(props.mt5Selection?.broker_name ?? "");
const name = ref(props.mt5Selection?.server_name ?? "");
const address = ref("");
const port = ref(443);
const protocol = ref<Server["protocol"]>(props.mt5Selection ? "mt5" : "mt4");
const webHostname = ref(props.server?.endpoints.find((endpoint) => endpoint.state !== "retired")?.web_hostname || "");
const isMT5 = computed(() => (props.server?.protocol ?? protocol.value) === "mt5");
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
        ...(isMT5.value ? { web_hostname: webHostname.value.trim() } : {}),
      });
    } else {
      await api("/servers", "POST", {
        protocol: protocol.value,
        ...(isMT5.value ? { web_hostname: webHostname.value.trim() } : {}),
        broker_name: broker.value,
        server_name: name.value,
        ip_address: address.value,
        port: port.value,
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
    <div v-if="!server" class="field">
      <label for="manual-server-platform">{{ t('mt5.platform') }}</label>
      <select id="manual-server-platform" v-model="protocol">
        <option value="mt4">MT4</option>
        <option value="mt5">MT5</option>
      </select>
    </div>
    <template v-if="!server"
      ><label class="field"
        >{{ t('manualServerForm.broker') }}<input
          v-model="broker"
          required
          maxlength="128"
          :placeholder="t('manualServerForm.brokerName')" /></label
      ><label class="field"
        >{{ isMT5 ? t('mt5.exactServerName') : t('manualServerForm.exactMtServerName') }}<input
          v-model="name"
          required
          maxlength="128"
          :placeholder="t('manualServerForm.exampleSyntheticLive')"
        /><small>{{ isMT5 ? t('mt5.exactServerNameHelp') : t('manualServerForm.theServerNameIsUsedForProtocolConfiguration') }}</small></label
      ></template
    >
    <p v-else class="notice notice-warning">
      {{ t('manualServerForm.aNewRevisionRetiresThePreviousAddressAnd') }}
    </p>
    <p v-if="isMT5 && mt5Selection" class="muted small">{{ t('directoryPicker.mt5SelectionHelp') }}</p>
    <label v-if="isMT5" class="field">
      {{ t('mt5.terminalHostname') }}
      <input v-model="webHostname" required maxlength="128" placeholder="mt5.broker.example" />
      <small>{{ t('mt5.terminalHostnameHelp') }}</small>
    </label>
    <div class="form-grid">
      <label class="field"
        >{{ t('manualServerForm.fixedIpAddress') }}<input
          v-model="address"
          required
          :placeholder="t('manualServerForm.independentlyVerifiedPublicIp')" /></label
      ><label class="field"
        >{{ t('manualServerForm.port') }}<input
          v-model.number="port"
          required
          type="number"
          min="1"
          max="65535"
      /></label>
    </div>
    <p class="muted small">
      {{ t('manualServerForm.savingRegistersConfigurationWithoutSendingCredentialsNextRecord') }}
    </p>
    <p v-if="error" role="alert" class="error-message">{{ localizeMessage(error) }}</p>
    <div class="dialog-actions">
      <button
        type="button"
        class="btn"
        :disabled="busy"
        @click="emit('cancel')"
      >
        {{ t('accountForm.cancel') }}
      </button>
      <button type="submit" class="btn btn-primary" :disabled="busy">
        <LoaderCircle v-if="busy" class="spin" :size="16" />
        {{
          busy
            ? t('manualServerForm.registering')
            : server
              ? t('manualServerForm.createRevisionAndPauseAccounts')
              : t('manualServerForm.registerServerAndContinueVerification')
        }}
      </button>
    </div>
  </form>
</template>
