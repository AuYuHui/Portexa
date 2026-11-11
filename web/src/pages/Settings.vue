<script setup lang="ts">
import { localizeMessage } from "../i18n";
import { useI18n } from "vue-i18n";
import { computed, onMounted, ref } from "vue";
import { useRoute } from "vue-router";
import { useQuery } from "@tanstack/vue-query";
import {
  Plus,
  Server as ServerIcon,
  ShieldCheck,
  ArrowUpRight,
  KeyRound,
  Lock,
  Radio,
  Clock,
  CheckCircle2,
} from "@lucide/vue";
import { api } from "../api";
import type { Server, Endpoint } from "../types";
import { date } from "../format";
import Modal from "../components/Modal.vue";
import AccountForm from "../components/AccountForm.vue";
import ServerForm from "../components/ServerForm.vue";
import ReviewForm from "../components/ReviewForm.vue";
import PasswordForm from "../components/PasswordForm.vue";
import ServerTimeForm from "../components/ServerTimeForm.vue";
import CollectionSettings from "../components/CollectionSettings.vue";

const { t } = useI18n();

const route = useRoute();

const {
  data: servers,
  error,
  isPending,
} = useQuery({
  queryKey: ["servers"],
  queryFn: () => api<Server[]>("/servers"),
  refetchInterval: 10_000,
});

const serverOpen = ref(false);
const accountOpen = ref(false);
const reviewOpen = ref(false);
const passwordOpen = ref(false);
const timeOpen = ref(false);
const timeServer = ref<Server>();
const currentTimeServer = computed(() => servers.value?.find((server) => server.id === timeServer.value?.id) || timeServer.value);
const editServer = ref<Server>();
const reviewEndpoint = ref<Endpoint>();

function review(endpoint: Endpoint) {
  reviewEndpoint.value = endpoint;
  reviewOpen.value = true;
}

function newRevision(server: Server) {
  editServer.value = server;
  serverOpen.value = true;
}

function newServer() {
  editServer.value = undefined;
  serverOpen.value = true;
}
function editTime(server: Server) {
  timeServer.value = server;
  timeOpen.value = true;
}

onMounted(() => {
  if (route.query.add === "account") accountOpen.value = true;
});
</script>

