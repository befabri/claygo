package claygo

import (
	"testing"
)

// deterministicMeasureTextForTest is a byte-identical copy of
// golden_test.go::deterministicMeasureText. We duplicate rather than alias
// because that function is in a *_test.go file and we want this file to be
// self-contained against future churn in golden_test.go.
func deterministicMeasureTextForTest(text StringSlice, cfg *TextElementConfig, _ any) Dimensions {
	charW := float32(int(float32(cfg.FontSize) * 0.55))
	chars := len(text.Text)
	gaps := float32(0)
	if chars > 0 {
		gaps = float32(chars - 1)
	}
	w := float32(chars)*charW + gaps*float32(cfg.LetterSpacing)
	var h float32
	if cfg.LineHeight > 0 {
		h = float32(cfg.LineHeight)
	} else {
		h = float32(cfg.FontSize + 4)
	}
	return Dimensions{Width: w, Height: h}
}

// freshContext builds a Context with the deterministic measurer wired up.
func freshContext(t *testing.T) *Context {
	t.Helper()
	mem := make([]byte, MinMemorySize())
	arena := CreateArenaWithCapacityAndMemory(uint(len(mem)), mem)
	ctx := Initialize(arena, Dimensions{Width: 1280, Height: 720}, ErrorHandler{
		Func: func(err ErrorData) {
			t.Errorf("clay error: type=%d text=%q", err.Type, err.Text)
		},
	})
	ctx.SetMeasureTextFunction(deterministicMeasureTextForTest, nil)
	return ctx
}

