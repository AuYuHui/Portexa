import { createApp } from "vue";
import { createPinia } from "pinia";
import { VueQueryPlugin } from "@tanstack/vue-query";
import { createRouter, createWebHistory } from "vue-router";
import { queryClient } from "./state";
import App from "./App.vue";
import Overview from "./pages/Overview.vue";
import Account from "./pages/Account.vue";
import Settings from "./pages/Settings.vue";
import "./style.css";

const router = createRouter({
  history: createWebHistory(),
  scrollBehavior: () => ({ top: 0 }),
  routes: [
    { path: "/", component: Overview },
    { path: "/accounts/:id", component: Account },
    { path: "/settings", component: Settings },
    { path: "/:pathMatch(.*)*", redirect: "/" },
  ],
});
createApp(App)
  .use(createPinia())
  .use(VueQueryPlugin, { queryClient })
  .use(router)
  .mount("#app");
