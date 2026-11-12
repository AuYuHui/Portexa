import { createApp, watchEffect } from "vue";
import { createPinia } from "pinia";
import { VueQueryPlugin } from "@tanstack/vue-query";
import { createRouter, createWebHistory } from "vue-router";
import { queryClient } from "./state";
import { i18n, t } from "./i18n";
import App from "./App.vue";
import Overview from "./pages/Overview.vue";
import Account from "./pages/Account.vue";
import Settings from "./pages/Settings.vue";
import { defineAsyncComponent } from "vue";
import "./style.css";

const router = createRouter({
  history: createWebHistory(),
  scrollBehavior(to, from, savedPosition) {
    if (savedPosition) return savedPosition;
    // Chart controls and filters update the query on the current page.
    if (to.path === from.path) return false;
    return { top: 0 };
  },
  routes: [
    { path: "/", component: Overview },
    { path: "/accounts/:id", component: Account },
    { path: "/settings", component: Settings },
    { path: "/analytics", component: defineAsyncComponent(() => import("./pages/Analytics.vue")) },
    { path: "/:pathMatch(.*)*", redirect: "/" },
  ],
});
watchEffect(() => {
  const path = router.currentRoute.value.path;
  const title = path === "/analytics" ? t("analytics.title") : path === "/settings"
    ? t("settings.connectionsSettings")
    : path.startsWith("/accounts/")
      ? t("app.accountDetails")
      : t("overview.accountOverview");
  document.title = `Portexa · ${title}`;
});
createApp(App)
  .use(i18n)
  .use(createPinia())
  .use(VueQueryPlugin, { queryClient })
  .use(router)
  .mount("#app");
