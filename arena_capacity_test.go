package claygo

import (
	"testing"
	"time"
)

// TestUndersizedArenaReportsArenaCapacityExceeded pins the diagnosability of
// an arena smaller than MinMemorySize(). NewArray soft-fails to a zero Array
// in that case, and before this test every downstream symptom (empty layouts,
// the frame-2 hang below) arrived with no error ever fired — the handler heard
// nothing because ErrorTypeArenaCapacityExceeded existed but had no call site.
// Mirrors Clay__Array_Allocate_Arena (oracle/clay.h ~line 3963), which reports
// through the current context's handler on every failed reservation.
func TestUndersizedArenaReportsArenaCapacityExceeded(t *testing.T) {
	previous := GetCurrentContext()
	defer SetCurrentContext(previous)
	SetCurrentContext(nil)

	const capacity = uint(64 << 10) // far below MinMemorySize (~7.4 MB at defaults)
	var got []ErrorData
	Initialize(CreateArenaWithCapacity(capacity), Dimensions{Width: 1280, Height: 720}, ErrorHandler{
		Func: func(err ErrorData) { got = append(got, err) },
	})

	for _, e := range got {
		if e.Type == ErrorTypeArenaCapacityExceeded {
			return
		}
	}
	t.Fatalf("Initialize with a 64 KB arena fired no ErrorTypeArenaCapacityExceeded; got %d errors: %+v", len(got), got)
}

func TestArenaInitializesWithoutByteBuffer(t *testing.T) {
	previous := GetCurrentContext()
	defer SetCurrentContext(previous)
	SetCurrentContext(nil)

	var got []ErrorData
	arena := CreateArenaWithCapacity(MinMemorySize())
	ctx := Initialize(arena, Dimensions{Width: 320, Height: 200}, ErrorHandler{
		Func: func(err ErrorData) { got = append(got, err) },
	})
	ctx.BeginLayout()
	BoxID(ctx, "Box", Decl{Layout: LayoutConfig{Sizing: Sizing{
		Width: SizingFixed(100), Height: SizingFixed(50),
	}}, BackgroundColor: RGBA(255, 255, 255, 255)}, nil)
	commands := ctx.EndLayout(0)

	if len(got) != 0 {
		t.Fatalf("arena reported errors: %+v", got)
	}
	if ctx.layoutElements.Length == 0 || commands.Len() == 0 || ctx.arena.NextAllocation == 0 {
		t.Fatalf("arena did not initialize: elements=%d commands=%d next=%d", ctx.layoutElements.Length, commands.Len(), ctx.arena.NextAllocation)
	}
}

func TestArenaReservationRejectsOverflow(t *testing.T) {
	arena := CreateArenaWithCapacity(^uint(0))
	arena.NextAllocation = ^uintptr(0) - 3
	if arena.reserveBytes(8, 8) {
		t.Fatal("overflowing reservation succeeded")
	}
	if arena.NextAllocation != ^uintptr(0)-3 {
		t.Fatalf("failed reservation advanced arena to %d", arena.NextAllocation)
	}

	arena = CreateArenaWithCapacity(64)
	if arena.reserveBytes(1, 3) {
		t.Fatal("reservation with non-power-of-two alignment succeeded")
	}
	if arena.NextAllocation != 0 {
		t.Fatalf("invalid alignment advanced arena to %d", arena.NextAllocation)
	}
	if !arena.reserveBytes(0, 1) {
		t.Fatal("zero-byte reservation failed")
	}
	if arena.NextAllocation != 0 {
		t.Fatalf("zero-byte reservation advanced arena to %d", arena.NextAllocation)
	}
	if !arena.reserveBytes(64, 1) || arena.NextAllocation != 64 {
		t.Fatalf("exact-capacity reservation failed: next=%d", arena.NextAllocation)
	}
	if arena.reserveBytes(1, 1) {
		t.Fatal("reservation beyond capacity succeeded")
	}
	if arena.NextAllocation != 64 {
		t.Fatalf("failed over-capacity reservation advanced arena to %d", arena.NextAllocation)
	}
}

// TestUndersizedArenaDoesNotHang reproduces the frame-2 infinite loop an
// undersized arena used to cause. With layoutElementsHashMapInternal at zero
// capacity, addHashMapItem's Set failed but the bucket head was published
// anyway, pointing at a slot that was never stored; the next frame's chain
// walk then read the out-of-range zero sentinel, whose NextIndex of 0 (not -1)
// sent it into a 0 -> 0 cycle inside BeginLayout — forever, at 100% CPU.
//
// The layout output is worthless at this arena size and that is fine; the
// contract under test is only that frames keep terminating and the failure
// stays observable through the error handler rather than a livelock.
func TestUndersizedArenaDoesNotHang(t *testing.T) {
	arena := CreateArenaWithCapacity(64 << 10)
	ctx := Initialize(arena, Dimensions{Width: 1280, Height: 720}, ErrorHandler{})
	ctx.SetMeasureTextFunction(func(text StringSlice, cfg *TextElementConfig, _ any) Dimensions {
		return Dimensions{Width: float32(len(text.Text)) * 6, Height: 8}
	}, nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 5 {
			ctx.BeginLayout()
			BoxID(ctx, "Box", Decl{
				Layout: LayoutConfig{Sizing: Sizing{Width: SizingGrow(), Height: SizingGrow()}},
			}, nil)
			ctx.EndLayout(0.016)
		}
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("layout frames on an undersized arena did not terminate; the frame-2 hashmap chain cycle is back")
	}
}
