import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "@/lib/api";
import type { Environment } from "@/hooks/useEnvironments";

export type ValuesResponse = {
  version: number;
  values: Record<string, unknown>;
  schemaVersion: number;
};

export function useEnvironment(envId: string) {
  return useQuery<Environment>({
    queryKey: ["environment", envId],
    queryFn: () => api<Environment>(`/v1/environments/${envId}`),
  });
}

export function useValues(envId: string) {
  return useQuery<ValuesResponse | null>({
    queryKey: ["values", envId],
    queryFn: async () => {
      try {
        return await api<ValuesResponse>(`/v1/environments/${envId}/values`);
      } catch (err) {
        // No values saved yet is an expected empty state, not an error.
        if (err instanceof ApiError && err.status === 404) {
          return null;
        }
        throw err;
      }
    },
  });
}

export function useSaveValues(envId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { values: Record<string, unknown> }) =>
      api(`/v1/environments/${envId}/values`, { method: "PUT", body }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["values", envId] });
      qc.invalidateQueries({ queryKey: ["versions", envId] });
    },
  });
}
