import { defineStore } from "pinia";
import { ref } from "vue";
import { queryClient } from "./state";
import { api, configureSession } from "./api";

interface Session {
  authenticated: boolean;
  csrf_token?: string;
}
export const useSession = defineStore("session", () => {
  const ready = ref(false);
  const authenticated = ref(false);
  function clear() {
    authenticated.value = false;
    configureSession("", clear);
    queryClient.clear();
  }
  function accept(data: Session) {
    authenticated.value = data.authenticated;
    configureSession(data.csrf_token || "", clear);
    ready.value = true;
  }
  async function load() {
    accept(await api<Session>("/auth/session"));
  }
  async function login(password: string) {
    accept(await api<Session>("/auth/login", "POST", { password }));
  }
  async function logout() {
    try {
      await api("/auth/logout", "POST");
    } finally {
      clear();
    }
  }
  let lastTouch = 0;
  async function interact() {
    if (!authenticated.value || Date.now() - lastTouch < 60_000) return;
    lastTouch = Date.now();
    try {
      await api("/auth/touch", "POST");
    } catch {
      /* API handles expiry; next real interaction can retry. */
    }
  }
  return { ready, authenticated, clear, load, login, logout, interact };
});
