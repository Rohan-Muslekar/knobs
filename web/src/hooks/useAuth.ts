import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Organization } from "@/hooks/useOrganizations";

export type SessionUser = { id: string; email: string; organizations: Organization[] };

export function useSession() {
  return useQuery<SessionUser>({
    queryKey: ["session"],
    queryFn: () => api<SessionUser>("/v1/auth/me"),
    retry: false,
  });
}

export function useLogin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (creds: { email: string; password: string }) =>
      api<SessionUser>("/v1/auth/login", { method: "POST", body: creds }),
    onSuccess: (user) => {
      qc.setQueryData(["session"], user);
    },
  });
}

export function useLogout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api("/v1/auth/logout", { method: "POST" }),
    onSuccess: () => {
      qc.clear();
    },
  });
}