// TestHashStringContentsWithConfigGoldens locks down the cache hash output
// for a hand-picked set of inputs against the C reference. The expected
// values were harvested by linking against oracle/clay.h with
// CLAY_DISABLE_SIMD (so the contents-only HashData path matches the Go
// implementation) and printing Clay__HashStringContentsWithConfig for each
// input; see the build instructions in measure.go's documentation.
func TestHashStringContentsWithConfigGoldens(t *testing.T) {
	cases := []struct {
		name                                        string
		text                                        string
		fontID, fontSize, letterSpacing, lineHeight uint16
		want                                        uint32
	}{
		{"empty/zero-cfg", "", 0, 0, 0, 0, 1},
		{"a/fs12", "a", 0, 12, 0, 0, 4127516372},
		{"hello/fs14/ls1", "hello", 0, 14, 1, 0, 1615652504},
		// lineHeight is *not* part of the hash mix in upstream, so changing it
		// must leave the hash alone. This case pins that invariant.
		{"hello/fs14/ls1/lh20", "hello", 0, 14, 1, 20, 1615652504},
		{"hello/fid1", "hello", 1, 14, 1, 0, 1803644931},
		{"hello/fs16", "hello", 0, 16, 1, 0, 2063998530},
		{"hello/ls2", "hello", 0, 14, 2, 0, 1341998585},
		{"greetings", "Greetings, world!", 3, 24, 0, 32, 2655954400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := TextElementConfig{
				FontID:        tc.fontID,
				FontSize:      tc.fontSize,
				LetterSpacing: tc.letterSpacing,
				LineHeight:    tc.lineHeight,
			}
			got := hashStringContentsWithConfig(tc.text, &cfg)
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// TestHashStringContentsWithConfigDistinct asserts that perturbing any of the
// keyed fields produces a different hash. Defends against accidental drops
// of one of the mix steps.
func TestHashStringContentsWithConfigDistinct(t *testing.T) {
	base := TextElementConfig{FontID: 0, FontSize: 14, LetterSpacing: 1, LineHeight: 0}
	baseHash := hashStringContentsWithConfig("hello", &base)

	cases := []struct {
		name string
		text string
		cfg  TextElementConfig
	}{
		{"different text", "world", base},
		{"different fontId", "hello", TextElementConfig{FontID: 1, FontSize: 14, LetterSpacing: 1}},
		{"different fontSize", "hello", TextElementConfig{FontID: 0, FontSize: 16, LetterSpacing: 1}},
		{"different letterSpacing", "hello", TextElementConfig{FontID: 0, FontSize: 14, LetterSpacing: 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := hashStringContentsWithConfig(tc.text, &tc.cfg)
			if got == baseHash {
				t.Errorf("hash collided with base (%d)", got)
			}
		})
	}
}

// TestMeasureTextCachedUnwrappedDimensions checks the unwrapped dimensions
// the measurement cache reports for a few short strings using the
// deterministic measurer.
//
// For text without spaces or newlines the unwrapped width is one trailing
// word; upstream subtracts letterSpacing from the final measuredWidth so that
// inter-letter gaps don't add an extra phantom letter at the end of the
// line.
func TestMeasureTextCachedUnwrappedDimensions(t *testing.T) {
	cases := []struct {
		name       string
		text       string
		cfg        TextElementConfig
		wantWidth  float32
		wantHeight float32
	}{
		{
			// charW = floor(14*0.55) = 7; "hi" -> 2*7 + 1*1 = 15; minus letterSpacing 1 = 14.
			name:       "hi/fs14/ls1",
			text:       "hi",
			cfg:        TextElementConfig{FontSize: 14, LetterSpacing: 1},
			wantWidth:  14,
			wantHeight: 18, // fs + 4
		},
		{
			// "a" -> 1*7 + 0*ls = 7; minus letterSpacing 0 = 7.
			name:       "a/fs14",
			text:       "a",
			cfg:        TextElementConfig{FontSize: 14},
			wantWidth:  7,
			wantHeight: 18,
		},
		{
			// "abc def" splits at the space: word "abc" -> 3*7 + 2*0 = 21, word "def"
			// -> same. Space width is 1*7 = 7. Line width = 21 + 7 + 21 = 49. Minus
			// letterSpacing 0 = 49.
			name:       "abc def/fs14",
			text:       "abc def",
			cfg:        TextElementConfig{FontSize: 14},
			wantWidth:  49,
			wantHeight: 18,
		},
		{
			// "abc\ndef" splits at the newline: lineWidth resets between words; the
			// max line is 21. Minus letterSpacing 0 = 21.
			name:       "abc\\ndef/fs14",
			text:       "abc\ndef",
			cfg:        TextElementConfig{FontSize: 14},
			wantWidth:  21,
			wantHeight: 18,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := freshContext(t)
			ctx.BeginLayout()
			item := ctx.measureTextCached(tc.text, &tc.cfg)
			if item == nil {
				t.Fatal("measureTextCached returned nil")
			}
			if item.UnwrappedDimensions.Width != tc.wantWidth {
				t.Errorf("width: got %v, want %v", item.UnwrappedDimensions.Width, tc.wantWidth)
			}
			if item.UnwrappedDimensions.Height != tc.wantHeight {
				t.Errorf("height: got %v, want %v", item.UnwrappedDimensions.Height, tc.wantHeight)
			}
		})
	}
}

// TestMeasureTextCachedContainsNewlines pins the containsNewlines flag, which
// the layout solver later uses to skip work when wrapping straightforward
// single-line strings.
func TestMeasureTextCachedContainsNewlines(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	cfg := TextElementConfig{FontSize: 14}
	plain := ctx.measureTextCached("no newlines here", &cfg)
	if plain.ContainsNewlines {
		t.Errorf("plain text reported ContainsNewlines = true")
	}
	multi := ctx.measureTextCached("first\nsecond", &cfg)
	if !multi.ContainsNewlines {
		t.Errorf("multiline text reported ContainsNewlines = false")
	}
}

// TestMeasureTextCachedRepeatHits checks that repeat measurements of the same
// (text, cfg) return the same slot — no duplicate allocation.
func TestMeasureTextCachedRepeatHits(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	cfg := TextElementConfig{FontSize: 14, LetterSpacing: 1}
	first := ctx.measureTextCached("hello world", &cfg)
	lengthBefore := ctx.measureTextHashMapInternal.Length
	wordsBefore := ctx.measuredWords.Length

	second := ctx.measureTextCached("hello world", &cfg)
	if first != second {
		t.Errorf("repeat call returned a different slot: %p vs %p", first, second)
	}
	if ctx.measureTextHashMapInternal.Length != lengthBefore {
		t.Errorf("repeat call grew the cache: was %d, now %d", lengthBefore, ctx.measureTextHashMapInternal.Length)
	}
	if ctx.measuredWords.Length != wordsBefore {
		t.Errorf("repeat call grew the word array: was %d, now %d", wordsBefore, ctx.measuredWords.Length)
	}
}

// TestMeasureTextCachedDistinctSlots checks the inverse: distinct (text, cfg)
// keys land in distinct cache slots.
func TestMeasureTextCachedDistinctSlots(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	cfgA := TextElementConfig{FontSize: 14}
	cfgB := TextElementConfig{FontSize: 16}
	a := ctx.measureTextCached("hello", &cfgA)
	b := ctx.measureTextCached("hello", &cfgB)
	if a == b {
		t.Errorf("different fontSizes returned same cache entry")
	}
	c := ctx.measureTextCached("world", &cfgA)
	if c == a {
		t.Errorf("different texts returned same cache entry")
	}
}

// TestMeasureTextCachedWordCount verifies the word-splitting algorithm emits
// the expected number of MeasuredWord entries for known inputs. Each space
// produces one MeasuredWord, each newline produces two (the optional pre-
// newline content + a synthetic zero-width marker), and a trailing run with
// no terminator adds one more.
func TestMeasureTextCachedWordCount(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		wantWords int32
	}{
		{"no-spaces", "abc", 1},                    // trailing run only
		{"one-space", "abc def", 2},                // one space + trailing run
		{"two-spaces", "abc def ghi", 3},           // two spaces + trailing run
		{"trailing-space", "abc ", 1},              // space-terminated word, no trailing run
		{"single-newline", "abc\ndef", 3},          // content + synthetic + trailing
		{"newline-only", "\n", 1},                  // just the synthetic marker
		{"leading-space", " abc", 2},               // space (empty pre-word marker) + trailing run
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := freshContext(t)
			ctx.BeginLayout()
			cfg := TextElementConfig{FontSize: 14}
			before := ctx.measuredWords.Length
			ctx.measureTextCached(tc.text, &cfg)
			got := ctx.measuredWords.Length - before
			if got != tc.wantWords {
				t.Errorf("text=%q: got %d MeasuredWord entries, want %d", tc.text, got, tc.wantWords)
			}
		})
	}
}

