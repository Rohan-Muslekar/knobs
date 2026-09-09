export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
    this.name = "ApiError";
  }
}

type Options = { method?: string; body?: unknown };

export async function api<T = unknown>(path: string, opts: Options = {}): Promise<T> {
  const res = await fetch(path, {
    method: opts.method ?? "GET",
    credentials: "include",
    headers: opts.body ? { "Content-Type": "application/json" } : undefined,
    body: opts.body ? JSON.stringify(opts.body) : undefined,
  });
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  const data = text ? JSON.parse(text) : undefined;
  if (!res.ok) {
    const message =
      data && typeof data === "object" && "error" in data
        ? String((data as { error: unknown }).error)
        : `request failed (${res.status})`;
    throw new ApiError(res.status, message);
  }
  return data as T;
}
