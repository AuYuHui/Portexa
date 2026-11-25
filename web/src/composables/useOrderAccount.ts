import { computed } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { useRoute, useRouter } from "vue-router";
import { api } from "../api";
import type { Account } from "../types";

export function useOrderAccount() {
  const route = useRoute();
  const router = useRouter();
  const query = useQuery({
    queryKey: ["accounts-for-orders"],
    queryFn: ({ signal }) => api<Account[]>("/accounts", "GET", undefined, signal),
  });
  const accounts = computed(() => query.data.value || []);
  const account = computed(() => {
    const id = route.query.account;
    return id ? accounts.value.find((item) => item.id === id) : accounts.value[0];
  });
  const accountId = computed(() => account.value?.id || "");
  function selectAccount(id: string) {
    if (id === accountId.value) return;
    return router.replace({ query: { ...route.query, account: id, page: undefined, ticket: undefined } });
  }
  return { accounts, account, accountId, selectAccount, accountsError: query.error,
    accountsPending: query.isPending, refetchAccounts: query.refetch };
}