<template>
  <div class="page-content">
    <div class="page-title">
      <div>
        <span class="eyebrow">CONNECTIONS & SECURITY</span>
        <h1>{{ t('settings.connectionsSettings') }}<span class="title-dot">.</span></h1>
        <p class="muted">{{ t('settings.findYourTradingServerConfirmTheConnectionAnd') }}</p>
      </div>
      <div class="page-actions">
        <button class="btn" @click="passwordOpen = true">
          <KeyRound :size="16" />{{ t('settings.changePassword') }}
        </button>
        <button class="btn btn-primary" @click="accountOpen = true">
          <Plus :size="17" />{{ t('overview.connectAccount') }}
        </button>
      </div>
    </div>

    <p v-if="error" role="alert" class="notice notice-error">
      {{ localizeMessage(error.message) }}
    </p>

    <!-- 流程指示：轻量流线型步骤指引，告别笨重大字报 -->
    <div class="setup-stepper">
      <div class="step-node" :class="{ 'step-done': (servers?.length || 0) > 0 }">
        <span class="step-num">01</span>
        <div class="step-body">
          <strong>{{ t('settings.findServer') }}</strong>
          <small>{{ t('settings.searchByBrokerOrServerName') }}</small>
        </div>
      </div>
      <div class="step-connector" />
      <div
        class="step-node"
        :class="{
          'step-done': servers?.some((s) =>
            s.endpoints.some((e) => e.risk_accepted_at_ms),
          ),
        }"
      >
        <span class="step-num">02</span>
        <div class="step-body">
          <strong>{{ t('settings.confirmConnection') }}</strong>
          <small>{{ t('settings.sourceRecordedAutomaticallyConfirmThisConnection') }}</small>
        </div>
      </div>
      <div class="step-connector" />
      <div class="step-node">
        <span class="step-num">03</span>
        <div class="step-body">
          <strong>{{ t('settings.connectReadOnlyAccount') }}</strong>
          <small>{{ t('settings.confirmTheUnitAndStartCollection') }}</small>
        </div>
      </div>
    </div>

    <!-- 可信服务器列表与配置 -->
    <section class="panel">
      <div class="panel-heading">
        <div>
          <h2>
            {{ t('settings.trustedServers') }}
            <span class="count-pill">{{ servers?.length || 0 }}</span>
          </h2>
          <p class="muted small">{{ t('settings.credentialsAreSentOnlyToTheVerifiedEndpoint') }}</p>
        </div>
        <button class="btn" @click="newServer">
          <Plus :size="16" />{{ t('settings.registerServer') }}
        </button>
      </div>

      <p v-if="isPending" class="empty-inline">{{ t('settings.loadingServers') }}</p>
      <div v-else-if="!servers?.length" class="empty-server-box">
        <ServerIcon :size="32" class="empty-icon" />
        <p class="empty-title">{{ t('settings.noServerEndpointsRegisteredYet') }}</p>
        <p class="muted small">
          {{ t('settings.searchForABrokerOrServerToRetrieve') }}
        </p>
        <button class="btn btn-primary btn-inline" @click="newServer">
          <Plus :size="15" />{{ t('settings.registerYourFirstServer') }}
        </button>
      </div>

      <article v-for="server in servers" :key="server.id" class="server-card">
        <div class="server-card-title">
          <span class="icon-box subtle"><ServerIcon :size="21" /></span>
          <div>
            <h3>{{ server.server_name }}</h3>
            <p class="muted small">
              {{ server.broker_name }} · MT4 <span class="divider-dot">·</span>
              {{ t('settings.serverTimeMode', {
                zone: server.time_zone_verified ? server.time_zone : t('settings.calibratingTime'),
                mode: server.time_mode === "automatic"
                  ? t('settings.automaticDetection')
                  : server.time_mode === "calibrated" ? t('settings.tradeTimeCalibration')
                  : server.time_zone_verified ? t('settings.manualCorrection') : t('settings.automatic'),
              }) }}
            </p>
          </div>
          <div class="server-card-actions">
            <button
              v-if="typeof server.time_ruleset_id === 'string'"
              class="text-button" @click="editTime(server)"
            ><Clock :size="15" />{{ t('settings.serverTime') }}</button>
            <button class="text-button" @click="newRevision(server)">{{ t('settings.newEndpointRevision') }} <ArrowUpRight :size="15" /></button>
          </div>
        </div>
        <div
          v-for="endpoint in server.endpoints"
          :key="endpoint.id"
          class="endpoint-row"
        >
          <span class="revision">v{{ endpoint.revision }}</span>
          <div class="endpoint-address">
            <strong class="mono"
              >{{ endpoint.ip_address }}:{{ endpoint.port }}</strong
            ><small>{{
              endpoint.state === "retired"
                ? t('settings.retiredCredentialsCanNoLongerBeSent')
                : endpoint.risk_accepted_at_ms
                  ? t('settings.riskAccepted', { value1: date(endpoint.risk_accepted_at_ms) })
                  : t('settings.independentVerificationAndAcceptanceOfRemainingRiskAre')
            }}</small>
          </div>
          <span
            v-if="endpoint.state === 'reviewed' && endpoint.risk_accepted_at_ms"
            class="badge badge-good"
            ><ShieldCheck :size="13" />{{ t('settings.verified') }}</span
          ><button
            v-else-if="endpoint.state !== 'retired'"
            class="btn btn-small"
            @click="review(endpoint)"
          >
            {{ t('settings.verifyEndpoint') }}</button
          ><span v-else class="badge badge-neutral">{{ t('settings.retired') }}</span>
        </div>
      </article>
    </section>

    <CollectionSettings />

    <!-- 系统与凭据安全：结构清晰，密码修改通过独立 dialog 触发 -->
    <section class="panel security-section">
      <div class="panel-heading">
        <div>
          <h2>{{ t('settings.systemCredentialSecurity') }}</h2>
          <p class="muted small">{{ t('settings.manageTheInstancePasswordAndProtectionOfPrivate') }}</p>
        </div>
      </div>

      <div class="security-grid">
        <!-- 密码管理卡片 -->
        <div class="security-card password-card">
          <div class="security-card-header">
            <span class="icon-box subtle"><KeyRound :size="20" /></span>
            <div>
              <h3>{{ t('settings.systemLoginPassword') }}</h3>
              <p class="muted small">{{ t('settings.protectAdministratorAccessAndSessionValidity') }}</p>
            </div>
          </div>
          <p class="card-desc">
            {{ t('settings.theSystemPasswordMustContainAtLeastCharacters') }}
          </p>
          <div class="security-card-footer">
            <span class="badge badge-good">
              <CheckCircle2 :size="13" />{{ t('settings.protected') }}
            </span>
            <button class="btn btn-small" @click="passwordOpen = true">
              <KeyRound :size="14" />{{ t('settings.changePassword') }}
            </button>
          </div>
        </div>

        <!-- 安全特性清单：取代原本的说教大字报 -->
        <div class="security-card features-card">
          <div class="feature-item">
            <div class="feature-icon"><Lock :size="16" /></div>
            <div>
              <strong>{{ t('settings.separatelyEncryptedInvestorCredentials') }}</strong>
              <p>{{ t('settings.credentialsAreEncryptedWithADedicatedKeyDatabase') }}</p>
            </div>
          </div>
          <div class="feature-item">
            <div class="feature-icon"><Radio :size="16" /></div>
            <div>
              <strong>{{ t('settings.endpointChangesPauseCollection') }}</strong>
              <p>{{ t('settings.changingAnEndpointStopsPreviousConnectionsAndPauses') }}</p>
            </div>
          </div>
          <div class="feature-item">
            <div class="feature-icon"><Clock :size="16" /></div>
            <div>
              <strong>{{ t('settings.originalWallClockTimesPreserved') }}</strong>
              <p>{{ t('settings.orderTimesFromServersWithUnknownTimeZones') }}</p>
            </div>
          </div>
        </div>
      </div>
    </section>

    <Modal v-model:open="timeOpen" :title="t('settings.serverTime')" :description="t('settings.automaticDetectionIsPreferredForDailyProfitAnd')">
      <ServerTimeForm v-if="currentTimeServer" :server="currentTimeServer" @saved="timeOpen = false" @cancel="timeOpen = false" />
    </Modal>

    <!-- 独立弹窗：修改密码 -->
    <Modal
      v-model:open="passwordOpen"
      :title="t('settings.changeSystemLoginPassword')"
      :description="t('settings.useAStrongPasswordOfAtLeastCharacters')"
    >
      <PasswordForm
        @cancel="passwordOpen = false"
        @saved="passwordOpen = false"
      />
    </Modal>

    <!-- 弹窗：登记服务器 -->
    <Modal
      v-model:open="serverOpen"
      :title="editServer ? t('settings.createEndpointRevision') : t('settings.registerMtServer')"
      :description="t('settings.registeringConfigurationDoesNotSendAccountCredentials')"
    >
      <ServerForm
        :server="editServer"
        @saved="serverOpen = false"
        @cancel="serverOpen = false"
      />
    </Modal>

    <!-- 弹窗：核验接入点 -->
    <Modal
      v-model:open="reviewOpen"
      :title="t('settings.verifyFixedEndpoint')"
      :description="t('settings.independentVerificationAndAcceptanceOfRemainingRiskAre161')"
    >
      <ReviewForm
        v-if="reviewEndpoint"
        :endpoint="reviewEndpoint"
        @saved="reviewOpen = false"
        @cancel="reviewOpen = false"
      />
    </Modal>

    <!-- 弹窗：接入账户 -->
    <Modal
      v-model:open="accountOpen"
      :title="t('settings.connectReadOnlyAccount')"
      :description="t('settings.theSameLoginNumberCanExistOnDifferent')"
    >
      <AccountForm
        @saved="accountOpen = false"
        @cancel="accountOpen = false"
      />
    </Modal>
  </div>
</template>
