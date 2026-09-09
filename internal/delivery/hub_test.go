package delivery_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/delivery"
)

func TestHubSubscribePublishReceive(t *testing.T) {
	h := delivery.NewHub()
	envID := uuid.New()

	ch, cancel := h.Subscribe(envID)
	defer cancel()

	want := delivery.Snapshot{Version: 1, SchemaHash: "abc", Values: map[string]any{"x": 1}}
	h.Publish(envID, want)

	select {
	case got := <-ch:
		if got.Version != want.Version || got.SchemaHash != want.SchemaHash {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for published snapshot")
	}
}

func TestHubPublishToOtherEnvironmentIsNotDelivered(t *testing.T) {
	h := delivery.NewHub()
	envA := uuid.New()
	envB := uuid.New()

	chA, cancelA := h.Subscribe(envA)
	defer cancelA()

	h.Publish(envB, delivery.Snapshot{Version: 1})

	select {
	case got := <-chA:
		t.Fatalf("envA subscriber received a publish meant for envB: %+v", got)
	case <-time.After(100 * time.Millisecond):
		// expected: nothing delivered.
	}
}

func TestHubCancelThenPublishDoesNotDeliver(t *testing.T) {
	h := delivery.NewHub()
	envID := uuid.New()

	ch, cancel := h.Subscribe(envID)
	cancel()

	// Publishing after cancel must not panic (send on closed channel) and
	// must not somehow resurrect delivery to the cancelled subscriber.
	h.Publish(envID, delivery.Snapshot{Version: 1})

	select {
	case got, ok := <-ch:
		if ok {
			t.Fatalf("received %+v on a cancelled subscription, want closed channel", got)
		}
		// ok == false: channel closed, as expected.
	case <-time.After(2 * time.Second):
		t.Fatal("timed out reading from cancelled channel; want it closed immediately")
	}
}

func TestHubCancelIsIdempotent(t *testing.T) {
	h := delivery.NewHub()
	envID := uuid.New()

	_, cancel := h.Subscribe(envID)
	cancel()
	cancel() // must not panic (double close)
}

// TestHubSlowSubscriberDoesNotBlockFastSubscriber is the key isolation
// guarantee: a subscriber that never drains its channel must not be able to
// wedge Publish for everyone else. Without drop-oldest/non-blocking sends,
// a slow consumer's full buffered channel would make Publish block forever
// on a synchronous send, starving every other subscriber of that (and any
// other) environment.
func TestHubSlowSubscriberDoesNotBlockFastSubscriber(t *testing.T) {
	h := delivery.NewHub()
	envID := uuid.New()

	_, slowCancel := h.Subscribe(envID)
	defer slowCancel()
	// The slow subscriber's channel is deliberately never read from.

	fastCh, fastCancel := h.Subscribe(envID)
	defer fastCancel()

	done := make(chan struct{})
	go func() {
		// Publish far more times than any reasonable buffer size while the
		// slow subscriber never reads. If Publish blocked on a full
		// subscriber channel, this goroutine would hang and the test would
		// time out below.
		for i := 0; i < 50; i++ {
			h.Publish(envID, delivery.Snapshot{Version: i})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a slow subscriber; drop-oldest/non-blocking isolation is broken")
	}

	// The fast subscriber must still have received at least the final
	// publish (possibly with some drops in between, which is fine).
	select {
	case got := <-fastCh:
		if got.Version < 0 {
			t.Fatalf("unexpected snapshot %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fast subscriber received nothing despite Publish completing")
	}
}

func TestHubMultipleSubscribersSameEnvironmentBothReceive(t *testing.T) {
	h := delivery.NewHub()
	envID := uuid.New()

	ch1, cancel1 := h.Subscribe(envID)
	defer cancel1()
	ch2, cancel2 := h.Subscribe(envID)
	defer cancel2()

	h.Publish(envID, delivery.Snapshot{Version: 7})

	for i, ch := range []<-chan delivery.Snapshot{ch1, ch2} {
		select {
		case got := <-ch:
			if got.Version != 7 {
				t.Fatalf("subscriber %d got version %d, want 7", i, got.Version)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("subscriber %d: timed out", i)
		}
	}
}
