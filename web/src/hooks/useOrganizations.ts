import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Role } from "@/lib/roles";

export type Organization = { id: string; name: string; slug: string; role: Role };

export type Member = { userId: string; email: string; role: Role; createdAt: string };

// If the email didn't already belong to a user, the server provisions one
// and returns a one-time temporary password alongside it.
export type AddMemberResult = { userId: string; email: string; role: Role; temporaryPassword?: string };

export function useOrganizations() {
  return useQuery<Organization[]>({
    queryKey: ["organizations"],
    queryFn: () => api<Organization[]>("/v1/organizations"),
  });
}

export function useCreateOrganization() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (org: { name: string }) => api<Organization>("/v1/organizations", { method: "POST", body: org }),
    onSuccess: () => {
      // The org list lives on the session payload as well as its own
      // endpoint, so both need to reflect the new org.
      qc.invalidateQueries({ queryKey: ["session"] });
      qc.invalidateQueries({ queryKey: ["organizations"] });
    },
  });
}

export function useMembers(orgId: string) {
  return useQuery<Member[]>({
    queryKey: ["members", orgId],
    queryFn: () => api<Member[]>(`/v1/organizations/${orgId}/members`),
    enabled: orgId.length > 0,
  });
}

export function useAddMember(orgId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { email: string; role: Role }) =>
      api<AddMemberResult>(`/v1/organizations/${orgId}/members`, { method: "POST", body }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["members", orgId] });
    },
  });
}

export function useUpdateMemberRole(orgId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ userId, role }: { userId: string; role: Role }) =>
      api(`/v1/organizations/${orgId}/members/${userId}`, { method: "PATCH", body: { role } }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["members", orgId] });
    },
  });
}

export function useRemoveMember(orgId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (userId: string) => api(`/v1/organizations/${orgId}/members/${userId}`, { method: "DELETE" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["members", orgId] });
    },
  });
}
