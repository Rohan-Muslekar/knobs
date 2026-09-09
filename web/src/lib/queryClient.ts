import { QueryClient } from "@tanstack/react-query";
import { ApiError } from "./api";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: (count, err) => {
        // Never retry auth failures; retry other errors once.
        if (err instanceof ApiError && (err.status === 401 || err.status === 404)) return false;
        return count < 1;
      },
      staleTime: 10_000,
    },
  },
});