// TestAddGetHashMapItemRoundTrip is a sanity test for the element ID hashmap:
// addHashMapItem followed by getHashMapItem returns the same entry, with the
// LayoutElement pointer and ElementID preserved.
func TestAddGetHashMapItemRoundTrip(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()

	// Reserve a couple of LayoutElement slots in the elements array.
	a := ctx.layoutElements.Add(LayoutElement{ID: 111})
	b := ctx.layoutElements.Add(LayoutElement{ID: 222})

	idA := ElementID{ID: 111, StringID: String{Text: "a"}}
	idB := ElementID{ID: 222, StringID: String{Text: "b"}}

	gotA := ctx.addHashMapItem(idA, a)
	gotB := ctx.addHashMapItem(idB, b)
	if gotA == nil || gotB == nil {
		t.Fatalf("addHashMapItem returned nil: a=%v b=%v", gotA, gotB)
	}

	lookupA := ctx.getHashMapItem(111)
	lookupB := ctx.getHashMapItem(222)
	if lookupA == nil || lookupB == nil {
		t.Fatalf("getHashMapItem missed: a=%v b=%v", lookupA, lookupB)
	}
	if lookupA.LayoutElement != a {
		t.Errorf("lookupA.LayoutElement = %p, want %p", lookupA.LayoutElement, a)
	}
	if lookupB.LayoutElement != b {
		t.Errorf("lookupB.LayoutElement = %p, want %p", lookupB.LayoutElement, b)
	}
	if lookupA.ElementID.StringID.Text != "a" {
		t.Errorf("lookupA.ElementID.StringID = %q, want %q", lookupA.ElementID.StringID.Text, "a")
	}

	// Lookup for an unknown ID returns nil.
	if miss := ctx.getHashMapItem(999); miss != nil {
		t.Errorf("expected nil for missing ID, got %+v", miss)
	}
}

// contextCapturingErrors builds a Context whose error handler records every
// error into the returned slice instead of failing the test. Used by tests
// that intentionally trigger Clay error paths.
func contextCapturingErrors(t *testing.T) (*Context, *[]ErrorData) {
	t.Helper()
	var errs []ErrorData
	mem := make([]byte, MinMemorySize())
	arena := CreateArenaWithCapacityAndMemory(uint(len(mem)), mem)
	ctx := Initialize(arena, Dimensions{Width: 1280, Height: 720}, ErrorHandler{
		Func: func(err ErrorData) { errs = append(errs, err) },
	})
	ctx.SetMeasureTextFunction(deterministicMeasureTextForTest, nil)
	return ctx, &errs
}

