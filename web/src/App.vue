<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import { RouterLink, RouterView, useRouter } from "vue-router";
import {
  LayoutDashboard,
  SlidersHorizontal,
  LogOut,
  ShieldCheck,
  ArrowUpRight,
  LoaderCircle,
  Layers3,
} from "@lucide/vue";
import { useSession } from "./session";
import Login from "./pages/Login.vue";

const session = useSession();
const router = useRouter();
const loadError = ref("");
async function bootstrap() {
  loadError.value = "";
  try {
    await session.load();
  } catch {
    loadError.value = "无法连接服务，请确认服务可用后重试。";
  }
}
function activity(event: Event) {
  if (event.isTrusted) void session.interact();
}
async function logout() {
  try {
    await session.logout();
  } finally {
    await router.replace("/");
  }
}
onMounted(() => {
  void bootstrap();
  window.addEventListener("pointerdown", activity, { passive: true });
  window.addEventListener("keydown", activity, { passive: true });
});
onUnmounted(() => {
  window.removeEventListener("pointerdown", activity);
  window.removeEventListener("keydown", activity);
});
</script>

<template>
  <div v-if="loadError" class="center-screen">
    <p role="alert">{{ loadError }}</p>
    <button class="btn" @click="bootstrap">重试连接</button>
  </div>
  <div v-else-if="!session.ready" class="center-screen">
    <LoaderCircle class="spin" :size="24" /><span>正在连接 Portexa</span>
  </div>
  <Login v-else-if="!session.authenticated" />
  <div v-else class="app-shell">
    <aside class="sidebar">
      <RouterLink to="/" class="brand" aria-label="Portexa 首页"
        ><span class="brand-mark"><Layers3 :size="24" /></span>Portexa<span
          class="brand-dot"
          >.</span
        ></RouterLink
      >
      <div class="workspace-label">个人资产工作台</div>
      <nav aria-label="主导航">
        <RouterLink to="/" class="nav-link" exact-active-class="nav-active"
          ><LayoutDashboard :size="19" />账户总览<ArrowUpRight
            class="nav-arrow"
            :size="15"
        /></RouterLink>
        <RouterLink to="/settings" class="nav-link" active-class="nav-active"
          ><SlidersHorizontal :size="19" />接入与设置</RouterLink
        >
      </nav>
      <div class="sidebar-note">
        <ShieldCheck :size="20" /><strong>私有部署 · 只读采集</strong>
        <p>资金与持仓集中查看<br />投资者凭据独立加密</p>
      </div>
      <button class="nav-link logout" @click="logout">
        <LogOut :size="18" />退出登录
      </button>
    </aside>
    <main class="main-area">
      <header class="topbar">
        <span class="breadcrumb"
          >工作台 <span>/</span>
          <strong>{{
            $route.path === "/settings"
              ? "接入与设置"
              : $route.path.startsWith("/accounts/")
                ? "账户详情"
                : "账户总览"
          }}</strong></span
        ><span class="private-badge"><span class="status-dot" />私有空间</span>
      </header>
      <RouterView />
      <footer class="footer">
        <span>PORTEXA <span class="muted">/</span> 资产有据，决策有序</span
        ><span>只读账户聚合 · 第一阶段</span>
      </footer>
    </main>
  </div>
</template>
