<script setup lang="ts">
import { localizeMessage } from "../i18n";
import { useI18n } from "vue-i18n";
import { computed, ref } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { ShieldCheck, LoaderCircle } from "@lucide/vue";
import type { Account, Server } from "../types";
import { api } from "../api";
import { refreshAccounts } from "../state";

const { t } = useI18n();
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
const error = ref<string | (() => string)>("");
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
    error.value = () => t('accountForm.theInvestorPasswordMustContainToUtfBytes');
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
      error.value = () => t('accountForm.selectOrEnterAValidAccountCurrencyUnit');
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
      {{ localizeMessage(loadError.message) }}
    </p>
    <p v-if="servers && !servers.length" class="notice notice-warning">
      {{ t('accountForm.closeThisWindowFirstThenRegisterAndIndependently') }}
    </p>
    <label class="field"
      >{{ t('accountForm.accountName') }}<input
        v-model="label"
        required
        maxlength="96"
        :placeholder="t('accountForm.exampleMainAccountConservativeStrategy')"
    /></label>
    <div class="form-grid">
      <label class="field"
        >{{ t('accountForm.server') }}<select
          v-model="serverID"
          :aria-label="t('accountForm.server')"
          required
          :disabled="!!account"
          @change="selectServer"
        >
          <option disabled value="">{{ t('accountForm.selectARegisteredServer') }}</option>
          <option v-for="s in servers" :key="s.id" :value="s.id">
            {{ s.broker_name }} / {{ s.server_name }}
          </option>
        </select></label
      ><label class="field"
        >{{ t('accountForm.mtLogin') }}<input
          v-model="login"
          inputmode="numeric"
          pattern="[0-9]+"
          required
          :disabled="!!account"
          :placeholder="t('accountForm.tradingLoginNumber')"
      /></label>
    </div>
    <label class="field"
      >{{ t('accountForm.fixedEndpoint') }}<select v-model="endpointID" required :aria-label="t('accountForm.fixedEndpoint')">
        <option disabled value="">{{ t('accountForm.selectAVerifiedRevisionWithAcceptedRisk') }}</option>
        <option
          v-if="
            account && !endpoints.some((e) => e.id === account?.endpoint_id)
          "
          disabled
          :value="account.endpoint_id"
        >
          {{ t('accountForm.thePreviousEndpointIsRetiredRebindTheAccount') }}
        </option>
        <option v-for="e in endpoints" :key="e.id" :value="e.id">
          {{ t('accountForm.revision') }} {{ e.revision }} · {{ e.ip_address }}:{{ e.port }}
        </option></select
      ><small
        >{{ t('accountForm.onlyVerifiedRevisionsWithAcceptedRiskAreAllowed') }}</small
      ></label
    >
    <label class="field"
      >{{ t('accountForm.investorPassword') }} {{ account ? t('accountForm.leaveBlankToKeepTheCurrentPassword') : ""
      }}<input
        v-model="password"
        type="password"
        :required="!account"
        autocomplete="new-password"
        maxlength="16"
        :placeholder="t('accountForm.useOnlyAReadOnlyInvestorPassword')"
      /><small
        >{{ t('accountForm.differentFromYourPortexaLoginPasswordLimitedTo') }}</small
      ></label
    >
    <div class="field">
      <label for="account-currency">{{ t('accountForm.accountUnit') }}</label>
      <div :class="{ 'currency-select-group': selectedCurrency === 'CUSTOM' }">
        <select
          id="account-currency"
          v-model="selectedCurrency"
          required
          :aria-label="t('accountForm.accountUnit')"
        >
          <optgroup :label="t('accountForm.unitsIncludedInTheUsdSubtotal')">
            <option value="USD">{{ t('accountForm.usdUsDollarConversion') }}</option>
            <option value="USC">{{ t('accountForm.uscUsDollarCentConversion') }}</option>
          </optgroup>
          <optgroup :label="t('accountForm.commonNativeCurrencies')">
            <option value="EUR">{{ t('accountForm.eurEuro') }}</option>
            <option value="GBP">{{ t('accountForm.gbpBritishPound') }}</option>
            <option value="JPY">{{ t('accountForm.jpyJapaneseYen') }}</option>
            <option value="AUD">{{ t('accountForm.audAustralianDollar') }}</option>
            <option value="CAD">{{ t('accountForm.cadCanadianDollar') }}</option>
            <option value="CHF">{{ t('accountForm.chfSwissFranc') }}</option>
            <option value="HKD">{{ t('accountForm.hkdHongKongDollar') }}</option>
            <option value="CNH">{{ t('accountForm.cnhOffshoreRenminbi') }}</option>
            <option value="SGD">{{ t('accountForm.sgdSingaporeDollar') }}</option>
            <option value="NZD">{{ t('accountForm.nzdNewZealandDollar') }}</option>
          </optgroup>
          <option value="CUSTOM">{{ t('accountForm.otherCurrencyEnterCode') }}</option>
        </select>
        <input
          v-if="selectedCurrency === 'CUSTOM'"
          v-model="customCurrency"
          type="text"
          required
          maxlength="12"
          :placeholder="t('accountForm.enterACurrencyCodeEGSek')"
          class="custom-currency-input"
          :aria-label="t('accountForm.customCurrencyCode')"
        />
      </div>
      <small v-if="selectedCurrency === 'USD'">
        {{ t('accountForm.standardUsdAccountIncludedInTheUsdSubtotal') }}
      </small>
      <small v-else-if="selectedCurrency === 'USC'">
        {{ t('accountForm.centAccountConvertedAtIntoTheUsdSubtotal') }}
      </small>
      <small v-else-if="selectedCurrency === 'CUSTOM'">
        {{ t('accountForm.enterACurrencyCodeSuchAsSekOr') }}
      </small>
      <small v-else>
        {{ t('accountForm.nativeCurrencyAccountAmountsAreShownSeparatelyAnd') }}
      </small>
    </div>
    <label class="checkbox-row"
      ><input v-model="confirmed" type="checkbox" /><span
        >{{ t('accountForm.iHaveVerifiedThisAccountSCurrencyAnd') }}<small
          >{{ t('accountForm.usdConvertsAtAndUscAtOtherCurrencies') }}</small
        ></span
      ></label
    >
    <label class="checkbox-row"
      ><input v-model="enabled" type="checkbox" /><span
        >{{ t('accountForm.enableAutomaticSnapshotCollection') }}<small
          >{{ t('accountForm.setTheSharedIntervalInConnectionsSettingsFunds') }}</small
        ></span
      ></label
    >
    <div class="notice notice-subtle">
      <ShieldCheck :size="17" />{{ t('accountForm.investorCredentialsAreStoredEncryptedTheApiNever') }}
    </div>
    <p v-if="error" class="error-message" role="alert">{{ localizeMessage(error) }}</p>
    <div class="dialog-actions">
      <button
        type="button"
        class="btn"
        :disabled="busy"
        @click="emit('cancel')"
      >
        {{ t('accountForm.cancel') }}
      </button>
      <button
        type="submit"
        class="btn btn-primary"
        :disabled="
          busy || !endpointID || !endpoints.some((e) => e.id === endpointID)
        "
      >
        <LoaderCircle v-if="busy" class="spin" :size="16" />
        {{ busy ? t('accountForm.saving') : account ? t('accountForm.saveAccountSettings') : t('accountForm.saveAndConnectAccount') }}
      </button>
    </div>
  </form>
</template>
