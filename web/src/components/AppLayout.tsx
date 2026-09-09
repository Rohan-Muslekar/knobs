import type { ReactNode } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { useLogout } from "@/hooks/useAuth";

export function AppLayout({ email, children }: { email: string; children: ReactNode }) {
  const navigate = useNavigate();
  const logout = useLogout();
  return (
    <div className="min-h-screen">
      <header className="flex items-center justify-between border-b px-6 py-3">
        <div className="flex items-center gap-6">
          <span className="font-semibold">Knobs</span>
          <nav className="text-sm">
            <Link to="/" className="text-muted-foreground hover:text-foreground">
              Projects
            </Link>
          </nav>
        </div>
        <div className="flex items-center gap-3 text-sm">
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
    </div>
  );
}
