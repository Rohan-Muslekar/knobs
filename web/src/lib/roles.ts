// Client-side mirror of the server's role hierarchy. This is a UX
// convenience only — hiding controls the caller can't use — not a security
// boundary; the server enforces the real authorization on every request.
export type Role = "viewer" | "editor" | "admin" | "owner";

const ORDER: Record<Role, number> = {
  viewer: 0,
  editor: 1,
  admin: 2,
  owner: 3,
};

export function roleAtLeast(role: Role | null | undefined, min: Role): boolean {
  if (!role) return false;
  return ORDER[role] >= ORDER[min];
}
