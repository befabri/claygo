package claygo

// LayoutDirection controls the direction children are laid out along.
type LayoutDirection uint8

const (
	LeftToRight LayoutDirection = iota
	TopToBottom
)

// LayoutAlignmentX controls horizontal alignment of children.
type LayoutAlignmentX uint8

const (
	AlignXLeft LayoutAlignmentX = iota
	AlignXRight
	AlignXCenter
)

// LayoutAlignmentY controls vertical alignment of children.
type LayoutAlignmentY uint8

const (
	AlignYTop LayoutAlignmentY = iota
	AlignYBottom
	AlignYCenter
)

// SizingType controls how an element takes up space on one axis.
type SizingType uint8

const (
	SizingTypeFit SizingType = iota
	SizingTypeGrow
	SizingTypePercent
	SizingTypeFixed
)

// TextWrapMode controls line breaking behavior for text elements.
type TextWrapMode uint8

const (
	TextWrapWords TextWrapMode = iota
	TextWrapNewlines
	TextWrapNone
)

// TextAlignment controls horizontal alignment of wrapped text lines.
type TextAlignment uint8

const (
	TextAlignLeft TextAlignment = iota
	TextAlignCenter
	TextAlignRight
)

// FloatingAttachPointType identifies one of nine anchor points on an element.
type FloatingAttachPointType uint8

const (
	AttachPointLeftTop FloatingAttachPointType = iota
	AttachPointLeftCenter
	AttachPointLeftBottom
	AttachPointCenterTop
	AttachPointCenterCenter
	AttachPointCenterBottom
	AttachPointRightTop
	AttachPointRightCenter
	AttachPointRightBottom
)

// PointerCaptureMode controls whether floating elements consume pointer events.
type PointerCaptureMode uint8

const (
	PointerCaptureModeCapture PointerCaptureMode = iota
	PointerCaptureModePassthrough
)

// FloatingAttachToElement controls what a floating element is positioned
// relative to.
type FloatingAttachToElement uint8

const (
	AttachToNone FloatingAttachToElement = iota
	AttachToParent
	AttachToElementWithID
	AttachToRoot
)

// FloatingClipToElement controls clipping inheritance for floating elements.
type FloatingClipToElement uint8

const (
	ClipToNone FloatingClipToElement = iota
	ClipToAttachedParent
)

// RenderCommandType discriminates the Clay render command union.
type RenderCommandType uint8

const (
	RenderCommandTypeNone RenderCommandType = iota
	RenderCommandTypeRectangle
	RenderCommandTypeBorder
	RenderCommandTypeText
	RenderCommandTypeImage
	RenderCommandTypeScissorStart
	RenderCommandTypeScissorEnd
	RenderCommandTypeOverlayColorStart
	RenderCommandTypeOverlayColorEnd
	RenderCommandTypeCustom
)

// PointerDataInteractionState describes the pointer button state this frame.
type PointerDataInteractionState uint8

const (
	PointerDataPressedThisFrame PointerDataInteractionState = iota
	PointerDataPressed
	PointerDataReleasedThisFrame
	PointerDataReleased
)

// ErrorType identifies the kind of error reported via ErrorHandler.
type ErrorType uint8

const (
	ErrorTypeTextMeasurementFunctionNotProvided ErrorType = iota
	ErrorTypeArenaCapacityExceeded
	ErrorTypeElementsCapacityExceeded
	ErrorTypeTextMeasurementCapacityExceeded
	ErrorTypeDuplicateID
	ErrorTypeFloatingContainerParentNotFound
	ErrorTypePercentageOver1
	ErrorTypeInternalError
	ErrorTypeUnbalancedOpenClose
	ErrorTypeHashMapCapacityExceeded
)

// TransitionState is the current phase of a transitioning element.
type TransitionState uint8

const (
	TransitionStateIdle TransitionState = iota
	TransitionStateEntering
	TransitionStateTransitioning
	TransitionStateExiting
)

// TransitionProperty is a bitmask of properties that can be transitioned.
type TransitionProperty uint16

const (
	TransitionPropertyNone            TransitionProperty = 0
	TransitionPropertyX               TransitionProperty = 1
	TransitionPropertyY               TransitionProperty = 2
	TransitionPropertyPosition                           = TransitionPropertyX | TransitionPropertyY
	TransitionPropertyWidth           TransitionProperty = 4
	TransitionPropertyHeight          TransitionProperty = 8
	TransitionPropertyDimensions                         = TransitionPropertyWidth | TransitionPropertyHeight
	TransitionPropertyBoundingBox                        = TransitionPropertyPosition | TransitionPropertyDimensions
	TransitionPropertyBackgroundColor TransitionProperty = 16
	TransitionPropertyOverlayColor    TransitionProperty = 32
	TransitionPropertyCornerRadius    TransitionProperty = 64
	TransitionPropertyBorderColor     TransitionProperty = 128
	TransitionPropertyBorderWidth     TransitionProperty = 256
	TransitionPropertyBorder                             = TransitionPropertyBorderColor | TransitionPropertyBorderWidth
)

// TransitionEnterTriggerType controls when an "enter" transition fires.
type TransitionEnterTriggerType uint8

const (
	TransitionEnterSkipOnFirstParentFrame TransitionEnterTriggerType = iota
	TransitionEnterTriggerOnFirstParentFrame
)

// TransitionExitTriggerType controls when an "exit" transition fires.
type TransitionExitTriggerType uint8

const (
	TransitionExitSkipWhenParentExits TransitionExitTriggerType = iota
	TransitionExitTriggerWhenParentExits
)

// TransitionInteractionHandlingType controls whether interactions are blocked
// during position transitions.
type TransitionInteractionHandlingType uint8

const (
	TransitionDisableInteractionsWhileTransitioningPosition TransitionInteractionHandlingType = iota
	TransitionAllowInteractionsWhileTransitioningPosition
)

// ExitTransitionSiblingOrdering controls z-ordering of an exiting element
// relative to its siblings.
type ExitTransitionSiblingOrdering uint8

const (
	ExitTransitionOrderingUnderneathSiblings ExitTransitionSiblingOrdering = iota
	ExitTransitionOrderingNaturalOrder
	ExitTransitionOrderingAboveSiblings
)
