import { screen, waitFor } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { server, http, HttpResponse } from "@/test/msw";
import { AuditPage } from "@/pages/AuditPage";

function renderAuditPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter([{ path: "/projects/:projectId/audit", element: <AuditPage /> }], {
    initialEntries: ["/projects/p1/audit"],
  });
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

const projectHandler = http.get("/v1/projects/p1", () =>
  HttpResponse.json({ id: "p1", name: "Acme", slug: "acme", createdAt: "2026-01-01T00:00:00Z" }),
);

describe("AuditPage", () => {
  it("renders audit entries newest-first", async () => {
    server.use(
      projectHandler,
      http.get("/v1/projects/p1/audit", () =>
        HttpResponse.json([
          {
            id: "a3",
            actor: "alice",
            action: "values.rollback",
            target: "environments/e1",
            diff: { version: 1 },
            at: "2026-01-03T00:00:00Z",
          },
          {
            id: "a2",
            actor: "bob",
            action: "values.update",
            target: "environments/e1",
            diff: { maxRetries: 5 },
            at: "2026-01-02T00:00:00Z",
          },
          {
            id: "a1",
            actor: "carol",
            action: "schema.update",
            target: "projects/p1",
            diff: { fields: ["maxRetries"] },
            at: "2026-01-01T00:00:00Z",
          },
        ]),
      ),
    );
    renderAuditPage();

    await waitFor(() => expect(screen.getByText("values.rollback")).toBeInTheDocument());

    const rows = screen.getAllByRole("row").slice(1); // drop header row
    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveTextContent("values.rollback");
    expect(rows[1]).toHaveTextContent("values.update");
    expect(rows[2]).toHaveTextContent("schema.update");
  });

  it("shows the empty state when there is no activity", async () => {
    server.use(projectHandler, http.get("/v1/projects/p1/audit", () => HttpResponse.json([])));
    renderAuditPage();

    await waitFor(() => expect(screen.getByText(/no activity yet/i)).toBeInTheDocument());
  });

  it("shows a project-not-found state on 404", async () => {
    server.use(
      http.get("/v1/projects/p1", () => HttpResponse.json({ error: "project not found" }, { status: 404 })),
      http.get("/v1/projects/p1/audit", () => HttpResponse.json([])),
    );
    renderAuditPage();

    await waitFor(() => expect(screen.getByText(/project not found/i)).toBeInTheDocument());
  });
});
