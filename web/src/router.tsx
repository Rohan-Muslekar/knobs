import { createBrowserRouter } from "react-router-dom";
import { RequireAuth } from "@/components/RequireAuth";
import { LoginPage } from "@/pages/LoginPage";
import { ProjectsPage } from "@/pages/ProjectsPage";
import { ProjectDetailPage } from "@/pages/ProjectDetailPage";
import { SchemaBuilderPage } from "@/pages/SchemaBuilderPage";
import { EnvironmentPage } from "@/pages/EnvironmentPage";
import { AuditPage } from "@/pages/AuditPage";

export const router = createBrowserRouter([
  { path: "/login", element: <LoginPage /> },
  {
    element: <RequireAuth />,
    children: [
      { path: "/", element: <ProjectsPage /> },
      { path: "/projects/:projectId", element: <ProjectDetailPage /> },
      { path: "/projects/:projectId/schema", element: <SchemaBuilderPage /> },
      { path: "/projects/:projectId/audit", element: <AuditPage /> },
      { path: "/environments/:envId", element: <EnvironmentPage /> },
    ],
  },
]);
