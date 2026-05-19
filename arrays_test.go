package claygo

import "testing"

// newTestArena returns a freshly-allocated arena of the given byte capacity.
func newTestArena(t *testing.T, capacity uint) *Arena {
	t.Helper()
	mem := make([]byte, capacity)
	a := CreateArenaWithCapacityAndMemory(capacity, mem)
	return &a
}

type pointStruct struct {
	X int32
	Y int32
}

func TestArrayNewArrayCapacity(t *testing.T) {
	arena := newTestArena(t, 4096)
	for _, capacity := range []int32{0, 1, 4, 16, 128} {
		arr := NewArray[int32](capacity, arena)
		if arr.Capacity != capacity {
			t.Errorf("capacity %d: got Capacity=%d", capacity, arr.Capacity)
		}
		if arr.Length != 0 {
			t.Errorf("capacity %d: expected Length=0, got %d", capacity, arr.Length)
		}
		if capacity > 0 && int32(len(arr.Data)) != capacity {
			t.Errorf("capacity %d: len(Data)=%d, want %d", capacity, len(arr.Data), capacity)
		}
	}
}

func TestArrayNewArrayStructBacking(t *testing.T) {
	arena := newTestArena(t, 4096)
	arr := NewArray[pointStruct](8, arena)
	if arr.Capacity != 8 || len(arr.Data) != 8 {
		t.Fatalf("unexpected backing: cap=%d len=%d", arr.Capacity, len(arr.Data))
	}
	for i := int32(0); i < 8; i++ {
		arr.Add(pointStruct{X: i, Y: i * 2})
	}
	if arr.Length != 8 {
		t.Fatalf("length after fill = %d, want 8", arr.Length)
	}
	got := arr.GetValue(3)
	if got != (pointStruct{X: 3, Y: 6}) {
		t.Errorf("GetValue(3)=%+v, want {3 6}", got)
	}
}

func TestArrayAddUntilFull(t *testing.T) {
	arena := newTestArena(t, 4096)
	arr := NewArray[int32](3, arena)

	for i, want := range []int32{10, 20, 30} {
		p := arr.Add(want)
		if p == nil {
			t.Fatalf("Add %d returned nil pointer", i)
		}
		if *p != want {
			t.Errorf("Add %d returned ptr to %d, want %d", i, *p, want)
		}
		if arr.Length != int32(i+1) {
			t.Errorf("after Add %d, Length=%d, want %d", i, arr.Length, i+1)
		}
	}
	if arr.Length != 3 {
		t.Fatalf("Length after fill = %d, want 3", arr.Length)
	}
}

func TestArrayAddOverCapacityReturnsZeroPtr(t *testing.T) {
	arena := newTestArena(t, 4096)
	arr := NewArray[int32](2, arena)
	arr.Add(1)
	arr.Add(2)
	if arr.Length != 2 {
		t.Fatalf("Length before overflow = %d, want 2", arr.Length)
	}
	p := arr.Add(999)
	if p == nil {
		t.Fatal("Add over capacity returned nil; want pointer to zero sentinel")
	}
	if p != &arr.zero {
		t.Error("Add over capacity should return pointer to the array's zero sentinel")
	}
	if *p != 0 {
		t.Errorf("zero sentinel value = %d, want 0", *p)
	}
	if arr.Length != 2 {
		t.Errorf("Length should not grow on overflow; got %d", arr.Length)
	}
	if arr.GetValue(0) != 1 || arr.GetValue(1) != 2 {
		t.Errorf("data corrupted after overflow Add: %+v", arr.Data)
	}
}

