import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type AuditEntry = {
  id: string;
  actor: string;
  action: string;
  target: string;
  diff: unknown;
  at: string;
};

export function useAudit(projectId: string) {
  return useQuery<AuditEntry[]>({
    queryKey: ["audit", projectId],
    queryFn: () => api<AuditEntry[]>(`/v1/projects/${projectId}/audit`),
  });
}
