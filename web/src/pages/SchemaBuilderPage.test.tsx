import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect } from "vitest";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { server, http, HttpResponse } from "@/test/msw";
import { SchemaBuilderPage } from "@/pages/SchemaBuilderPage";
import { Toaster } from "@/components/ui/sonner";

function renderSchemaBuilder() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter([{ path: "/projects/:projectId/schema", element: <SchemaBuilderPage /> }], {
    initialEntries: ["/projects/p1/schema"],
  });
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
      <Toaster />
    </QueryClientProvider>,
  );
}

describe("SchemaBuilderPage", () => {
  it("renders the existing field from the schema", async () => {
    server.use(
      http.get("/v1/projects/p1/schema", () =>
        HttpResponse.json({
          definition: { fields: [{ name: "maxRetries", type: "int", required: true }] },
          schemaVersion: 1,
        }),
      ),
    );
    renderSchemaBuilder();
    await waitFor(() => expect(screen.getByDisplayValue("maxRetries")).toBeInTheDocument());
  });

  it("adds a field and saves; PUT fires with the full new field list", async () => {
    let saved: unknown = null;
    server.use(
      http.get("/v1/projects/p1/schema", () =>
        HttpResponse.json({
          definition: { fields: [{ name: "maxRetries", type: "int", required: true }] },
          schemaVersion: 1,
        }),
      ),
      http.put("/v1/projects/p1/schema", async ({ request }) => {
        saved = await request.json();
        return HttpResponse.json({ definition: saved, schemaVersion: 2 });
      }),
    );
    renderSchemaBuilder();
    await waitFor(() => expect(screen.getByDisplayValue("maxRetries")).toBeInTheDocument());

    await userEvent.click(screen.getByRole("button", { name: /add field/i }));
    const rows = screen.getAllByTestId(/field-row-/);
    const newRow = within(rows[rows.length - 1]);
    await userEvent.type(newRow.getByLabelText(/field name/i), "featureX");

    await userEvent.click(screen.getByRole("button", { name: /save schema/i }));

    await waitFor(() =>
      expect(saved).toEqual({
        fields: [
          { name: "maxRetries", type: "int", required: true },
          { name: "featureX", type: "string", required: false },
        ],
      }),
    );
    await waitFor(() => expect(screen.getByText(/schema saved/i)).toBeInTheDocument());
  });

  it("removes the right field from a mid-list Remove and saves the correct remaining fields", async () => {
    let saved: unknown = null;
    server.use(
      http.get("/v1/projects/p1/schema", () =>
        HttpResponse.json({
          definition: {
            fields: [
              { name: "fieldA", type: "string", required: false },
              { name: "fieldB", type: "string", required: false },
              { name: "fieldC", type: "string", required: false },
            ],
          },
          schemaVersion: 1,
        }),
      ),
      http.put("/v1/projects/p1/schema", async ({ request }) => {
        saved = await request.json();
        return HttpResponse.json({ definition: saved, schemaVersion: 2 });
      }),
    );
    renderSchemaBuilder();
    await waitFor(() => expect(screen.getByDisplayValue("fieldB")).toBeInTheDocument());

    // Remove the middle row (fieldB) and confirm the remaining rows are fieldA and fieldC,
    // not index-shifted duplicates/ghosts of fieldB.
    const rows = screen.getAllByTestId(/field-row-/);
    await userEvent.click(within(rows[1]).getByRole("button", { name: /remove/i }));

    await waitFor(() => expect(screen.queryByDisplayValue("fieldB")).toBeNull());
    expect(screen.getByDisplayValue("fieldA")).toBeInTheDocument();
    expect(screen.getByDisplayValue("fieldC")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: /save schema/i }));

    await waitFor(() =>
      expect(saved).toEqual({
        fields: [
          { name: "fieldA", type: "string", required: false },
          { name: "fieldC", type: "string", required: false },
        ],
      }),
    );
  });

  it("shows a 409 change-safety alert naming the offending environment", async () => {
    server.use(
      http.get("/v1/projects/p1/schema", () =>
        HttpResponse.json({
          definition: { fields: [{ name: "maxRetries", type: "int", required: true }] },
          schemaVersion: 1,
        }),
      ),
      http.put("/v1/projects/p1/schema", () =>
        HttpResponse.json(
          { error: "changing type of maxRetries would break existing values in environment prod", environment: "prod" },
          { status: 409 },
        ),
      ),
    );
    renderSchemaBuilder();
    await waitFor(() => expect(screen.getByDisplayValue("maxRetries")).toBeInTheDocument());

    await userEvent.click(screen.getByRole("button", { name: /save schema/i }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(/environment prod/i));
  });
});
