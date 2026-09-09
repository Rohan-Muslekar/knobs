package delivery

import (
	"sync"

	"github.com/google/uuid"
)

// subscriberBuffer is how many un-consumed snapshots a subscriber channel
// holds before Publish starts dropping the oldest one. It only needs to be
// a handful: a subscriber that falls behind by this much is, for delivery
// purposes, only ever interested in the latest snapshot anyway (each one is
// a full state, not a delta), so dropping older ones loses nothing a
// reconnect-with-since wouldn't also lose.
const subscriberBuffer = 4

// Hub fans a per-environment stream of Snapshots out to every subscriber of
// that environment. It is the in-process half of the SSE delivery pipeline;
// the Postgres LISTEN/NOTIFY Listener is what feeds it Publish calls across
// server instances.
//
// Hub is safe for concurrent use. Publish never blocks on a slow or stalled
// subscriber: each subscriber's channel is buffered, and once full, Publish
// drops that subscriber's oldest queued snapshot to make room for the new
// one rather than waiting for the subscriber to drain it. This means one
// wedged SSE client can never stall delivery to the rest, nor to the
// pg_notify-driven Listener goroutine that calls Publish.
type Hub struct {
	mu   sync.Mutex
	subs map[uuid.UUID]map[int]chan Snapshot
	next int
}

// NewHub creates an empty Hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[uuid.UUID]map[int]chan Snapshot)}
}

// Subscribe registers interest in envID's snapshots. The returned channel
// receives every snapshot subsequently Published for envID (subject to the
// drop-oldest buffering above) until cancel is called. The caller must call
// cancel exactly once it is done reading, typically via defer or on client
// disconnect; cancel is idempotent, so a second call is harmless.
//
// cancel both removes the subscription (so Publish no longer references it)
// and closes the channel, so a range/receive loop over it terminates
// cleanly rather than blocking forever.
func (h *Hub) Subscribe(envID uuid.UUID) (<-chan Snapshot, func()) {
	ch := make(chan Snapshot, subscriberBuffer)

	h.mu.Lock()
	if h.subs[envID] == nil {
		h.subs[envID] = make(map[int]chan Snapshot)
	}
	id := h.next
	h.next++
	h.subs[envID][id] = ch
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			// The delete and the close both happen under the same mutex
			// Publish holds while it iterates+sends, so Publish can never
			// observe (and send on) a channel that has already been
			// closed here: either Publish's iteration snapshot of the map
			// already ran before this delete (and simply won't see this
			// subscriber next time), or it runs after and the entry is
			// already gone.
			if m, ok := h.subs[envID]; ok {
				delete(m, id)
				if len(m) == 0 {
					delete(h.subs, envID)
				}
			}
			close(ch)
			h.mu.Unlock()
		})
	}
	return ch, cancel
}

// Publish delivers snap to every current subscriber of envID. It never
// blocks: a subscriber whose buffer is full has its oldest queued snapshot
// dropped to make room, so one slow reader can't delay or block delivery to
// anyone else (or to the caller, typically the Listener's NOTIFY loop).
func (h *Hub) Publish(envID uuid.UUID, snap Snapshot) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subs[envID] {
		select {
		case ch <- snap:
		default:
			// Buffer full: drop the oldest queued snapshot to make room,
			// then retry the send. Both steps are non-blocking selects, so
			// even if another goroutine could somehow race the channel
			// empty in between (it can't: Publish is the only sender and
			// holds h.mu for its whole run), we degrade to "drop this
			// publish for this subscriber" rather than block.
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- snap:
			default:
			}
		}
	}
}