func TestArrayGetInAndOutOfRange(t *testing.T) {
	arena := newTestArena(t, 4096)
	arr := NewArray[int32](4, arena)
	arr.Add(100)
	arr.Add(200)

	if v := *arr.Get(0); v != 100 {
		t.Errorf("Get(0)=%d, want 100", v)
	}
	if v := *arr.Get(1); v != 200 {
		t.Errorf("Get(1)=%d, want 200", v)
	}

	// Out-of-range (>= Length but < Capacity).
	if p := arr.Get(2); p != &arr.zero {
		t.Error("Get(2) (>= Length) should return pointer to zero sentinel")
	}
	// Out-of-range past Capacity.
	if p := arr.Get(99); p != &arr.zero {
		t.Error("Get(99) (>= Capacity) should return pointer to zero sentinel")
	}
	// Negative index.
	if p := arr.Get(-1); p != &arr.zero {
		t.Error("Get(-1) should return pointer to zero sentinel")
	}
	if *arr.Get(-1) != 0 {
		t.Error("zero sentinel should hold zero value")
	}
}

func TestArrayGetValueOutOfRange(t *testing.T) {
	arena := newTestArena(t, 4096)
	arr := NewArray[int32](2, arena)
	arr.Add(7)
	if v := arr.GetValue(0); v != 7 {
		t.Errorf("GetValue(0)=%d, want 7", v)
	}
	if v := arr.GetValue(5); v != 0 {
		t.Errorf("GetValue(5) out of range = %d, want zero", v)
	}
	if v := arr.GetValue(-2); v != 0 {
		t.Errorf("GetValue(-2) = %d, want zero", v)
	}
}

func TestArrayGetCheckCapacity(t *testing.T) {
	arena := newTestArena(t, 4096)
	arr := NewArray[int32](4, arena)
	arr.Add(11)
	// Length == 1, but Capacity == 4: GetCheckCapacity should succeed for indices 0..3.
	for i := int32(0); i < 4; i++ {
		p := arr.GetCheckCapacity(i)
		if p == &arr.zero {
			t.Errorf("GetCheckCapacity(%d) returned sentinel; expected real slot", i)
		}
		if p != &arr.Data[i] {
			t.Errorf("GetCheckCapacity(%d) returned wrong pointer", i)
		}
	}
	// Out-of-capacity falls back to sentinel.
	if arr.GetCheckCapacity(4) != &arr.zero {
		t.Error("GetCheckCapacity(Capacity) should return sentinel")
	}
	if arr.GetCheckCapacity(-1) != &arr.zero {
		t.Error("GetCheckCapacity(-1) should return sentinel")
	}
}

func TestArraySet(t *testing.T) {
	arena := newTestArena(t, 4096)
	arr := NewArray[int32](4, arena)

	// Set within length (after seeding via Add) should not bump Length.
	arr.Add(1)
	arr.Add(2)
	if p := arr.Set(0, 42); p == nil || *p != 42 {
		t.Errorf("Set(0,42) returned %v", p)
	}
	if arr.Length != 2 {
		t.Errorf("Length after Set within range = %d, want 2", arr.Length)
	}
	if arr.GetValue(0) != 42 {
		t.Errorf("Set(0,42) did not persist")
	}

	// Set exactly at Length (boundary case): Length=2, Set(2, x) should bump
	// Length to 3. This pins the off-by-one in `i >= Length`.
	if p := arr.Set(2, 77); p == nil || *p != 77 {
		t.Errorf("Set(2,77) returned %v", p)
	}
	if arr.Length != 3 {
		t.Errorf("Length after Set at exactly Length = %d, want 3", arr.Length)
	}
	if arr.GetValue(2) != 77 {
		t.Errorf("Set(2,77) did not persist")
	}

	// Set past Length but within Capacity bumps Length to i+1.
	if p := arr.Set(3, 99); p == nil || *p != 99 {
		t.Errorf("Set(3,99) returned %v", p)
	}
	if arr.Length != 4 {
		t.Errorf("Length after Set(3,99) = %d, want 4", arr.Length)
	}

	// Set at Capacity (out of range) returns nil.
	if p := arr.Set(4, 7); p != nil {
		t.Error("Set at Capacity should return nil")
	}
	if arr.Length != 4 {
		t.Errorf("Length should not grow on failed Set; got %d", arr.Length)
	}

	// Negative index returns nil.
	if p := arr.Set(-1, 7); p != nil {
		t.Error("Set(-1) should return nil")
	}
}

