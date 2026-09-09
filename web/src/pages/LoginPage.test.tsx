import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect } from "vitest";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { server, handlers } from "@/test/msw";
import { LoginPage } from "@/pages/LoginPage";

function renderLogin() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter(
    [
      { path: "/login", element: <LoginPage /> },
      { path: "/", element: <div>Projects Home</div> },
    ],
    { initialEntries: ["/login"] },
  );
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

describe("LoginPage", () => {
  it("logs in and redirects home on success", async () => {
    server.use(handlers.me(null), handlers.loginOk({ id: "u1", email: "a@x.com" }));
    renderLogin();
    await userEvent.type(screen.getByLabelText(/email/i), "a@x.com");
    await userEvent.type(screen.getByLabelText(/password/i), "pw");
    await userEvent.click(screen.getByRole("button", { name: /log in/i }));
    await waitFor(() => expect(screen.getByText("Projects Home")).toBeInTheDocument());
  });

  it("shows an error on bad credentials", async () => {
    server.use(handlers.me(null), handlers.loginFail());
    renderLogin();
    await userEvent.type(screen.getByLabelText(/email/i), "a@x.com");
    await userEvent.type(screen.getByLabelText(/password/i), "wrong");
    await userEvent.click(screen.getByRole("button", { name: /log in/i }));
    await waitFor(() => expect(screen.getByText(/invalid credentials/i)).toBeInTheDocument());
  });
});
