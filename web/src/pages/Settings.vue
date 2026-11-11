<script setup lang="ts">
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
        <h1>接入与设置<span class="title-dot">.</span></h1>
        <p class="muted">搜索交易服务器，确认接入配置后连接你的账户。</p>
      </div>
      <div class="page-actions">
        <button class="btn" @click="passwordOpen = true">
          <KeyRound :size="16" />修改密码
        </button>
        <button class="btn btn-primary" @click="accountOpen = true">
          <Plus :size="17" />接入账户
        </button>
      </div>
    </div>

    <p v-if="error" role="alert" class="notice notice-error">
      {{ error.message }}
    </p>

    <!-- 流程指示：轻量流线型步骤指引，告别笨重大字报 -->
    <div class="setup-stepper">
      <div class="step-node" :class="{ 'step-done': (servers?.length || 0) > 0 }">
        <span class="step-num">01</span>
        <div class="step-body">
          <strong>搜索服务器</strong>
          <small>按经纪商或服务器名称查找</small>
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
          <strong>确认接入配置</strong>
          <small>来源自动记录，确认本次接入</small>
        </div>
      </div>
      <div class="step-connector" />
      <div class="step-node">
        <span class="step-num">03</span>
        <div class="step-body">
          <strong>接入只读账户</strong>
          <small>绑定单位，开启采集</small>
        </div>
      </div>
    </div>

    <!-- 可信服务器列表与配置 -->
    <section class="panel">
      <div class="panel-heading">
        <div>
          <h2>
            可信服务器
            <span class="count-pill">{{ servers?.length || 0 }}</span>
          </h2>
          <p class="muted small">凭据只发往账户明确绑定且已核验的接入点。</p>
        </div>
        <button class="btn" @click="newServer">
          <Plus :size="16" />登记服务器
        </button>
      </div>

      <p v-if="isPending" class="empty-inline">正在加载服务器…</p>
      <div v-else-if="!servers?.length" class="empty-server-box">
        <ServerIcon :size="32" class="empty-icon" />
        <p class="empty-title">尚未登记任何服务器接入点</p>
        <p class="muted small">
          搜索经纪商或服务器，系统自动取得候选接入地址，无需手填 IP 和端口。
        </p>
        <button class="btn btn-primary btn-inline" @click="newServer">
          <Plus :size="15" />现在登记首个服务器
        </button>
      </div>

      <article v-for="server in servers" :key="server.id" class="server-card">
        <div class="server-card-title">
          <span class="icon-box subtle"><ServerIcon :size="21" /></span>
          <div>
            <h3>{{ server.server_name }}</h3>
            <p class="muted small">
              {{ server.broker_name }} · MT4 <span class="divider-dot">·</span>
              {{ server.time_zone_verified ? server.time_zone : "时间校准中" }}（{{
                server.time_mode === "automatic"
                  ? "自动识别"
                  : server.time_mode === "calibrated" ? "交易时间校准"
                  : server.time_zone_verified ? "手动校正" : "自动"
              }}）
            </p>
          </div>
          <div class="server-card-actions">
            <button
              v-if="typeof server.time_ruleset_id === 'string'"
              class="text-button" @click="editTime(server)"
            ><Clock :size="15" />服务器时间</button>
            <button class="text-button" @click="newRevision(server)">新建接入修订 <ArrowUpRight :size="15" /></button>
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
                ? "已停用，不能继续发送凭据"
                : endpoint.risk_accepted_at_ms
                  ? `已确认风险 · ${date(endpoint.risk_accepted_at_ms)}`
                  : "需登记独立核验依据并确认剩余风险"
            }}</small>
          </div>
          <span
            v-if="endpoint.state === 'reviewed' && endpoint.risk_accepted_at_ms"
            class="badge badge-good"
            ><ShieldCheck :size="13" />已核验</span
          ><button
            v-else-if="endpoint.state !== 'retired'"
            class="btn btn-small"
            @click="review(endpoint)"
          >
            核验接入点</button
          ><span v-else class="badge badge-neutral">已停用</span>
        </div>
      </article>
    </section>

    <!-- 系统与凭据安全：结构清晰，密码修改通过独立 dialog 触发 -->
    <section class="panel security-section">
      <div class="panel-heading">
        <div>
          <h2>系统与凭据安全</h2>
          <p class="muted small">实例主密码管理与私有数据安全保障机制。</p>
        </div>
      </div>

      <div class="security-grid">
        <!-- 密码管理卡片 -->
        <div class="security-card password-card">
          <div class="security-card-header">
            <span class="icon-box subtle"><KeyRound :size="20" /></span>
            <div>
              <h3>系统登录密码</h3>
              <p class="muted small">保护控制台的管理员密码与会话有效性。</p>
            </div>
          </div>
          <p class="card-desc">
            主密码至少需 16 个字符。修改密码后将立即撤销所有现有会话并要求重新登录。
          </p>
          <div class="security-card-footer">
            <span class="badge badge-good">
              <CheckCircle2 :size="13" />保护中
            </span>
            <button class="btn btn-small" @click="passwordOpen = true">
              <KeyRound :size="14" />修改密码
            </button>
          </div>
        </div>

        <!-- 安全特性清单：取代原本的说教大字报 -->
        <div class="security-card features-card">
          <div class="feature-item">
            <div class="feature-icon"><Lock :size="16" /></div>
            <div>
              <strong>投资者凭据独立加密</strong>
              <p>账户凭据使用专用主密钥单独加密，数据库备份中不含密钥，保障离线安全。</p>
            </div>
          </div>
          <div class="feature-item">
            <div class="feature-icon"><Radio :size="16" /></div>
            <div>
              <strong>接入变更熔断暂停</strong>
              <p>接入地址变化后旧连接立即中断，账户进入暂停状态，需新修订核验后重新绑定。</p>
            </div>
          </div>
          <div class="feature-item">
            <div class="feature-icon"><Clock :size="16" /></div>
            <div>
              <strong>保留原始墙钟时间</strong>
              <p>未知时区的服务器订单时间严格保留原值，不生成不可靠的日期统计。</p>
            </div>
          </div>
        </div>
      </div>
    </section>

    <Modal v-model:open="timeOpen" title="服务器时间" description="系统优先自动识别，用于统计服务器当日利润与结余。">
      <ServerTimeForm v-if="currentTimeServer" :server="currentTimeServer" @saved="timeOpen = false" @cancel="timeOpen = false" />
    </Modal>

    <!-- 独立弹窗：修改密码 -->
    <Modal
      v-model:open="passwordOpen"
      title="修改系统登录密码"
      description="建议使用 16 位以上的高强度密码。修改后需重新登录。"
    >
      <PasswordForm
        @cancel="passwordOpen = false"
        @saved="passwordOpen = false"
      />
    </Modal>

    <!-- 弹窗：登记服务器 -->
    <Modal
      v-model:open="serverOpen"
      :title="editServer ? '建立接入点新修订' : '登记 MT4 服务器'"
      description="登记配置不发送账户凭据。"
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
      title="核验固定接入点"
      description="独立核验和剩余风险确认是发送凭据前的必要门槛。"
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
      title="接入只读账户"
      description="同一个登录号可以存在于不同服务器，按服务器分别管理。"
    >
      <AccountForm
        @saved="accountOpen = false"
        @cancel="accountOpen = false"
      />
    </Modal>
  </div>
</template>
