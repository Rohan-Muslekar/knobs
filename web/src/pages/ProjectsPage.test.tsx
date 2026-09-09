import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect } from "vitest";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { server, http, HttpResponse } from "@/test/msw";
import { ProjectsPage } from "@/pages/ProjectsPage";

function renderProjects() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter([{ path: "/", element: <ProjectsPage /> }], { initialEntries: ["/"] });
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

describe("ProjectsPage", () => {
  it("lists projects", async () => {
    server.use(
      http.get("/v1/projects", () =>
        HttpResponse.json([
          { id: "p1", name: "Acme", slug: "acme", createdAt: "2026-01-01T00:00:00Z" },
          { id: "p2", name: "Globex", slug: "globex", createdAt: "2026-01-02T00:00:00Z" },
        ]),
      ),
    );
    renderProjects();
    await waitFor(() => expect(screen.getByText("Acme")).toBeInTheDocument());
    expect(screen.getByText("Globex")).toBeInTheDocument();
  });

  it("shows an empty state", async () => {
    server.use(http.get("/v1/projects", () => HttpResponse.json([])));
    renderProjects();
    await waitFor(() => expect(screen.getByText(/no projects yet/i)).toBeInTheDocument());
  });

  it("creates a project and the new row appears after refetch", async () => {
    let created = false;
    server.use(
      http.get("/v1/projects", () =>
        HttpResponse.json(
          created ? [{ id: "p3", name: "NewCo", slug: "newco", createdAt: "2026-01-03T00:00:00Z" }] : [],
        ),
      ),
      http.post("/v1/projects", async ({ request }) => {
        const body = (await request.json()) as { name: string; slug: string };
        created = true;
        return HttpResponse.json({ id: "p3", ...body, createdAt: "2026-01-03T00:00:00Z" });
      }),
    );
    renderProjects();
    await waitFor(() => expect(screen.getByText(/no projects yet/i)).toBeInTheDocument());

    await userEvent.click(screen.getByRole("button", { name: /new project/i }));
    await userEvent.type(screen.getByLabelText(/name/i), "NewCo");
    await userEvent.type(screen.getByLabelText(/slug/i), "newco");
    await userEvent.click(screen.getByRole("button", { name: /create/i }));

    await waitFor(() => expect(screen.getByText("NewCo")).toBeInTheDocument());
    // Dialog closed: its title is no longer on screen.
    expect(screen.queryByText("New project", { selector: "[data-slot=dialog-title]" })).toBeNull();
  });

  it("shows an inline slug error on 409 and keeps the dialog open", async () => {
    server.use(
      http.get("/v1/projects", () => HttpResponse.json([])),
      http.post("/v1/projects", () => HttpResponse.json({ error: "slug already exists" }, { status: 409 })),
    );
    renderProjects();
    await waitFor(() => expect(screen.getByText(/no projects yet/i)).toBeInTheDocument());

    await userEvent.click(screen.getByRole("button", { name: /new project/i }));
    await userEvent.type(screen.getByLabelText(/name/i), "Dup");
    await userEvent.type(screen.getByLabelText(/slug/i), "dup");
    await userEvent.click(screen.getByRole("button", { name: /create/i }));

    await waitFor(() => expect(screen.getByText(/slug already exists/i)).toBeInTheDocument());
    // Dialog stays open on failure.
    expect(screen.getByLabelText(/slug/i)).toBeInTheDocument();
  });
});
