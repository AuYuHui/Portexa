<script setup lang="ts">
import { ref } from "vue";
import {
  Layers3,
  ArrowRight,
  ShieldCheck,
  LoaderCircle,
  KeyRound,
} from "@lucide/vue";
import { useSession } from "../session";
const session = useSession();
const password = ref("");
const busy = ref(false);
const error = ref("");
async function submit() {
  busy.value = true;
  error.value = "";
  try {
    await session.login(password.value);
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    password.value = "";
    busy.value = false;
  }
}
</script>

<template>
  <main class="login-screen">
    <section class="login-story">
      <div class="brand">
        <span class="brand-mark"><Layers3 :size="25" /></span>Portexa<span
          class="brand-dot"
          >.</span
        >
      </div>
      <span class="eyebrow">YOUR ACCOUNTS, ONE PERSPECTIVE</span>
      <h1>每个账户，<br />一目了然。</h1>
      <p>
        在你的私有空间，统一查看资金、持仓<br
          class="desktop-only"
        />与同步状态。让数据回到同一张桌面。
      </p>
      <div class="login-assurance">
        <ShieldCheck :size="20" /><span
          >私有部署 <span>·</span> 投资者凭据加密 <span>·</span> 只读采集</span
        >
      </div>
      <div class="story-lines" aria-hidden="true">
        <i /><i /><i /><i /><i />
      </div>
    </section>
    <section class="login-form-wrap">
      <form class="login-form" @submit.prevent="submit">
        <span class="icon-box"><KeyRound :size="24" /></span>
        <h2>进入你的工作台</h2>
        <p class="muted">使用本地初始化时设置的系统密码登录。</p>
        <label class="field"
          >系统密码<input
            v-model="password"
            name="password"
            type="password"
            autocomplete="current-password"
            required
            autofocus
            placeholder="请输入至少 16 个字符的密码"
            maxlength="1024"
        /></label>
        <p v-if="error" class="error-message" role="alert">{{ error }}</p>
        <button
          class="btn btn-primary login-submit"
          :disabled="busy || !password"
        >
          <LoaderCircle v-if="busy" :size="18" class="spin" /><span>{{
            busy ? "正在验证" : "安全登录"
          }}</span
          ><ArrowRight v-if="!busy" :size="18" />
        </button>
        <p class="login-help">
          忘记密码？请在服务器本地重置。<br />没有用户名，也无需注册。
        </p>
      </form>
      <span class="login-footer">PORTEXA · 你的数据，由你掌握</span>
    </section>
  </main>
</template>
