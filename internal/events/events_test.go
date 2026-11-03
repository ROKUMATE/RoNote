package events

import (
	"testing"

	"github.com/google/uuid"
)

func TestBroker(t *testing.T) {
	t.Parallel()
	b := NewBroker()
	one, two := uuid.New(), uuid.New()
	evs1, cancel1 := b.Subscribe(one)
	evs1b, cancel1b := b.Subscribe(one)
	evs2, cancel2 := b.Subscribe(two)

	b.publish(Event{ImportID: one, Status: "fetching"})
	for _, ch := range []<-chan Event{evs1, evs1b} {
		if ev := <-ch; ev.Status != "fetching" {
			t.Errorf("subscriber got %+v, want fetching", ev)
		}
	}
	select {
	case ev := <-evs2:
		t.Errorf("another import's subscriber got %+v", ev)
	default:
	}

	b.resyncAll()
	for i, ch := range []<-chan Event{evs1, evs1b, evs2} {
		if ev := <-ch; !ev.Resync {
			t.Errorf("subscriber %d after resyncAll got %+v, want a resync", i, ev)
		}
	}

	cancel1()
	cancel1() // a second cancel is harmless
	b.publish(Event{ImportID: one, Status: "done"})
	select {
	case ev := <-evs1:
		t.Errorf("a cancelled subscriber got %+v", ev)
	default:
	}
	if ev := <-evs1b; ev.Status != "done" {
		t.Errorf("the remaining subscriber got %+v, want done", ev)
	}
	cancel1b()
	cancel2()
	if n := b.subscribers(); n != 0 {
		t.Errorf("subscribers after every cancel = %d, want 0", n)
	}
}

func TestBrokerNeverBlocks(t *testing.T) {
	t.Parallel()
	b := NewBroker()
	id := uuid.New()
	_, cancel := b.Subscribe(id)
	defer cancel()
	for range buffered * 3 { // nobody reads; publish must still return
		b.publish(Event{ImportID: id, Status: "fetching"})
	}
	b.resyncAll()
}
