import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect } from "vitest";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { server, http, HttpResponse } from "@/test/msw";
import { ProjectDetailPage } from "@/pages/ProjectDetailPage";
import { OrgProvider } from "@/context/OrgContext";
import type { Role } from "@/lib/roles";

function renderProjectDetail(role: Role = "admin") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter([{ path: "/projects/:projectId", element: <ProjectDetailPage /> }], {
    initialEntries: ["/projects/p1"],
  });
  return render(
    <QueryClientProvider client={client}>
      <OrgProvider organizations={[{ id: "org1", name: "Acme", slug: "acme", role }]}>
        <RouterProvider router={router} />
      </OrgProvider>
    </QueryClientProvider>,
  );
}

describe("ProjectDetailPage", () => {
  it("renders the project name and both environment names", async () => {
    server.use(
      http.get("/v1/projects/p1", () =>
        HttpResponse.json({ id: "p1", name: "Acme", slug: "acme", createdAt: "2026-01-01T00:00:00Z" }),
      ),
      http.get("/v1/projects/p1/environments", () =>
        HttpResponse.json([
          { id: "e1", projectId: "p1", name: "production", currentVersionId: null, createdAt: "2026-01-01T00:00:00Z" },
          { id: "e2", projectId: "p1", name: "staging", currentVersionId: null, createdAt: "2026-01-02T00:00:00Z" },
        ]),
      ),
    );
    renderProjectDetail();
    await waitFor(() => expect(screen.getByText("Acme")).toBeInTheDocument());
    expect(screen.getByText("production")).toBeInTheDocument();
    expect(screen.getByText("staging")).toBeInTheDocument();

    const configureLinks = screen.getAllByRole("link", { name: /configure/i });
    expect(configureLinks).toHaveLength(2);
    expect(configureLinks[0]).toHaveAttribute("href", "/environments/e1");
    expect(configureLinks[1]).toHaveAttribute("href", "/environments/e2");
    configureLinks.forEach((link) => expect(link).not.toHaveAttribute("aria-disabled"));

    expect(screen.getByRole("link", { name: /edit schema/i })).toHaveAttribute("href", "/projects/p1/schema");
    expect(screen.getByRole("link", { name: /audit log/i })).toHaveAttribute("href", "/projects/p1/audit");
  });

  it("creates an environment and the new row appears after refetch", async () => {
    let created = false;
    server.use(
      http.get("/v1/projects/p1", () =>
        HttpResponse.json({ id: "p1", name: "Acme", slug: "acme", createdAt: "2026-01-01T00:00:00Z" }),
      ),
      http.get("/v1/projects/p1/environments", () =>
        HttpResponse.json(
          created
            ? [{ id: "e3", projectId: "p1", name: "qa", currentVersionId: null, createdAt: "2026-01-03T00:00:00Z" }]
            : [],
        ),
      ),
      http.post("/v1/projects/p1/environments", async ({ request }) => {
        const body = (await request.json()) as { name: string };
        created = true;
        return HttpResponse.json({
          id: "e3",
          projectId: "p1",
          name: body.name,
          currentVersionId: null,
          createdAt: "2026-01-03T00:00:00Z",
        });
      }),
    );
    renderProjectDetail();
    await waitFor(() => expect(screen.getByText(/no environments yet/i)).toBeInTheDocument());

    await userEvent.click(screen.getByRole("button", { name: /new environment/i }));
    await userEvent.type(screen.getByLabelText(/name/i), "qa");
    await userEvent.click(screen.getByRole("button", { name: /create/i }));

    await waitFor(() => expect(screen.getByText("qa")).toBeInTheDocument());
    // Dialog closed: its title is no longer on screen.
    expect(screen.queryByText("New environment", { selector: "[data-slot=dialog-title]" })).toBeNull();
  });

  it("shows an inline name error on 409 and keeps the dialog open", async () => {
    server.use(
      http.get("/v1/projects/p1", () =>
        HttpResponse.json({ id: "p1", name: "Acme", slug: "acme", createdAt: "2026-01-01T00:00:00Z" }),
      ),
      http.get("/v1/projects/p1/environments", () => HttpResponse.json([])),
      http.post("/v1/projects/p1/environments", () =>
        HttpResponse.json({ error: "environment already exists" }, { status: 409 }),
      ),
    );
    renderProjectDetail();
    await waitFor(() => expect(screen.getByText(/no environments yet/i)).toBeInTheDocument());

    await userEvent.click(screen.getByRole("button", { name: /new environment/i }));
    await userEvent.type(screen.getByLabelText(/name/i), "production");
    await userEvent.click(screen.getByRole("button", { name: /create/i }));

    await waitFor(() => expect(screen.getByText(/environment already exists/i)).toBeInTheDocument());
    // Dialog stays open on failure.
    expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
  });

  it("shows a project-not-found state on 404", async () => {
    server.use(
      http.get("/v1/projects/p1", () => HttpResponse.json({ error: "project not found" }, { status: 404 })),
      http.get("/v1/projects/p1/environments", () => HttpResponse.json([])),
    );
    renderProjectDetail();
    await waitFor(() => expect(screen.getByText(/project not found/i)).toBeInTheDocument());
  });
});
