import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect } from "vitest";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { server, http, HttpResponse } from "@/test/msw";
import { EnvironmentPage } from "@/pages/EnvironmentPage";
import { Toaster } from "@/components/ui/sonner";
import { OrgProvider } from "@/context/OrgContext";
import type { Role } from "@/lib/roles";

function renderEnvironmentPage(role: Role = "editor") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter([{ path: "/environments/:envId", element: <EnvironmentPage /> }], {
    initialEntries: ["/environments/e1"],
  });
  return render(
    <QueryClientProvider client={client}>
      <OrgProvider organizations={[{ id: "org1", name: "Acme", slug: "acme", role }]}>
        <RouterProvider router={router} />
        <Toaster />
      </OrgProvider>
    </QueryClientProvider>,
  );
}

const envHandler = http.get("/v1/environments/e1", () =>
  HttpResponse.json({
    id: "e1",
    projectId: "p1",
    name: "production",
    currentVersionId: "ver1",
    createdAt: "2026-01-01T00:00:00Z",
  }),
);

const schemaHandler = http.get("/v1/projects/p1/schema", () =>
  HttpResponse.json({
    definition: {
      fields: [
        { name: "maxRetries", type: "int", required: true },
        { name: "featureX", type: "bool", required: false },
      ],
    },
    schemaVersion: 1,
  }),
);

const versionsHandler = http.get("/v1/environments/e1/versions", () =>
  HttpResponse.json([
    { id: "v1", version: 1, schemaVersion: 1, createdBy: "alice", createdAt: "2026-01-01T00:00:00Z", isCurrent: true },
  ]),
);

