package claygo

// LayoutConfig controls sizing, padding, gap, alignment, and child layout
// direction for an element.
type LayoutConfig struct {
	Sizing          Sizing
	Padding         Padding
	ChildGap        uint16
	ChildAlignment  ChildAlignment
	LayoutDirection LayoutDirection
	// WrapChildren makes children that do not fit on the layout axis start a
	// new line: rows stacked top to bottom for LeftToRight, columns stacked
	// left to right for TopToBottom. Lines break greedily at the sizes
	// children have before Grow distributes space; each line then shares its
	// own slack, ChildGap separates lines as well as children, and
	// ChildAlignment places lines and their children the way it places a
	// single row. A parent whose children fit on one line lays out exactly as
	// it would with WrapChildren off.
	//
	// claygo extension; upstream Clay has no child wrapping.
	WrapChildren bool
}

// TextElementConfig controls text measurement, wrapping, alignment, and render
// styling.
type TextElementConfig struct {
	UserData      any
	TextColor     Color
	FontID        uint16
	FontSize      uint16
	LetterSpacing uint16
	LineHeight    uint16
	WrapMode      TextWrapMode
	TextAlignment TextAlignment
}

// AspectRatioElementConfig pins an element's final width/height ratio.
type AspectRatioElementConfig struct {
	AspectRatio float32
}

// ImageElementConfig carries an opaque image handle through to render
// commands.
type ImageElementConfig struct {
	ImageData any
}

// FloatingAttachPoints pairs an anchor on the floating element with an anchor
// on its parent / target.
type FloatingAttachPoints struct {
	Element FloatingAttachPointType
	Parent  FloatingAttachPointType
}

// FloatingElementConfig controls a floating element's position relative to its
// attach target.
type FloatingElementConfig struct {
	Offset             Vector2
	Expand             Dimensions
	ParentID           uint32
	ZIndex             int16
	AttachPoints       FloatingAttachPoints
	PointerCaptureMode PointerCaptureMode
	AttachTo           FloatingAttachToElement
	ClipTo             FloatingClipToElement
}

// CustomElementConfig carries an opaque user pointer through to CUSTOM render
// commands.
type CustomElementConfig struct {
	CustomData any
}

// ClipElementConfig enables clipping (scissoring) and provides a scroll offset
// applied to children.
type ClipElementConfig struct {
	Horizontal  bool
	Vertical    bool
	ChildOffset Vector2
}

// BorderElementConfig sets a shared border color and per-side widths.
type BorderElementConfig struct {
	Color Color
	Width BorderWidth
}

// TransitionData is the animated state snapshot passed to transition handlers.
type TransitionData struct {
	BoundingBox     BoundingBox
	BackgroundColor Color
	OverlayColor    Color
	BorderColor     Color
	BorderWidth     BorderWidth
}

// TransitionCallbackArguments is passed to the user's transition handler each
// frame.
type TransitionCallbackArguments struct {
	TransitionState TransitionState
	Initial         TransitionData
	Current         *TransitionData
	Target          TransitionData
	ElapsedTime     float32
	Duration        float32
	Properties      TransitionProperty
}

// TransitionEnterConfig groups settings for the entering phase.
type TransitionEnterConfig struct {
	SetInitialState func(target TransitionData, properties TransitionProperty) TransitionData
	Trigger         TransitionEnterTriggerType
}

// TransitionExitConfig groups settings for the exiting phase.
type TransitionExitConfig struct {
	SetFinalState   func(initial TransitionData, properties TransitionProperty) TransitionData
	Trigger         TransitionExitTriggerType
	SiblingOrdering ExitTransitionSiblingOrdering
}

// TransitionElementConfig describes how an element should animate between
// states.
type TransitionElementConfig struct {
	// Handler updates args.Current and returns true when the transition is complete.
	Handler             func(args TransitionCallbackArguments) bool
	Duration            float32
	Properties          TransitionProperty
	InteractionHandling TransitionInteractionHandlingType
	Enter               TransitionEnterConfig
	Exit                TransitionExitConfig
}

// Decl is the full element declaration passed to Box. Mirrors
// Clay_ElementDeclaration but uses Go idioms (any for opaque
// pointers, embedded configs).
type Decl struct {
	Layout          LayoutConfig
	BackgroundColor Color
	OverlayColor    Color
	CornerRadius    CornerRadius
	AspectRatio     AspectRatioElementConfig
	Image           ImageElementConfig
	Floating        FloatingElementConfig
	Custom          CustomElementConfig
	Clip            ClipElementConfig
	Border          BorderElementConfig
	Transition      TransitionElementConfig
	UserData        any
}
