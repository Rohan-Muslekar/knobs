/**
 * A manually-driven byte stream for deterministic SSE tests: the test pushes raw text
 * chunks (simulating bytes arriving off the wire, at whatever chunk boundaries it likes)
 * and closes/errors it when done. No timers, no real network — fully test-controlled.
 */
export interface ControllableStream {
  stream: ReadableStream<Uint8Array>;
  push(text: string): void;
  close(): void;
  error(err: unknown): void;
  /**
   * Whether the underlying source's `cancel()` has been invoked — i.e. some reader called
   * `reader.cancel()` (directly, or via `AbortController.abort()`'s abort listener in
   * stream.ts). Lets a test assert that a connection was actually released on a given
   * code path, not just that a callback fired.
   */
  cancelled: boolean;
}

export function createControllableStream(): ControllableStream {
  let ctrl!: ReadableStreamDefaultController<Uint8Array>;
  const state = { cancelled: false };
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      ctrl = controller;
    },
    cancel() {
      state.cancelled = true;
    },
  });
  const encoder = new TextEncoder();

  return {
    stream,
    get cancelled(): boolean {
      return state.cancelled;
    },
    push(text: string): void {
      // Swallowed: if the consumer already cancelled its reader (openStream's cancel()),
      // the underlying source is in a "canceled" state and enqueue throws. That's exactly
      // the end state a cancel-then-push test wants to observe — the push is a no-op, not
      // a test failure.
      try {
        ctrl.enqueue(encoder.encode(text));
      } catch {
        /* stream already cancelled/closed — no-op */
      }
    },
    close(): void {
      try {
        ctrl.close();
      } catch {
        /* already closed/cancelled — no-op */
      }
    },
    error(err: unknown): void {
      try {
        ctrl.error(err);
      } catch {
        /* already closed/cancelled — no-op */
      }
    },
  };
}
