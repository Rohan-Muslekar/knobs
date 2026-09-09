import { setupServer } from "msw/node";
import { http, HttpResponse } from "msw";

export const server = setupServer();

// Reusable handler builders for tests.
export const handlers = {
  me: (user: { id: string; email: string } | null) =>
    http.get("/v1/auth/me", () =>
      user ? HttpResponse.json(user) : HttpResponse.json({ error: "authentication required" }, { status: 401 }),
    ),
  loginOk: (user: { id: string; email: string }) =>
    http.post("/v1/auth/login", () => HttpResponse.json(user)),
  loginFail: () =>
    http.post("/v1/auth/login", () => HttpResponse.json({ error: "invalid credentials" }, { status: 401 })),
  logoutOk: () => http.post("/v1/auth/logout", () => HttpResponse.json({ status: "ok" })),
};

export { http, HttpResponse };
