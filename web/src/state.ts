import { QueryClient } from "@tanstack/vue-query";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 5000, retry: false, refetchOnWindowFocus: true },
  },
});
export const refreshAccounts = () =>
  queryClient.invalidateQueries({
    predicate: (q) =>
      ["overview", "account", "snapshots", "servers", "funds", "equity-history"].includes(
        String(q.queryKey[0]),
      ),
  });
