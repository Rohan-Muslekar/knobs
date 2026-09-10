import { useState } from "react";
import type { ReactNode } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useLogout } from "@/hooks/useAuth";
import { useOrg } from "@/context/OrgContext";
import { CreateOrganizationDialog } from "@/components/CreateOrganizationDialog";

export function AppLayout({ email, children }: { email: string; children: ReactNode }) {
  const navigate = useNavigate();
  const logout = useLogout();
  const { organizations, currentOrg, setCurrentOrgId } = useOrg();
  const [createOrgOpen, setCreateOrgOpen] = useState(false);

  return (
    <div className="min-h-screen">
      <header className="flex items-center justify-between border-b px-6 py-3">
        <div className="flex items-center gap-6">
          <span className="font-semibold">Knobs</span>
          <nav className="flex items-center gap-4 text-sm">
            <Link to="/" className="text-muted-foreground hover:text-foreground">
              Projects
            </Link>
            {currentOrg && (
              <Link to={`/organizations/${currentOrg.id}/members`} className="text-muted-foreground hover:text-foreground">
                Members
              </Link>
            )}
          </nav>
        </div>
        <div className="flex items-center gap-3 text-sm">
          {currentOrg && (
            <Select value={currentOrg.id} onValueChange={(value) => value && setCurrentOrgId(value)}>
              <SelectTrigger size="sm" aria-label="Organization">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {organizations.map((org) => (
                  <SelectItem key={org.id} value={org.id}>
                    {org.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
          <Button variant="outline" size="sm" onClick={() => setCreateOrgOpen(true)}>
            New organization
          </Button>
          <span className="text-muted-foreground">{email}</span>
          <Button
            variant="outline"
            size="sm"
            onClick={() => logout.mutate(undefined, { onSettled: () => navigate("/login") })}
          >
            Log out
          </Button>
        </div>
      </header>
      <main className="mx-auto max-w-5xl px-6 py-8">{children}</main>
      <CreateOrganizationDialog
        open={createOrgOpen}
        onOpenChange={setCreateOrgOpen}
        onCreated={(org) => setCurrentOrgId(org.id)}
      />
    </div>
  );
}