func TestArraySetDontTouchLength(t *testing.T) {
	arena := newTestArena(t, 4096)
	arr := NewArray[int32](4, arena)
	arr.Add(10)

	// Writing past Length must not bump Length.
	if p := arr.SetDontTouchLength(2, 55); p == nil || *p != 55 {
		t.Errorf("SetDontTouchLength(2,55) returned %v", p)
	}
	if arr.Length != 1 {
		t.Errorf("Length after SetDontTouchLength = %d, want 1", arr.Length)
	}
	// The slot was nevertheless written.
	if arr.Data[2] != 55 {
		t.Errorf("SetDontTouchLength did not persist: %+v", arr.Data)
	}
	// Out of capacity returns nil.
	if p := arr.SetDontTouchLength(99, 1); p != nil {
		t.Error("SetDontTouchLength past Capacity should return nil")
	}
	if p := arr.SetDontTouchLength(-1, 1); p != nil {
		t.Error("SetDontTouchLength(-1) should return nil")
	}
}

func TestArrayRemoveSwapback(t *testing.T) {
	arena := newTestArena(t, 4096)
	arr := NewArray[int32](4, arena)
	arr.Add(10)
	arr.Add(20)
	arr.Add(30)
	arr.Add(40)

	// Remove middle: last (40) should swap into slot 1.
	got := arr.RemoveSwapback(1)
	if got != 20 {
		t.Errorf("RemoveSwapback(1) = %d, want 20", got)
	}
	if arr.Length != 3 {
		t.Errorf("Length after remove = %d, want 3", arr.Length)
	}
	if arr.GetValue(1) != 40 {
		t.Errorf("slot 1 after swapback = %d, want 40", arr.GetValue(1))
	}

	// Remove last element: no swap visible since it swaps with itself
	// (after Length--, index == Length).
	got = arr.RemoveSwapback(2) // current state: [10, 40, 30]
	if got != 30 {
		t.Errorf("RemoveSwapback(last) = %d, want 30", got)
	}
	if arr.Length != 2 {
		t.Errorf("Length = %d, want 2", arr.Length)
	}

	// Out-of-range remove returns zero, leaves length unchanged.
	if got := arr.RemoveSwapback(99); got != 0 {
		t.Errorf("RemoveSwapback(99) = %d, want 0", got)
	}
	if got := arr.RemoveSwapback(-1); got != 0 {
		t.Errorf("RemoveSwapback(-1) = %d, want 0", got)
	}
	if arr.Length != 2 {
		t.Errorf("Length after no-op remove = %d, want 2", arr.Length)
	}
}

func TestArrayRemoveSwapbackOnlyElement(t *testing.T) {
	arena := newTestArena(t, 4096)
	arr := NewArray[int32](4, arena)
	arr.Add(7)
	got := arr.RemoveSwapback(0)
	if got != 7 {
		t.Errorf("RemoveSwapback(0) on single-element array = %d, want 7", got)
	}
	if arr.Length != 0 {
		t.Errorf("Length after removing only element = %d, want 0", arr.Length)
	}
	// Now removing from empty must soft-fail.
	if got := arr.RemoveSwapback(0); got != 0 {
		t.Errorf("RemoveSwapback on empty = %d, want 0", got)
	}
}

func TestArrayLengthZero(t *testing.T) {
	arena := newTestArena(t, 4096)
	arr := NewArray[int32](4, arena)
	if arr.Length != 0 {
		t.Fatalf("freshly allocated Length = %d", arr.Length)
	}
	if p := arr.Get(0); p != &arr.zero {
		t.Error("Get(0) on empty should return sentinel")
	}
	if v := arr.GetValue(0); v != 0 {
		t.Errorf("GetValue(0) on empty = %d, want 0", v)
	}
}

