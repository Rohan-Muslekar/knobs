import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { server, http, HttpResponse } from "@/test/msw";
import { VersionHistory } from "@/components/VersionHistory";
import { Toaster } from "@/components/ui/sonner";
import { OrgProvider } from "@/context/OrgContext";
import type { Role } from "@/lib/roles";

function renderVersionHistory(envId = "e1", role: Role = "editor") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <OrgProvider organizations={[{ id: "org1", name: "Acme", slug: "acme", role }]}>
        <VersionHistory envId={envId} />
        <Toaster />
      </OrgProvider>
    </QueryClientProvider>,
  );
}

const twoVersions = (current: number) => [
  {
    id: "v2",
    version: 2,
    schemaVersion: 1,
    createdBy: "alice",
    createdAt: "2026-01-02T00:00:00Z",
    isCurrent: current === 2,
  },
  {
    id: "v1",
    version: 1,
    schemaVersion: 1,
    createdBy: "alice",
    createdAt: "2026-01-01T00:00:00Z",
    isCurrent: current === 1,
  },
];

describe("VersionHistory", () => {
  it("renders both versions with a current badge on the current row", async () => {
    server.use(http.get("/v1/environments/e1/versions", () => HttpResponse.json(twoVersions(2))));

    renderVersionHistory();

    await waitFor(() => expect(screen.getAllByRole("row")).toHaveLength(3)); // header + 2 rows
    expect(screen.getByText(/current/i)).toBeInTheDocument();
    // only the non-current row (v1) gets a roll back button
    expect(screen.getAllByRole("button", { name: /roll back/i })).toHaveLength(1);
  });

  it("shows an empty state when there is no version history", async () => {
    server.use(http.get("/v1/environments/e1/versions", () => HttpResponse.json([])));

    renderVersionHistory();

    await waitFor(() => expect(screen.getByText(/no version history/i)).toBeInTheDocument());
  });

  it("rolls back to a prior version through the confirm dialog; POST fires with {version} and the list refetches", async () => {
    let current = 2;
    let rollbackBody: { version: number } | null = null;
    server.use(
      http.get("/v1/environments/e1/versions", () => HttpResponse.json(twoVersions(current))),
      http.post("/v1/environments/e1/rollback", async ({ request }) => {
        rollbackBody = (await request.json()) as { version: number };
        current = rollbackBody.version;
        return HttpResponse.json({ status: "ok" });
      }),
    );

    renderVersionHistory();
    await waitFor(() => expect(screen.getAllByRole("row")).toHaveLength(3));

    await userEvent.click(screen.getByRole("button", { name: /roll back/i }));

    expect(await screen.findByText(/roll back to version 1\?/i)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /confirm/i }));

    await waitFor(() => expect(rollbackBody).toEqual({ version: 1 }));
    await waitFor(() => expect(screen.getByText(/rolled back/i)).toBeInTheDocument());

    // after refetch, v1 is now current so its roll back button disappears and v2's appears
    await waitFor(() => expect(screen.getAllByRole("button", { name: /roll back/i })).toHaveLength(1));
  });

  it("shows a visible error when rollback fails", async () => {
    server.use(
      http.get("/v1/environments/e1/versions", () => HttpResponse.json(twoVersions(2))),
      http.post("/v1/environments/e1/rollback", () =>
        HttpResponse.json({ error: "rollback failed" }, { status: 500 }),
      ),
    );

    renderVersionHistory();
    await waitFor(() => expect(screen.getAllByRole("row")).toHaveLength(3));

    await userEvent.click(screen.getByRole("button", { name: /roll back/i }));
    await userEvent.click(await screen.findByRole("button", { name: /confirm/i }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(/rollback failed/i));
  });

  it("hides the roll back button for a viewer", async () => {
    server.use(http.get("/v1/environments/e1/versions", () => HttpResponse.json(twoVersions(2))));

    renderVersionHistory("e1", "viewer");

    await waitFor(() => expect(screen.getAllByRole("row")).toHaveLength(3));
    expect(screen.queryByRole("button", { name: /roll back/i })).toBeNull();
  });
});
