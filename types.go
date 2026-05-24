package claygo

// String represents a non-owning view over text. In the C original this is a
// (chars, length) pair plus a "static" lifetime hint; in Go we lean on the
// language's native string type, which already has cheap O(1) slicing and GC
// lifetime management.
type String struct {
	Text string
}

// MakeString wraps a Go string for use with claygo.
func MakeString(s string) String { return String{Text: s} }

// Length returns the byte length of the string.
func (s String) Length() int32 { return int32(len(s.Text)) }

// StringSlice is a slice of text passed to measurement/rendering callbacks.
// Base points at the full source string when available.
type StringSlice struct {
	Text string
	Base string
}

// Dimensions is a 2D size in pixels.
type Dimensions struct {
	Width, Height float32
}

// Vector2 is a 2D position or offset in pixels.
type Vector2 struct {
	X, Y float32
}

// Color is an RGBA color. Conventionally 0-255 per channel, but interpretation
// is up to the renderer.
type Color struct {
	R, G, B, A float32
}

// RGBA constructs a Color from 0-255 components.
func RGBA(r, g, b, a float32) Color { return Color{R: r, G: g, B: b, A: a} }

// BoundingBox is an axis-aligned rectangle in layout space.
type BoundingBox struct {
	X, Y, Width, Height float32
}

// CornerRadius controls per-corner rounding.
type CornerRadius struct {
	TopLeft, TopRight, BottomLeft, BottomRight float32
}

// UniformCornerRadius returns a CornerRadius with the same value on all corners.
func UniformCornerRadius(r float32) CornerRadius {
	return CornerRadius{TopLeft: r, TopRight: r, BottomLeft: r, BottomRight: r}
}

// Padding is per-side spacing in pixels between an element's bounding box and
// its children.
type Padding struct {
	Left, Right, Top, Bottom uint16
}

// PaddingAll returns Padding with the same value on all sides.
func PaddingAll(v uint16) Padding { return Padding{Left: v, Right: v, Top: v, Bottom: v} }

// BorderWidth controls per-side border thickness and an optional between-children
// border that is generated as an extra rectangle render command.
type BorderWidth struct {
	Left, Right, Top, Bottom uint16
	BetweenChildren          uint16
}

// BorderOutside returns a BorderWidth with the same value on all four outer
// sides and no between-children divider. Mirrors CLAY_BORDER_OUTSIDE
// (oracle/clay.h ~line 122).
func BorderOutside(w uint16) BorderWidth {
	return BorderWidth{Left: w, Right: w, Top: w, Bottom: w}
}

// BorderAll returns a BorderWidth with the same value on all four sides
// AND a matching between-children divider. Mirrors CLAY_BORDER_ALL
// (oracle/clay.h ~line 124).
func BorderAll(w uint16) BorderWidth {
	return BorderWidth{Left: w, Right: w, Top: w, Bottom: w, BetweenChildren: w}
}

// ChildAlignment controls how children are aligned within their parent on each
// axis.
type ChildAlignment struct {
	X LayoutAlignmentX
	Y LayoutAlignmentY
}

// SizingMinMax bounds the minimum and maximum pixel size of an element on a
// single axis. Used together with SizingFit/SizingGrow.
type SizingMinMax struct {
	Min, Max float32
}

// SizingAxis controls the size of one axis of an element. Use the SizingFit,
// SizingGrow, SizingFixed, and SizingPercent helpers to construct values.
type SizingAxis struct {
	MinMax  SizingMinMax
	Percent float32
	Type    SizingType
}

// SizingFit clamps the axis to fit its contents, optionally bounded by min/max.
func SizingFit(minMax ...float32) SizingAxis {
	return SizingAxis{MinMax: sizingMinMaxFromVarargs(minMax), Type: SizingTypeFit}
}

// SizingGrow expands the axis to fill available space in the parent, sharing
// with sibling GROW elements, optionally bounded by min/max.
func SizingGrow(minMax ...float32) SizingAxis {
	return SizingAxis{MinMax: sizingMinMaxFromVarargs(minMax), Type: SizingTypeGrow}
}

// SizingFixed clamps the axis to an exact pixel size.
func SizingFixed(size float32) SizingAxis {
	return SizingAxis{MinMax: SizingMinMax{Min: size, Max: size}, Type: SizingTypeFixed}
}

// SizingPercent clamps the axis to a fraction (0-1) of the parent's axis size,
// minus padding and child gaps.
func SizingPercent(p float32) SizingAxis {
	return SizingAxis{Percent: p, Type: SizingTypePercent}
}

func sizingMinMaxFromVarargs(minMax []float32) SizingMinMax {
	switch len(minMax) {
	case 0:
		return SizingMinMax{}
	case 1:
		return SizingMinMax{Min: minMax[0]}
	default:
		return SizingMinMax{Min: minMax[0], Max: minMax[1]}
	}
}

// Sizing pairs a width and height sizing axis.
type Sizing struct {
	Width, Height SizingAxis
}

// ElementID is a hashed identifier for a UI element.
type ElementID struct {
	ID       uint32
	Offset   uint32
	BaseID   uint32
	StringID String
}

// Arena is the bump-allocator backing all of Clay's internal data structures.
// The caller supplies a byte slice large enough to satisfy MinMemorySize().
type Arena struct {
	NextAllocation uintptr
	Capacity       uint
	Memory         []byte
}

// ScrollContainerData is the runtime state of a scrolling element, returned by
// Context.GetScrollContainerData.
type ScrollContainerData struct {
	ScrollPosition            *Vector2
	ScrollContainerDimensions Dimensions
	ContentDimensions         Dimensions
	Config                    ClipElementConfig
	Found                     bool
}

// ElementData is bounding-box info for an element looked up by ID.
type ElementData struct {
	BoundingBox BoundingBox
	Found       bool
}

// PointerData captures the pointer (mouse/touch) state for the current frame.
type PointerData struct {
	Position Vector2
	State    PointerDataInteractionState
}
