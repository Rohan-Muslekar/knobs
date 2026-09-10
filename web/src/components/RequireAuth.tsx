import { Navigate, Outlet } from "react-router-dom";
import { useSession } from "@/hooks/useAuth";
import { OrgProvider } from "@/context/OrgContext";
import { AppLayout } from "./AppLayout";

export function RequireAuth() {
  const { data, isPending, isError } = useSession();
  if (isPending) {
    return <div className="flex h-screen items-center justify-center text-muted-foreground">Loading…</div>;
  }
  if (isError || !data) {
    return <Navigate to="/login" replace />;
  }
  return (
    <OrgProvider organizations={data.organizations}>
      <AppLayout email={data.email}>
        <Outlet />
      </AppLayout>
    </OrgProvider>
  );
}
