package world

import "testing"

func TestStoreSetReplacesExistingValue(t *testing.T) {
	s := newStore[int](2)
	s.set(10, 100)
	s.set(10, 101)

	if s.len() != 1 {
		t.Fatalf("length = %d, want 1", s.len())
	}
	got, ok := s.get(10)
	if !ok || *got != 101 {
		t.Fatalf("value = %v, present = %v, want 101, true", got, ok)
	}
}

func TestStoreRemoveUsesSwapDeleteAndRepairsIndex(t *testing.T) {
	s := newStore[int](3)
	s.set(10, 100)
	s.set(20, 200)
	s.set(30, 300)

	if !s.remove(20) {
		t.Fatal("remove returned false")
	}
	if _, ok := s.get(20); ok {
		t.Fatal("removed component still exists")
	}
	got, ok := s.get(30)
	if !ok || *got != 300 {
		t.Fatalf("moved value = %v, present = %v, want 300, true", got, ok)
	}
	if s.len() != 2 {
		t.Fatalf("length = %d, want 2", s.len())
	}
}
