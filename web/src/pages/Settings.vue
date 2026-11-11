<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRoute } from "vue-router";
import { useQuery } from "@tanstack/vue-query";
import {
  Plus,
  Server as ServerIcon,
  ShieldCheck,
  ArrowUpRight,
  KeyRound,
} from "@lucide/vue";
import { api } from "../api";
import { useSession } from "../session";
import type { Server, Endpoint } from "../types";
import { date } from "../format";
import Modal from "../components/Modal.vue";
import AccountForm from "../components/AccountForm.vue";
import ServerForm from "../components/ServerForm.vue";
import ReviewForm from "../components/ReviewForm.vue";
const route = useRoute();
const session = useSession();
const {
  data: servers,
  error,
  isPending,
} = useQuery({
  queryKey: ["servers"],
  queryFn: () => api<Server[]>("/servers"),
});
const serverOpen = ref(false);
const accountOpen = ref(false);
const reviewOpen = ref(false);
const editServer = ref<Server>();
const reviewEndpoint = ref<Endpoint>();
const oldPassword = ref("");
const newPassword = ref("");
const confirmPassword = ref("");
const passwordError = ref("");
const passwordBusy = ref(false);
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
async function changePassword() {
  passwordError.value = "";
  if (Array.from(newPassword.value).length < 16) {
    passwordError.value = "新密码至少需要 16 个字符。";
    return;
  }
  if (newPassword.value !== confirmPassword.value) {
    passwordError.value = "两次输入的新密码不一致。";
    return;
  }
  passwordBusy.value = true;
  try {
    await api("/auth/password", "PUT", {
      old_password: oldPassword.value,
      new_password: newPassword.value,
    });
    session.clear();
  } catch (e) {
    passwordError.value = (e as Error).message;
  } finally {
    oldPassword.value = "";
    newPassword.value = "";
    confirmPassword.value = "";
    passwordBusy.value = false;
  }
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
        <p class="muted">先核验接入点，再安全连接你的账户。</p>
      </div>
      <button class="btn btn-primary" @click="accountOpen = true">
        <Plus :size="17" />接入账户
      </button>
    </div>
    <p v-if="error" role="alert" class="notice notice-error">
      {{ error.message }}
    </p>
    <div class="setup-steps">
      <div>
        <span>01</span><strong>登记服务器</strong
        ><small>固定 IP 和准确名称</small>
      </div>
      <div>
        <span>02</span><strong>独立核验并确认风险</strong
        ><small>当前修订单独生效</small>
      </div>
      <div>
        <span>03</span><strong>接入只读账户</strong
        ><small>确认单位，启用采集</small>
      </div>
    </div>
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
      <p v-else-if="!servers?.length" class="empty-inline">
        尚未登记服务器。从你已独立核验的经纪商配置开始。
      </p>
      <article v-for="server in servers" :key="server.id" class="server-card">
        <div class="server-card-title">
          <span class="icon-box subtle"><ServerIcon :size="21" /></span>
          <div>
            <h3>{{ server.server_name }}</h3>
            <p class="muted small">
              {{ server.broker_name }} · MT4 <span class="divider-dot">·</span>
              {{ server.time_zone || "时区未登记" }}（{{
                server.time_zone_verified ? "已核验" : "未核验"
              }}）
            </p>
          </div>
          <button class="text-button" @click="newRevision(server)">
            新建接入修订 <ArrowUpRight :size="15" />
          </button>
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
    <div class="settings-bottom">
      <section class="panel password-panel">
        <div class="panel-heading">
          <div>
            <h2><KeyRound :size="18" />系统登录密码</h2>
            <p class="muted small">
              至少 16 个字符。修改后撤销所有会话，重新登录。
            </p>
          </div>
        </div>
        <form class="form-stack" @submit.prevent="changePassword">
          <label class="field"
            >当前密码<input
              v-model="oldPassword"
              type="password"
              autocomplete="current-password"
              required
              maxlength="1024"
          /></label>
          <div class="form-grid">
            <label class="field"
              >新密码<input
                v-model="newPassword"
                type="password"
                autocomplete="new-password"
                required
                maxlength="1024" /></label
            ><label class="field"
              >再次输入新密码<input
                v-model="confirmPassword"
                type="password"
                autocomplete="new-password"
                required
                maxlength="1024"
            /></label>
          </div>
          <p v-if="passwordError" class="error-message" role="alert">
            {{ passwordError }}
          </p>
          <button class="btn" :disabled="passwordBusy">
            {{ passwordBusy ? "正在修改" : "修改密码并退出登录" }}
          </button>
        </form>
      </section>
      <aside class="security-note">
        <ShieldCheck :size="27" />
        <h2>凭据与数据，各自保管。</h2>
        <p>
          账户投资者密码使用独立密钥加密。数据库备份不包含密钥，请在服务器上分别妥善保管。
        </p>
        <p>
          接入地址变化后，旧连接会取消，账户会暂停。新修订核验完成后，在账户详情中明确重新绑定再启用。
        </p>
        <p class="small">
          未知服务器时区的订单时间保留墙钟原值，不生成依赖准确日期的统计。
        </p>
      </aside>
    </div>
    <Modal
      v-model:open="serverOpen"
      :title="editServer ? '建立接入点新修订' : '登记 MT4 服务器'"
      description="登记配置不发送账户凭据。"
      ><ServerForm :server="editServer" @saved="serverOpen = false"
    /></Modal>
    <Modal
      v-model:open="reviewOpen"
      title="核验固定接入点"
      description="独立核验和剩余风险确认是发送凭据前的必要门槛。"
      ><ReviewForm
        v-if="reviewEndpoint"
        :endpoint="reviewEndpoint"
        @saved="reviewOpen = false"
    /></Modal>
    <Modal
      v-model:open="accountOpen"
      title="接入只读账户"
      description="同一个登录号可以存在于不同服务器，按服务器分别管理。"
      ><AccountForm @saved="accountOpen = false"
    /></Modal>
  </div>
</template>
