package notifications

import (
	"testing"

	"github.com/google/fleetspeak/fleetspeak/src/common"
)

func count(ch <-chan struct{}) int {
	cnt := 0
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return cnt
			}
			cnt++
		default:
			return cnt
		}
	}
}

func TestDispatcherDispatches(t *testing.T) {
	id1, err := common.BytesToClientID([]byte{0, 0, 0, 0, 0, 0, 0, 1})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := common.BytesToClientID([]byte{0, 0, 0, 0, 0, 0, 0, 2})
	if err != nil {
		t.Fatal(err)
	}
	id3, err := common.BytesToClientID([]byte{0, 0, 0, 0, 0, 0, 0, 3})
	if err != nil {
		t.Fatal(err)
	}

	d := NewDispatcher()
	ch1, fin1 := d.Register(id1)
	defer fin1()
	ch2, fin2 := d.Register(id2)
	defer fin2()

	if cnt := count(ch1); cnt != 0 {
		t.Errorf("Channel should initially be empty, found %d records.", cnt)
	}

	// Should not block, should put one marker in the channel.
	d.Dispatch(id1)

	if cnt := count(ch1); cnt != 1 {
		t.Errorf("Dispatch should add one record, but found %d records.", cnt)
	}
	if cnt := count(ch2); cnt != 0 {
		t.Errorf("Unused channel should be empty, but found %d records.", cnt)
	}

	// Multiple calls should also not block, and should put exactly one marker in
	// the channel.
	d.Dispatch(id1)
	d.Dispatch(id1)
	d.Dispatch(id1)

	if cnt := count(ch1); cnt != 1 {
		t.Errorf("Dispatch should add one record, but found %d records.", cnt)
	}
	if cnt := count(ch2); cnt != 0 {
		t.Errorf("Unused channel should be empty, but found %d records.", cnt)
	}

	// Dispatching an unknown id should be a noop.
	d.Dispatch(id3)
	if cnt := count(ch1); cnt != 0 {
		t.Errorf("Unused channel should be empty, but found %d records.", cnt)
	}
	if cnt := count(ch2); cnt != 0 {
		t.Errorf("Unused channel should be empty, but found %d records.", cnt)
	}
}

func TestDispatcherCloses(t *testing.T) {
	id1, err := common.BytesToClientID([]byte{0, 0, 0, 0, 0, 0, 0, 1})
	if err != nil {
		t.Fatal(err)
	}

	d := NewDispatcher()
	ch1, fin1 := d.Register(id1)
	ch2, fin2 := d.Register(id1)

	if _, ok := <-ch1; ok {
		t.Error("ch1 should close with new registration, but read succeeded.")
	}
	// fin1 should be a safe noop at this point.
	fin1()

	d.Dispatch(id1)
	if cnt := count(ch2); cnt != 1 {
		t.Errorf("Dispatch should add one record, but found %d records.", cnt)
	}

	fin2()
	if _, ok := <-ch2; ok {
		t.Error("ch2 should close when fin2 is called, but read succeeded.")
	}
}
