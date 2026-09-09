import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type FieldType = "string" | "int" | "float" | "bool" | "json" | "enum" | "duration";

export type SchemaField = {
  name: string;
  type: FieldType;
  required: boolean;
  default?: unknown;
  min?: number;
  max?: number;
  pattern?: string;
  enumValues?: unknown[];
  description?: string;
};

export type SchemaDefinition = { fields: SchemaField[] };

export function useSchema(projectId: string) {
  return useQuery<{ definition: SchemaDefinition; schemaVersion: number }>({
    queryKey: ["schema", projectId],
    queryFn: () => api(`/v1/projects/${projectId}/schema`),
  });
}

export function useUpdateSchema(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { fields: SchemaField[] }) =>
      api(`/v1/projects/${projectId}/schema`, { method: "PUT", body }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["schema", projectId] });
    },
  });
}
