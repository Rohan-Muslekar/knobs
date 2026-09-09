import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type Project = { id: string; name: string; slug: string; createdAt: string };

export type Environment = {
  id: string;
  projectId: string;
  name: string;
  currentVersionId: string | null;
  createdAt: string;
};

export function useProject(projectId: string) {
  return useQuery<Project>({
    queryKey: ["project", projectId],
    queryFn: () => api<Project>(`/v1/projects/${projectId}`),
  });
}

export function useEnvironments(projectId: string) {
  return useQuery<Environment[]>({
    queryKey: ["environments", projectId],
    queryFn: () => api<Environment[]>(`/v1/projects/${projectId}/environments`),
  });
}

export function useCreateEnvironment(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (environment: { name: string }) =>
      api<Environment>(`/v1/projects/${projectId}/environments`, { method: "POST", body: environment }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["environments", projectId] });
    },
  });
}