describe("EnvironmentPage", () => {
  it("renders both fields pre-filled from the current values", async () => {
    server.use(
      envHandler,
      versionsHandler,
      schemaHandler,
      http.get("/v1/environments/e1/values", () =>
        HttpResponse.json({ version: 1, values: { maxRetries: 3, featureX: true }, schemaVersion: 1 }),
      ),
    );
    renderEnvironmentPage();

    await waitFor(() => expect(screen.getByLabelText("maxRetries")).toHaveValue(3));
    expect(screen.getByRole("switch", { name: "featureX" })).toBeChecked();
  });

  it("edits a value and saves; PUT fires with the new values", async () => {
    let saved: { values: Record<string, unknown> } | null = null;
    server.use(
      envHandler,
      versionsHandler,
      schemaHandler,
      http.get("/v1/environments/e1/values", () =>
        HttpResponse.json({ version: 1, values: { maxRetries: 3, featureX: true }, schemaVersion: 1 }),
      ),
      http.put("/v1/environments/e1/values", async ({ request }) => {
        saved = (await request.json()) as { values: Record<string, unknown> };
        return HttpResponse.json({ version: 2, values: saved.values, schemaVersion: 1 });
      }),
    );
    renderEnvironmentPage();
    await waitFor(() => expect(screen.getByLabelText("maxRetries")).toHaveValue(3));

    await userEvent.clear(screen.getByLabelText("maxRetries"));
    await userEvent.type(screen.getByLabelText("maxRetries"), "5");
    await userEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(saved).toEqual({ values: { maxRetries: 5, featureX: true } }));
    await waitFor(() => expect(screen.getByText(/values saved/i)).toBeInTheDocument());
  });

  it("shows the server validation message on a 422", async () => {
    server.use(
      envHandler,
      versionsHandler,
      schemaHandler,
      http.get("/v1/environments/e1/values", () =>
        HttpResponse.json({ version: 1, values: { maxRetries: 3, featureX: true }, schemaVersion: 1 }),
      ),
      http.put("/v1/environments/e1/values", () =>
        HttpResponse.json({ error: "maxRetries: must be <= 5" }, { status: 422 }),
      ),
    );
    renderEnvironmentPage();
    await waitFor(() => expect(screen.getByLabelText("maxRetries")).toHaveValue(3));

    await userEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(/must be <= 5/i));
  });

  it("starts with an empty form when there are no values yet (404)", async () => {
    server.use(
      envHandler,
      versionsHandler,
      schemaHandler,
      http.get("/v1/environments/e1/values", () => HttpResponse.json({ error: "not found" }, { status: 404 })),
    );
    renderEnvironmentPage();

    await waitFor(() => expect(screen.getByLabelText("maxRetries")).toBeInTheDocument());
    expect(screen.getByLabelText("maxRetries")).toHaveValue(null);
    expect(screen.getByRole("switch", { name: "featureX" })).not.toBeChecked();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("changes an enum value and saves; PUT fires with the new value", async () => {
    let saved: { values: Record<string, unknown> } | null = null;
    server.use(
      envHandler,
      versionsHandler,
      http.get("/v1/projects/p1/schema", () =>
        HttpResponse.json({
          definition: { fields: [{ name: "tier", type: "enum", required: true, enumValues: ["gold", "silver"] }] },
          schemaVersion: 1,
        }),
      ),
      http.get("/v1/environments/e1/values", () =>
        HttpResponse.json({ version: 1, values: { tier: "gold" }, schemaVersion: 1 }),
      ),
      http.put("/v1/environments/e1/values", async ({ request }) => {
        saved = (await request.json()) as { values: Record<string, unknown> };
        return HttpResponse.json({ version: 2, values: saved.values, schemaVersion: 1 });
      }),
    );
    renderEnvironmentPage();

    await waitFor(() => expect(screen.getByRole("combobox", { name: "tier" })).toBeInTheDocument());

    await userEvent.click(screen.getByRole("combobox", { name: "tier" }));
    await userEvent.click(await screen.findByRole("option", { name: "silver" }));

    await userEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(saved).toEqual({ values: { tier: "silver" } }));
  });

  it("shows the rolled-back value, not a stale edit, after rolling back a touched field", async () => {
    // Regression test for the edits-masking bug: `edits` used to survive a
    // rollback and keep overriding the freshly refetched server values.
    let current = 2;
    const valuesByVersion: Record<number, Record<string, unknown>> = {
      1: { maxRetries: 3 },
      2: { maxRetries: 5 },
    };
    let rollbackBody: { version: number } | null = null;

    server.use(
      envHandler,
      http.get("/v1/projects/p1/schema", () =>
        HttpResponse.json({
          definition: { fields: [{ name: "maxRetries", type: "int", required: true }] },
          schemaVersion: 1,
        }),
      ),
      http.get("/v1/environments/e1/values", () =>
        HttpResponse.json({ version: current, values: valuesByVersion[current], schemaVersion: 1 }),
      ),
      http.get("/v1/environments/e1/versions", () =>
        HttpResponse.json([
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
        ]),
      ),
      http.post("/v1/environments/e1/rollback", async ({ request }) => {
        rollbackBody = (await request.json()) as { version: number };
        current = rollbackBody.version;
        return HttpResponse.json({ status: "ok" });
      }),
    );

    renderEnvironmentPage();
    await waitFor(() => expect(screen.getByLabelText("maxRetries")).toHaveValue(5));

    // Touch the field without saving, so `edits` is non-empty going into the rollback.
    await userEvent.clear(screen.getByLabelText("maxRetries"));
    await userEvent.type(screen.getByLabelText("maxRetries"), "99");
    await waitFor(() => expect(screen.getByLabelText("maxRetries")).toHaveValue(99));

    await userEvent.click(screen.getByRole("button", { name: /roll back/i }));
    expect(await screen.findByText(/roll back to version 1\?/i)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /confirm/i }));

    await waitFor(() => expect(rollbackBody).toEqual({ version: 1 }));
    // The rolled-back value (3) must win over the stale, never-saved edit (99).
    await waitFor(() => expect(screen.getByLabelText("maxRetries")).toHaveValue(3));
  });

  it("blocks save on invalid JSON in a json field and never calls the API", async () => {
    let putCalled = false;
    server.use(
      envHandler,
      versionsHandler,
      http.get("/v1/projects/p1/schema", () =>
        HttpResponse.json({
          definition: { fields: [{ name: "config", type: "json", required: false }] },
          schemaVersion: 1,
        }),
      ),
      http.get("/v1/environments/e1/values", () =>
        HttpResponse.json({ version: 1, values: { config: { a: 1 } }, schemaVersion: 1 }),
      ),
      http.put("/v1/environments/e1/values", () => {
        putCalled = true;
        return HttpResponse.json({ version: 2, values: {}, schemaVersion: 1 });
      }),
    );
    renderEnvironmentPage();

    await waitFor(() => expect(screen.getByLabelText("config")).toHaveValue('{"a":1}'));

    await userEvent.clear(screen.getByLabelText("config"));
    // "{{" is user-event's escape for a literal "{" — this types the unclosed, invalid JSON `{bad`.
    await userEvent.type(screen.getByLabelText("config"), "{{bad");
    await userEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(/not valid json/i));
    expect(putCalled).toBe(false);
  });

  it("sends targeting rules for a field with content, omitting a field left as `[]`", async () => {
    let saved: { values: Record<string, unknown>; targeting?: Record<string, unknown> } | null = null;
    const rules = [{ conditions: [{ attribute: "country", operator: "in", values: ["US"] }], value: true }];
    server.use(
      envHandler,
      versionsHandler,
      schemaHandler,
      http.get("/v1/environments/e1/values", () =>
        HttpResponse.json({ version: 1, values: { maxRetries: 3, featureX: true }, schemaVersion: 1 }),
      ),
      http.put("/v1/environments/e1/values", async ({ request }) => {
        saved = (await request.json()) as typeof saved;
        return HttpResponse.json({ version: 2, values: saved!.values, schemaVersion: 1 });
      }),
    );
    renderEnvironmentPage();
    await waitFor(() => expect(screen.getByLabelText("maxRetries")).toHaveValue(3));

    // Two fields on this schema -> two "Targeting" disclosures, in field order.
    const toggles = screen.getAllByRole("button", { name: /targeting/i });
    await userEvent.click(toggles[0]); // maxRetries
    fireEvent.change(screen.getByLabelText("maxRetries targeting rules"), {
      target: { value: JSON.stringify(rules) },
    });

    await userEvent.click(toggles[1]); // featureX
    fireEvent.change(screen.getByLabelText("featureX targeting rules"), { target: { value: "[]" } });

    await userEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() =>
      expect(saved).toEqual({ values: { maxRetries: 3, featureX: true }, targeting: { maxRetries: rules } }),
    );
  });

  it("omits targeting from the request body entirely when every field is left blank", async () => {
    let saved: { values: Record<string, unknown>; targeting?: Record<string, unknown> } | null = null;
    server.use(
      envHandler,
      versionsHandler,
      schemaHandler,
      http.get("/v1/environments/e1/values", () =>
        HttpResponse.json({ version: 1, values: { maxRetries: 3, featureX: true }, schemaVersion: 1 }),
      ),
      http.put("/v1/environments/e1/values", async ({ request }) => {
        saved = (await request.json()) as typeof saved;
        return HttpResponse.json({ version: 2, values: saved!.values, schemaVersion: 1 });
      }),
    );
    renderEnvironmentPage();
    await waitFor(() => expect(screen.getByLabelText("maxRetries")).toHaveValue(3));

    await userEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(saved).toEqual({ values: { maxRetries: 3, featureX: true } }));
    expect(saved && "targeting" in saved).toBe(false);
  });

  it("maps a targeting[\"field\"] 422 to that field and shows it even if the section was collapsed", async () => {
    server.use(
      envHandler,
      versionsHandler,
      schemaHandler,
      http.get("/v1/environments/e1/values", () =>
        HttpResponse.json({ version: 1, values: { maxRetries: 3, featureX: true }, schemaVersion: 1 }),
      ),
      http.put("/v1/environments/e1/values", () =>
        HttpResponse.json(
          { error: 'targeting["maxRetries"].rules[0].variants[1].value: weights must sum to 100' },
          { status: 422 },
        ),
      ),
    );
    renderEnvironmentPage();
    await waitFor(() => expect(screen.getByLabelText("maxRetries")).toHaveValue(3));

    const toggles = screen.getAllByRole("button", { name: /targeting/i });
    await userEvent.click(toggles[0]); // open maxRetries' section
    fireEvent.change(screen.getByLabelText("maxRetries targeting rules"), {
      target: { value: '[{"value":true}]' },
    });
    await userEvent.click(toggles[0]); // collapse it again before saving
    expect(screen.queryByLabelText("maxRetries targeting rules")).toBeNull();

    await userEvent.click(screen.getByRole("button", { name: /save/i }));

    // The field-level targeting error must render near the field, not just
    // in the generic top-of-form banner — both should be present, and the
    // field one must show even though its section was collapsed.
    await waitFor(() => expect(screen.getAllByText(/weights must sum to 100/i)).toHaveLength(2));
    expect(screen.getByLabelText("maxRetries targeting rules")).toBeInTheDocument();
  });

  it("hides the Save control for a viewer", async () => {
    server.use(
      envHandler,
      versionsHandler,
      schemaHandler,
      http.get("/v1/environments/e1/values", () =>
        HttpResponse.json({ version: 1, values: { maxRetries: 3, featureX: true }, schemaVersion: 1 }),
      ),
    );
    renderEnvironmentPage("viewer");

    await waitFor(() => expect(screen.getByLabelText("maxRetries")).toHaveValue(3));
    expect(screen.queryByRole("button", { name: /save/i })).toBeNull();
  });
});
