import { render, waitFor, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { server, handlers } from "@/test/msw";
import { RequireAuth } from "@/components/RequireAuth";

function renderAt(path: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter(
    [
      { path: "/login", element: <div>Login Page</div> },
      { element: <RequireAuth />, children: [{ path: "/", element: <div>Projects Home</div> }] },
    ],
    { initialEntries: [path] },
  );
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

describe("RequireAuth", () => {
  it("redirects to /login when unauthenticated", async () => {
    server.use(handlers.me(null));
    renderAt("/");
    await waitFor(() => expect(screen.getByText("Login Page")).toBeInTheDocument());
  });

  it("renders protected content when authenticated", async () => {
    server.use(handlers.me({ id: "u1", email: "a@x.com" }));
    renderAt("/");
    await waitFor(() => expect(screen.getByText("Projects Home")).toBeInTheDocument());
  });
});
