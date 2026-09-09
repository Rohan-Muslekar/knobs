import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, it, expect } from "vitest";
import { server, handlers } from "@/test/msw";
import { useSession, useLogin, useLogout } from "@/hooks/useAuth";

function wrapper() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("useSession", () => {
  it("returns the user when authenticated", async () => {
    server.use(handlers.me({ id: "u1", email: "a@x.com" }));
    const { result } = renderHook(() => useSession(), { wrapper: wrapper() });
    await waitFor(() => expect(result.current.data?.email).toBe("a@x.com"));
  });

  it("errors (401) when not authenticated", async () => {
    server.use(handlers.me(null));
    const { result } = renderHook(() => useSession(), { wrapper: wrapper() });
    await waitFor(() => expect(result.current.isError).toBe(true));
  });
});

describe("useLogin", () => {
  it("populates the session cache on success", async () => {
    // handlers.me covers the initial useSession fetch (unauthenticated) that
    // fires on mount, before login happens; without it onUnhandledRequest:"error"
    // would fail the test on that request.
    server.use(handlers.me(null), handlers.loginOk({ id: "u1", email: "a@x.com" }));
    const { result } = renderHook(() => ({ login: useLogin(), session: useSession() }), {
      wrapper: wrapper(),
    });
    result.current.login.mutate({ email: "a@x.com", password: "pw" });
    await waitFor(() => expect(result.current.login.isSuccess).toBe(true));
    expect(result.current.login.data?.email).toBe("a@x.com");
    // The real assertion: onSuccess wrote the user into the ["session"] cache,
    // so useSession reflects it too — not just the mutation's own resolved value.
    await waitFor(() => expect(result.current.session.data?.email).toBe("a@x.com"));
  });
});

describe("useLogout", () => {
  it("clears the session cache on success", async () => {
    server.use(handlers.me({ id: "u1", email: "a@x.com" }), handlers.logoutOk());
    const { result } = renderHook(() => ({ logout: useLogout(), session: useSession() }), {
      wrapper: wrapper(),
    });
    // Seed the cache: wait for the initial useSession fetch to resolve.
    await waitFor(() => expect(result.current.session.data?.email).toBe("a@x.com"));

    // qc.clear() drops the cached session, but the still-mounted useSession
    // observer immediately refetches. Swap /me to unauthenticated first, the
    // way the real backend would once the session cookie is gone, so the
    // post-clear refetch doesn't just repopulate the same user.
    server.use(handlers.me(null));
    result.current.logout.mutate();
    await waitFor(() => expect(result.current.logout.isSuccess).toBe(true));
    await waitFor(() => expect(result.current.session.data).toBeFalsy());
  });
});
