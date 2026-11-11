<script setup lang="ts">
import { localizeMessage } from "./i18n";
import { useI18n } from "vue-i18n";
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
import LanguageSwitch from "./components/LanguageSwitch.vue";

const { t } = useI18n();

const session = useSession();
const router = useRouter();
const loadError = ref<string | (() => string)>("");
async function bootstrap() {
  loadError.value = "";
  try {
    await session.load();
  } catch {
    loadError.value = () => t('app.unableToConnectCheckThatTheServiceIs');
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
    <p role="alert">{{ localizeMessage(loadError) }}</p>
    <button class="btn" @click="bootstrap">{{ t('app.retryConnection') }}</button>
  </div>
  <div v-else-if="!session.ready" class="center-screen">
    <LoaderCircle class="spin" :size="24" /><span>{{ t('app.connectingToPortexa') }}</span>
  </div>
  <Login v-else-if="!session.authenticated" />
  <div v-else class="app-shell">
    <aside class="sidebar">
      <RouterLink to="/" class="brand" :aria-label="t('app.portexaHome')"
        ><span class="brand-mark"><Layers3 :size="24" /></span>Portexa<span
          class="brand-dot"
          >.</span
        ></RouterLink
      >
      <div class="workspace-label">{{ t('app.personalAssetWorkspace') }}</div>
      <nav :aria-label="t('app.mainNavigation')">
        <RouterLink to="/" class="nav-link" exact-active-class="nav-active"
          ><LayoutDashboard :size="19" />{{ t('overview.accountOverview') }}<ArrowUpRight
            class="nav-arrow"
            :size="15"
        /></RouterLink>
        <RouterLink to="/settings" class="nav-link" active-class="nav-active"
          ><SlidersHorizontal :size="19" />{{ t('settings.connectionsSettings') }}</RouterLink
        >
      </nav>
      <div class="sidebar-note">
        <ShieldCheck :size="20" /><strong>{{ t('app.privateDeploymentReadOnlyCollection') }}</strong>
        <p>{{ t('app.fundsAndPositionsInOnePlace') }}<br />{{ t('settings.separatelyEncryptedInvestorCredentials') }}</p>
      </div>
      <button class="nav-link logout" @click="logout">
        <LogOut :size="18" />{{ t('app.signOut') }}
      </button>
    </aside>
    <main class="main-area">
      <header class="topbar">
        <span class="breadcrumb"
          >{{ t('app.workspace') }} <span>/</span>
          <strong>{{
            $route.path === "/settings"
              ? t('settings.connectionsSettings')
              : $route.path.startsWith("/accounts/")
                ? t('app.accountDetails')
                : t('overview.accountOverview')
          }}</strong></span
        >
        <div class="topbar-actions">
          <LanguageSwitch />
          <span class="private-badge"><span class="status-dot" />{{ t('app.privateWorkspace') }}</span>
        </div>
      </header>
      <RouterView />
      <footer class="footer">
        <span>PORTEXA <span class="muted">/</span> {{ t('app.clearAssetsInformedDecisions') }}</span
        ><span>{{ t('app.readOnlyAccountAggregationPhaseOne') }}</span>
      </footer>
    </main>
  </div>
</template>
