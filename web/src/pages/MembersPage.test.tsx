import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect } from "vitest";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { server, http, HttpResponse } from "@/test/msw";
import { MembersPage } from "@/pages/MembersPage";
import { OrgProvider } from "@/context/OrgContext";
import type { Organization } from "@/hooks/useOrganizations";

function renderMembers(orgId: string, role: Organization["role"]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter([{ path: "/organizations/:orgId/members", element: <MembersPage /> }], {
    initialEntries: [`/organizations/${orgId}/members`],
  });
  return render(
    <QueryClientProvider client={client}>
      <OrgProvider organizations={[{ id: orgId, name: "Acme", slug: "acme", role }]}>
        <RouterProvider router={router} />
      </OrgProvider>
    </QueryClientProvider>,
  );
}

describe("MembersPage", () => {
  it("lists members and shows role/remove controls for an owner", async () => {
    server.use(
      http.get("/v1/organizations/org1/members", () =>
        HttpResponse.json([
          { userId: "u1", email: "owner@x.com", role: "owner", createdAt: "2026-01-01T00:00:00Z" },
          { userId: "u2", email: "viewer@x.com", role: "viewer", createdAt: "2026-01-02T00:00:00Z" },
        ]),
      ),
    );

    renderMembers("org1", "owner");

    await waitFor(() => expect(screen.getByText("owner@x.com")).toBeInTheDocument());
    expect(screen.getByText("viewer@x.com")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: /remove/i })).toHaveLength(2);
    expect(screen.getAllByRole("combobox").length).toBeGreaterThanOrEqual(2);
  });

  it("hides role and remove controls for a non-owner (admin)", async () => {
    server.use(
      http.get("/v1/organizations/org1/members", () =>
        HttpResponse.json([{ userId: "u1", email: "owner@x.com", role: "owner", createdAt: "2026-01-01T00:00:00Z" }]),
      ),
    );

    renderMembers("org1", "admin");

    await waitFor(() => expect(screen.getByText("owner@x.com")).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: /remove/i })).toBeNull();
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(screen.getByText("owner")).toBeInTheDocument();
  });

  it("shows the temporary password once after adding a newly-provisioned member", async () => {
    server.use(
      http.get("/v1/organizations/org1/members", () => HttpResponse.json([])),
      http.post("/v1/organizations/org1/members", () =>
        HttpResponse.json({ userId: "u3", email: "new@x.com", role: "viewer", temporaryPassword: "s3cret-pass" }),
      ),
    );

    renderMembers("org1", "owner");
    await waitFor(() => expect(screen.getByText(/no members yet/i)).toBeInTheDocument());

    await userEvent.click(screen.getByRole("button", { name: /add member/i }));
    await userEvent.type(screen.getByLabelText(/email/i), "new@x.com");
    await userEvent.click(screen.getByRole("button", { name: /^add$/i }));

    await waitFor(() => expect(screen.getByDisplayValue("s3cret-pass")).toBeInTheDocument());
    expect(screen.getByText(/shown once/i)).toBeInTheDocument();
    // The add-member dialog itself has closed in favor of the password dialog.
    expect(screen.queryByText("Add member", { selector: "[data-slot=dialog-title]" })).toBeNull();
  });

  it("surfaces a 409 (already a member) as a readable error and keeps the dialog open", async () => {
    server.use(
      http.get("/v1/organizations/org1/members", () => HttpResponse.json([])),
      http.post("/v1/organizations/org1/members", () =>
        HttpResponse.json({ error: "already a member" }, { status: 409 }),
      ),
    );

    renderMembers("org1", "owner");
    await waitFor(() => expect(screen.getByText(/no members yet/i)).toBeInTheDocument());

    await userEvent.click(screen.getByRole("button", { name: /add member/i }));
    await userEvent.type(screen.getByLabelText(/email/i), "dup@x.com");
    await userEvent.click(screen.getByRole("button", { name: /^add$/i }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(/already a member/i));
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
  });

  it("hides Add member for a viewer", async () => {
    server.use(http.get("/v1/organizations/org1/members", () => HttpResponse.json([])));

    renderMembers("org1", "viewer");
    await waitFor(() => expect(screen.getByText(/no members yet/i)).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: /add member/i })).toBeNull();
  });

  it("hides Add member for an editor", async () => {
    server.use(http.get("/v1/organizations/org1/members", () => HttpResponse.json([])));

    renderMembers("org1", "editor");
    await waitFor(() => expect(screen.getByText(/no members yet/i)).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: /add member/i })).toBeNull();
  });

  it("shows Add member for an admin, and only offers roles up to the caller's own", async () => {
    server.use(http.get("/v1/organizations/org1/members", () => HttpResponse.json([])));

    renderMembers("org1", "admin");
    await waitFor(() => expect(screen.getByText(/no members yet/i)).toBeInTheDocument());

    await userEvent.click(screen.getByRole("button", { name: /add member/i }));
    await userEvent.click(screen.getByRole("combobox", { name: "Role" }));
    expect(await screen.findByRole("option", { name: "admin" })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "owner" })).toBeNull();
  });
});
