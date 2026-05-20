package claygo

// TextRenderData is the payload for RenderCommandTypeText.
type TextRenderData struct {
	StringContents StringSlice
	TextColor      Color
	FontID         uint16
	FontSize       uint16
	LetterSpacing  uint16
	LineHeight     uint16
}

// RectangleRenderData is the payload for RenderCommandTypeRectangle.
type RectangleRenderData struct {
	BackgroundColor Color
	CornerRadius    CornerRadius
}

// ImageRenderData is the payload for RenderCommandTypeImage.
type ImageRenderData struct {
	BackgroundColor Color
	CornerRadius    CornerRadius
	ImageData       any
}

// CustomRenderData is the payload for RenderCommandTypeCustom.
type CustomRenderData struct {
	BackgroundColor Color
	CornerRadius    CornerRadius
	CustomData      any
}

// ClipRenderData is the payload for RenderCommandTypeScissorStart/End.
type ClipRenderData struct {
	Horizontal bool
	Vertical   bool
}

// OverlayColorRenderData is the payload for
// RenderCommandTypeOverlayColorStart/End.
type OverlayColorRenderData struct {
	Color Color
}

// BorderRenderData is the payload for RenderCommandTypeBorder.
type BorderRenderData struct {
	Color        Color
	CornerRadius CornerRadius
	Width        BorderWidth
}

// RenderData is the discriminated payload of a RenderCommand. The C original
// is a union; in Go we hold all variants by value, which only costs ~32 bytes
// per command and avoids any heap allocation. Consult CommandType on the
// RenderCommand to know which field is meaningful.
type RenderData struct {
	Rectangle    RectangleRenderData
	Text         TextRenderData
	Image        ImageRenderData
	Custom       CustomRenderData
	Border       BorderRenderData
	Clip         ClipRenderData
	OverlayColor OverlayColorRenderData
}

// RenderCommand is one instruction in the sorted output of EndLayout.
type RenderCommand struct {
	BoundingBox BoundingBox
	RenderData  RenderData
	UserData    any
	ID          uint32
	ZIndex      int16
	CommandType RenderCommandType
}

// RenderCommandArray is the array returned by EndLayout. The array is sorted
// in ascending draw order; renderers can iterate naively.
type RenderCommandArray struct {
	Commands []RenderCommand
}

// Len returns the number of commands.
func (a RenderCommandArray) Len() int { return len(a.Commands) }

// Get returns the i-th command, or a zero command if out of range.
func (a RenderCommandArray) Get(i int) RenderCommand {
	if i < 0 || i >= len(a.Commands) {
		return RenderCommand{}
	}
	return a.Commands[i]
}

// GetPtr returns a pointer to the i-th command, or nil if out of range. This
// mirrors Clay_RenderCommandArray_Get for callers that need pointer semantics.
func (a RenderCommandArray) GetPtr(i int) *RenderCommand {
	if i < 0 || i >= len(a.Commands) {
		return nil
	}
	return &a.Commands[i]
}

// RenderCommandArray_Get is the package-level Clay_RenderCommandArray_Get
// analogue. It returns nil when array is nil or i is out of range.
func RenderCommandArray_Get(array *RenderCommandArray, i int32) *RenderCommand {
	if array == nil {
		return nil
	}
	return array.GetPtr(int(i))
}
