import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type EnvironmentVersion = {
  id: string;
  version: number;
  schemaVersion: number;
  createdBy: string;
  createdAt: string;
  isCurrent: boolean;
};

export function useVersions(envId: string) {
  return useQuery<EnvironmentVersion[]>({
    queryKey: ["versions", envId],
    queryFn: () => api<EnvironmentVersion[]>(`/v1/environments/${envId}/versions`),
  });
}

export function useRollback(envId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { version: number }) =>
      api(`/v1/environments/${envId}/rollback`, { method: "POST", body }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["values", envId] });
      qc.invalidateQueries({ queryKey: ["versions", envId] });
    },
  });
}
