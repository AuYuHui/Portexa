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

declare module "vue-router" {
  interface RouteMeta {
    title?: () => string;
  }
}

const router = createRouter({
  history: createWebHistory(),
  scrollBehavior(to, from, savedPosition) {
    if (savedPosition) return savedPosition;
    // Chart controls and filters update the query on the current page.
    if (to.path === from.path) return false;
    return { top: 0 };
  },
  routes: [
    { path: "/", component: Overview, meta: { title: () => t("overview.accountOverview") } },
    { path: "/accounts/:id", component: Account, meta: { title: () => t("app.accountDetails") } },
    { path: "/settings", component: Settings, meta: { title: () => t("settings.connectionsSettings") } },
    { path: "/analytics", component: defineAsyncComponent(() => import("./pages/Analytics.vue")), meta: { title: () => t("analytics.title") } },
    { path: "/positions", component: () => import("./pages/LivePositions.vue"), meta: { title: () => t("orders.positions") } },
    { path: "/order-history", component: () => import("./pages/OrderHistory.vue"), meta: { title: () => t("orders.history") } },
    { path: "/:pathMatch(.*)*", redirect: "/" },
  ],
});
watchEffect(() => {
  const title = router.currentRoute.value.meta.title?.() || t("overview.accountOverview");
  document.title = `Portexa · ${title}`;
});
createApp(App)
  .use(i18n)
  .use(createPinia())
  .use(VueQueryPlugin, { queryClient })
  .use(router)
  .mount("#app");