func TestArrayFillToCapacityThenSet(t *testing.T) {
	arena := newTestArena(t, 4096)
	arr := NewArray[int32](3, arena)
	arr.Add(1)
	arr.Add(2)
	arr.Add(3)
	if arr.Length != 3 {
		t.Fatalf("Length = %d, want 3", arr.Length)
	}
	// Set in-bounds at exactly Capacity-1 still works.
	if p := arr.Set(2, 99); p == nil || *p != 99 {
		t.Errorf("Set(last) returned %v", p)
	}
	if arr.Length != 3 {
		t.Errorf("Length unchanged check: %d", arr.Length)
	}
	// Set at exactly Capacity fails.
	if arr.Set(3, 7) != nil {
		t.Error("Set at Capacity should return nil")
	}
}

func TestArraySliceGet(t *testing.T) {
	data := []int32{5, 6, 7, 8}
	slice := ArraySlice[int32]{Length: int32(len(data)), Data: data}

	if v := *slice.Get(0); v != 5 {
		t.Errorf("slice.Get(0) = %d, want 5", v)
	}
	if v := *slice.Get(3); v != 8 {
		t.Errorf("slice.Get(3) = %d, want 8", v)
	}
	if p := slice.Get(4); p != &slice.zero {
		t.Error("slice.Get past Length should return sentinel")
	}
	if p := slice.Get(-1); p != &slice.zero {
		t.Error("slice.Get(-1) should return sentinel")
	}
	if *slice.Get(99) != 0 {
		t.Error("slice sentinel should be zero")
	}
}

func TestArrayArenaExhaustionSoftFail(t *testing.T) {
	// 8 bytes can fit at most 2 int32s.
	arena := newTestArena(t, 8)
	arr := NewArray[int32](100, arena)
	if arr.Capacity != 0 {
		t.Errorf("expected zero Array on arena exhaustion; got Capacity=%d", arr.Capacity)
	}
	if arr.Length != 0 {
		t.Errorf("expected Length=0; got %d", arr.Length)
	}
	if arr.Data != nil {
		t.Errorf("expected nil Data; got len=%d", len(arr.Data))
	}
	// Soft-fail operations must not panic.
	if p := arr.Get(0); p != &arr.zero {
		t.Error("Get on exhausted array should return sentinel")
	}
	if v := arr.GetValue(0); v != 0 {
		t.Errorf("GetValue on exhausted array = %d, want 0", v)
	}
	if p := arr.GetCheckCapacity(0); p != &arr.zero {
		t.Error("GetCheckCapacity on exhausted array should return sentinel")
	}
	if p := arr.Add(42); p != &arr.zero {
		t.Error("Add on exhausted array should return sentinel")
	}
	if arr.Length != 0 {
		t.Errorf("Length should remain 0 after failed Add; got %d", arr.Length)
	}
	if p := arr.Set(0, 1); p != nil {
		t.Error("Set on exhausted array should return nil")
	}
	if p := arr.SetDontTouchLength(0, 1); p != nil {
		t.Error("SetDontTouchLength on exhausted array should return nil")
	}
	if v := arr.RemoveSwapback(0); v != 0 {
		t.Errorf("RemoveSwapback on exhausted array = %d, want 0", v)
	}
}

func TestArrayZeroSentinelIsolation(t *testing.T) {
	// Sentinels are per-Array so writes through one cannot leak to another.
	arena := newTestArena(t, 4096)
	a := NewArray[int32](1, arena)
	b := NewArray[int32](1, arena)
	pa := a.Get(99) // out of range; sentinel pointer
	*pa = 1234      // intentionally write through sentinel
	if *b.Get(99) != 0 {
		t.Errorf("array b sentinel was polluted by writes to array a")
	}
}
