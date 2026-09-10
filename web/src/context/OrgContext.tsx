import { createContext, useContext, useMemo, useState } from "react";
import type { ReactNode } from "react";
import type { Organization } from "@/hooks/useOrganizations";
import { roleAtLeast } from "@/lib/roles";
import type { Role } from "@/lib/roles";

const STORAGE_KEY = "knobs.currentOrgId";

function readStoredOrgId(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY);
  } catch {
    return null;
  }
}

function writeStoredOrgId(id: string) {
  try {
    localStorage.setItem(STORAGE_KEY, id);
  } catch {
    // Storage unavailable (private browsing, disabled site data, ...) — the
    // selection just won't survive a reload.
  }
}

type OrgContextValue = {
  organizations: Organization[];
  currentOrg: Organization | null;
  role: Role | null;
  setCurrentOrgId: (id: string) => void;
  can: (min: Role) => boolean;
};

const OrgContext = createContext<OrgContextValue | null>(null);

export function OrgProvider({ organizations, children }: { organizations: Organization[]; children: ReactNode }) {
  const [orgId, setOrgId] = useState<string | null>(readStoredOrgId);

  const currentOrg = useMemo(
    () => organizations.find((org) => org.id === orgId) ?? organizations[0] ?? null,
    [organizations, orgId],
  );

  const role = currentOrg?.role ?? null;

  const value = useMemo<OrgContextValue>(
    () => ({
      organizations,
      currentOrg,
      role,
      setCurrentOrgId: (id: string) => {
        setOrgId(id);
        writeStoredOrgId(id);
      },
      can: (min: Role) => roleAtLeast(role, min),
    }),
    [organizations, currentOrg, role],
  );

  return <OrgContext.Provider value={value}>{children}</OrgContext.Provider>;
}

export function useOrg(): OrgContextValue {
  const ctx = useContext(OrgContext);
  if (!ctx) throw new Error("useOrg must be used within an OrgProvider");
  return ctx;
}

export function useCurrentOrg(): Organization | null {
  return useOrg().currentOrg;
}

export function useRole(): Role | null {
  return useOrg().role;
}

export function useCan(): (min: Role) => boolean {
  return useOrg().can;
}
