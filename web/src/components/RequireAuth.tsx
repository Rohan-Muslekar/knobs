import { Navigate, Outlet } from "react-router-dom";
import { useSession } from "@/hooks/useAuth";
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
    <AppLayout email={data.email}>
      <Outlet />
    </AppLayout>
  );
}