// TestHashMapCollisionChained exercises the linear-probe chain: two IDs that
// land in the same bucket (id % capacity collides) must both be retrievable
// via getHashMapItem, with the chain linked through NextIndex.
func TestHashMapCollisionChained(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()

	cap32 := uint32(ctx.layoutElementsHashMap.Capacity)
	// Two distinct IDs that share the same hash bucket.
	idA := ElementID{ID: 100, StringID: String{Text: "a"}}
	idB := ElementID{ID: 100 + cap32, StringID: String{Text: "b"}}
	if idA.ID%cap32 != idB.ID%cap32 {
		t.Fatalf("test setup: ids should collide, got buckets %d vs %d",
			idA.ID%cap32, idB.ID%cap32)
	}

	elemA := ctx.layoutElements.Add(LayoutElement{ID: idA.ID})
	elemB := ctx.layoutElements.Add(LayoutElement{ID: idB.ID})
	itemA := ctx.addHashMapItem(idA, elemA)
	itemB := ctx.addHashMapItem(idB, elemB)
	if itemA == nil || itemB == nil {
		t.Fatalf("addHashMapItem returned nil on collision insert")
	}

	// Both must be retrievable.
	if got := ctx.getHashMapItem(idA.ID); got == nil || got.LayoutElement != elemA {
		t.Errorf("getHashMapItem(idA) = %v, want elemA=%p", got, elemA)
	}
	if got := ctx.getHashMapItem(idB.ID); got == nil || got.LayoutElement != elemB {
		t.Errorf("getHashMapItem(idB) = %v, want elemB=%p", got, elemB)
	}

	// The chain must be linked: the bucket head is either A's slot pointing at
	// B via NextIndex, or B's slot pointing at A. Exactly one of them has
	// NextIndex != -1 (the head of the chain).
	chainTails := 0
	if itemA.NextIndex != -1 {
		chainTails++
	}
	if itemB.NextIndex != -1 {
		chainTails++
	}
	if chainTails != 1 {
		t.Errorf("expected exactly one chain link, got %d (A.NextIndex=%d, B.NextIndex=%d)",
			chainTails, itemA.NextIndex, itemB.NextIndex)
	}
}

// TestHashMapDuplicateIDReportsError pins the same-frame duplicate-id error
// path: adding the same ElementID twice within one BeginLayout/EndLayout cycle
// should fire ErrorTypeDuplicateID via the error handler.
func TestHashMapDuplicateIDReportsError(t *testing.T) {
	ctx, errs := contextCapturingErrors(t)
	ctx.BeginLayout()

	id := ElementID{ID: 555, StringID: String{Text: "dup"}}
	elem1 := ctx.layoutElements.Add(LayoutElement{ID: id.ID})
	elem2 := ctx.layoutElements.Add(LayoutElement{ID: id.ID})

	if got := ctx.addHashMapItem(id, elem1); got == nil {
		t.Fatalf("first add returned nil")
	}
	if len(*errs) != 0 {
		t.Fatalf("first add unexpectedly errored: %v", *errs)
	}

	// Second add in the same frame: must call the error handler with
	// ErrorTypeDuplicateID, but still return the existing entry (not nil).
	if got := ctx.addHashMapItem(id, elem2); got == nil {
		t.Fatalf("second add returned nil; want existing entry")
	}
	if len(*errs) != 1 || (*errs)[0].Type != ErrorTypeDuplicateID {
		t.Fatalf("expected one ErrorTypeDuplicateID, got %+v", *errs)
	}
}

// TestHashMapGenerationStaleReplaces verifies that the same ElementID added
// across two BeginLayout cycles is treated as a stale entry to be replaced,
// not a duplicate-id error. The new LayoutElement pointer must overwrite the
// old one in the existing hashmap slot.
func TestHashMapGenerationStaleReplaces(t *testing.T) {
	ctx, errs := contextCapturingErrors(t)

	ctx.BeginLayout()
	id := ElementID{ID: 777, StringID: String{Text: "stale"}}
	elemFrame1 := ctx.layoutElements.Add(LayoutElement{ID: id.ID})
	if ctx.addHashMapItem(id, elemFrame1) == nil {
		t.Fatalf("frame-1 add returned nil")
	}
	internalLenAfterFrame1 := ctx.layoutElementsHashMapInternal.Length

	// Advance to next frame. BeginLayout resets ephemeral state and bumps
	// generation; the hashmap entry's Generation now lags ctx.generation.
	ctx.BeginLayout()
	elemFrame2 := ctx.layoutElements.Add(LayoutElement{ID: id.ID})
	got := ctx.addHashMapItem(id, elemFrame2)
	if got == nil {
		t.Fatalf("frame-2 add returned nil")
	}
	if len(*errs) != 0 {
		t.Errorf("frame-2 add unexpectedly fired errors: %v", *errs)
	}
	if got.LayoutElement != elemFrame2 {
		t.Errorf("stale entry not replaced: LayoutElement = %p, want %p",
			got.LayoutElement, elemFrame2)
	}
	// No new slot should have been allocated — the existing one was reused.
	if ctx.layoutElementsHashMapInternal.Length != internalLenAfterFrame1 {
		t.Errorf("stale replace allocated a new slot: internal Length %d, want %d",
			ctx.layoutElementsHashMapInternal.Length, internalLenAfterFrame1)
	}
}
