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
	var got []ErrorData
	mem := make([]byte, 64<<10) // far below MinMemorySize (~7.4 MB at defaults)
	arena := CreateArenaWithCapacityAndMemory(uint(len(mem)), mem)
	Initialize(arena, Dimensions{Width: 1280, Height: 720}, ErrorHandler{
		Func: func(err ErrorData) { got = append(got, err) },
	})

	for _, e := range got {
		if e.Type == ErrorTypeArenaCapacityExceeded {
			return
		}
	}
	t.Fatalf("Initialize with a 64 KB arena fired no ErrorTypeArenaCapacityExceeded; got %d errors: %+v", len(got), got)
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
	mem := make([]byte, 64<<10)
	arena := CreateArenaWithCapacityAndMemory(uint(len(mem)), mem)
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
