// claygo extension: child wrap (Clay_LayoutConfig.wrapChildren).
//
// Not part of upstream Clay. This file is a fragment of clay_ext.h:
// patches/0001-child-wrap.patch splices `#include "patches/child-wrap.h"` into
// the CLAY_IMPLEMENTATION section of the verbatim clay.h, directly in front of
// Clay__SizeContainersAlongAxis, so every type, macro and function Clay has
// defined by that point is visible here. The patch itself carries only what
// must live inside upstream's own structs and functions: the config field,
// the line type, the element and context fields, and one-line hooks tagged
// "claygo extension: child wrap". Everything else about the feature is in
// this file, as ordinary C: edit it, rebuild, `make verify`.
//
// The Go port mirrors this file function for function in wrapchildren.go;
// the semantics are written up in docs/child-wrap-spec.md.
//
// Same zlib/libpng terms as clay.h; see ../../LICENSE.md.

#ifndef CLAY__ARRAY_DEFINE
#error "patches/child-wrap.h is a fragment of clay_ext.h and cannot be compiled on its own; build the oracle with make (oracle/UPSTREAM.md, Extensions)."
#endif

// Defined after Clay__SizeContainersAlongAxis in clay.h, used by Clay__WrapEmitDividers.
void Clay__AddRenderCommand(Clay_RenderCommand renderCommand);

// ---------------------------------------------------------------------------
// Sizing: line packing and per-line distribution, called from
// Clay__CloseElement and Clay__SizeContainersAlongAxis.
// ---------------------------------------------------------------------------

// Same criterion Clay__SizeContainersAlongAxis uses for its resizable buffer.
bool Clay__WrapChildIsResizable(Clay_LayoutElement *child, bool xAxis) {
    Clay_SizingAxis childSizing = Clay__GetElementSizing(child, xAxis);
    return childSizing.type != CLAY__SIZING_TYPE_PERCENT
        && childSizing.type != CLAY__SIZING_TYPE_FIXED
        && (!child->isTextElement || child->textConfig.wrapMode == CLAY_TEXT_WRAP_WORDS);
}

float Clay__WrapPadding(Clay_LayoutConfig *layoutConfig, bool xAxis) {
    return (float)(xAxis ? (layoutConfig->padding.left + layoutConfig->padding.right) : (layoutConfig->padding.top + layoutConfig->padding.bottom));
}

float Clay__WrapAxisSize(Clay_LayoutElement *element, bool xAxis) {
    return xAxis ? element->dimensions.width : element->dimensions.height;
}

// Wrap-axis minimum of a wrapping parent: its widest child, not the sum, so an
// ancestor can shrink the parent below its single-line width and make it wrap.
// Called from Clay__CloseElement once the children list is attached.
void Clay__WrapCloseElement(Clay_Context *context, Clay_LayoutElement *openLayoutElement) {
    Clay_LayoutConfig *layoutConfig = &openLayoutElement->config.layout;
    bool xAxis = layoutConfig->layoutDirection == CLAY_LEFT_TO_RIGHT;
    // Clip containers keep the padding-only minimum upstream gave them.
    if (xAxis ? openLayoutElement->config.clip.horizontal : openLayoutElement->config.clip.vertical) {
        return;
    }
    float largestMin = 0;
    for (int32_t i = 0; i < openLayoutElement->children.length; i++) {
        Clay_LayoutElement *child = Clay_LayoutElementArray_Get(&context->layoutElements, openLayoutElement->children.elements[i]);
        largestMin = CLAY__MAX(largestMin, xAxis ? child->minDimensions.width : child->minDimensions.height);
    }
    float minSize = Clay__WrapPadding(layoutConfig, xAxis) + largestMin;
    if (xAxis) {
        openLayoutElement->minDimensions.width = minSize;
    } else {
        openLayoutElement->minDimensions.height = minSize;
    }
}

// Wrap-axis content of one line, accumulated in the order upstream uses for a
// whole row (live non-percent sizes and gaps first, percent sizes last) so a
// single line distributes exactly like a non-wrapping row.
float Clay__WrapLineContent(Clay_Context *context, Clay_LayoutElement *parent, Clay__WrapLine *line, bool xAxis) {
    float childGap = parent->config.layout.childGap;
    float content = 0;
    bool isFirstChild = true;
    for (int32_t childOffset = line->start; childOffset < line->start + line->count; childOffset++) {
        Clay_LayoutElement *child = Clay_LayoutElementArray_Get(&context->layoutElements, parent->children.elements[childOffset]);
        if (child->exiting) {
            continue;
        }
        Clay_SizingAxis childSizing = Clay__GetElementSizing(child, xAxis);
        content += (childSizing.type == CLAY__SIZING_TYPE_PERCENT ? 0 : Clay__WrapAxisSize(child, xAxis));
        if (!isFirstChild) {
            content += childGap;
        }
        isFirstChild = false;
    }
    for (int32_t childOffset = line->start; childOffset < line->start + line->count; childOffset++) {
        Clay_LayoutElement *child = Clay_LayoutElementArray_Get(&context->layoutElements, parent->children.elements[childOffset]);
        if (Clay__GetElementSizing(child, xAxis).type == CLAY__SIZING_TYPE_PERCENT) {
            content += Clay__WrapAxisSize(child, xAxis);
        }
    }
    return content;
}

// Greedy first-fit packing along the wrap axis at the sizes children have
// before this parent distributes space. A child starts a new line when the
// line already has content and lineSize + childGap + childSize exceeds
// innerSize by more than CLAY__EPSILON, so a line that a previous pass grew to
// fill exactly packs the same way again despite float rounding. If the pool
// runs out, the remaining children join the last line.
void Clay__WrapPackLines(Clay_Context *context, Clay_LayoutElement *parent, bool xAxis) {
    Clay_LayoutConfig *layoutConfig = &parent->config.layout;
    float innerSize = Clay__WrapAxisSize(parent, xAxis) - Clay__WrapPadding(layoutConfig, xAxis);
    float childGap = layoutConfig->childGap;
    parent->wrapLines = CLAY__INIT(Clay__WrapLineArraySlice) { .length = 0, .internalArray = &context->wrapLines.internalArray[context->wrapLines.length] };
    Clay__WrapLine *line = NULL;
    float lineSize = 0;
    bool lineHasContent = false;
    for (int32_t childOffset = 0; childOffset < parent->children.length; childOffset++) {
        Clay_LayoutElement *child = Clay_LayoutElementArray_Get(&context->layoutElements, parent->children.elements[childOffset]);
        float childSize = Clay__WrapAxisSize(child, xAxis);
        if (line == NULL || (!child->exiting && lineHasContent && lineSize + childGap + childSize > innerSize + CLAY__EPSILON)) {
            Clay__WrapLine *added = Clay__WrapLineArray_Add(&context->wrapLines, CLAY__INIT(Clay__WrapLine) { .start = childOffset });
            if (added == &Clay__WrapLine_DEFAULT) {
                if (line) {
                    line->count += parent->children.length - childOffset;
                }
                break;
            }
            line = added;
            parent->wrapLines.length++;
            lineSize = 0;
            lineHasContent = false;
        }
        line->count++;
        if (child->exiting) {
            continue;
        }
        lineSize += lineHasContent ? childGap + childSize : childSize;
        lineHasContent = true;
    }
    for (int32_t lineIndex = 0; lineIndex < parent->wrapLines.length; lineIndex++) {
        Clay__WrapLine *packed = Clay__WrapLineArraySlice_Get(&parent->wrapLines, lineIndex);
        packed->contentSize = Clay__WrapLineContent(context, parent, packed, xAxis);
    }
}

// Per-line natural extents on the cross axis and, when publish is set, the
// parent's stacked cross-axis content and minimum. The size only grows and is
// clamped, like upstream's post-text-wrap height propagation, so a single line
// reproduces the non-wrapping result exactly; the minimum stacks child
// minimums so an ancestor cannot squash the lines, except for a parent that
// clips on that axis, which keeps upstream's padding-only minimum and can hide
// its content. Column wrap publishes only from the first sizing sweep: the
// second sweep's x pass is the ancestor's final word on the width.
void Clay__WrapUpdateCrossContent(Clay_Context *context, Clay_LayoutElement *parent, bool crossXAxis, bool publish) {
    Clay_LayoutConfig *layoutConfig = &parent->config.layout;
    float content = 0;
    float minContent = 0;
    for (int32_t lineIndex = 0; lineIndex < parent->wrapLines.length; lineIndex++) {
        Clay__WrapLine *line = Clay__WrapLineArraySlice_Get(&parent->wrapLines, lineIndex);
        float natural = 0;
        float naturalWithExiting = 0;
        float naturalMin = 0;
        for (int32_t childOffset = line->start; childOffset < line->start + line->count; childOffset++) {
            Clay_LayoutElement *child = Clay_LayoutElementArray_Get(&context->layoutElements, parent->children.elements[childOffset]);
            float childSize = Clay__WrapAxisSize(child, crossXAxis);
            // Upstream lets exiting children grow their parent, so the size does too.
            naturalWithExiting = CLAY__MAX(naturalWithExiting, childSize);
            if (child->exiting) {
                continue;
            }
            natural = CLAY__MAX(natural, childSize);
            naturalMin = CLAY__MAX(naturalMin, crossXAxis ? child->minDimensions.width : child->minDimensions.height);
        }
        line->naturalExtent = natural;
        content += naturalWithExiting;
        minContent += naturalMin;
    }
    if (!publish) {
        return;
    }
    float gaps = (float)(CLAY__MAX(parent->wrapLines.length - 1, 0) * layoutConfig->childGap);
    content += gaps;
    minContent += gaps;
    // Padding is added side by side, matching upstream's `height + top + bottom`.
    if (crossXAxis) {
        content = content + layoutConfig->padding.left + layoutConfig->padding.right;
        minContent += Clay__WrapPadding(layoutConfig, true);
    } else {
        content = content + layoutConfig->padding.top + layoutConfig->padding.bottom;
        minContent += Clay__WrapPadding(layoutConfig, false);
    }
    Clay_SizingAxis sizing = crossXAxis ? layoutConfig->sizing.width : layoutConfig->sizing.height;
    float *size = crossXAxis ? &parent->dimensions.width : &parent->dimensions.height;
    float *minSize = crossXAxis ? &parent->minDimensions.width : &parent->minDimensions.height;
    *size = CLAY__MIN(CLAY__MAX(CLAY__MAX(content, *size), sizing.size.minMax.min), sizing.size.minMax.max);
    if (crossXAxis ? parent->config.clip.horizontal : parent->config.clip.vertical) {
        return;
    }
    *minSize = CLAY__MIN(CLAY__MAX(minContent, sizing.size.minMax.min), sizing.size.minMax.max);
}

// Splits the parent's cross-axis slack among its lines the way a row's slack
// is split among GROW children: positive slack grows the smallest lines first,
// negative slack shrinks the largest first (never below zero). A single line
// takes the whole inner size, which is what makes one wrapped line identical
// to a non-wrapping parent.
void Clay__WrapDistributeExtents(Clay_Context *context, Clay_LayoutElement *parent, bool crossXAxis, Clay__int32_tArray *lineIndexBuffer) {
    (void)context;
    Clay_LayoutConfig *layoutConfig = &parent->config.layout;
    int32_t lineCount = parent->wrapLines.length;
    float innerSize = Clay__WrapAxisSize(parent, crossXAxis) - Clay__WrapPadding(layoutConfig, crossXAxis);
    float stacked = 0;
    lineIndexBuffer->length = 0;
    for (int32_t lineIndex = 0; lineIndex < lineCount; lineIndex++) {
        Clay__WrapLine *line = Clay__WrapLineArraySlice_Get(&parent->wrapLines, lineIndex);
        line->extent = line->naturalExtent;
        stacked += line->naturalExtent;
        Clay__int32_tArray_Add(lineIndexBuffer, lineIndex);
    }
    if (lineCount == 1) {
        Clay__WrapLineArraySlice_Get(&parent->wrapLines, 0)->extent = innerSize;
        return;
    }
    stacked += (float)(CLAY__MAX(lineCount - 1, 0) * layoutConfig->childGap);
    float sizeToDistribute = innerSize - stacked;
    if (sizeToDistribute < 0) {
        while (sizeToDistribute < -CLAY__EPSILON && lineIndexBuffer->length > 0) {
            float largest = 0;
            float secondLargest = 0;
            float sizeToAdd = sizeToDistribute;
            for (int32_t i = 0; i < lineIndexBuffer->length; i++) {
                float extent = Clay__WrapLineArraySlice_Get(&parent->wrapLines, Clay__int32_tArray_GetValue(lineIndexBuffer, i))->extent;
                if (Clay__FloatEqual(extent, largest)) { continue; }
                if (extent > largest) {
                    secondLargest = largest;
                    largest = extent;
                }
                if (extent < largest) {
                    secondLargest = CLAY__MAX(secondLargest, extent);
                    sizeToAdd = secondLargest - largest;
                }
            }
            sizeToAdd = CLAY__MAX(sizeToAdd, sizeToDistribute / lineIndexBuffer->length);
            for (int32_t i = 0; i < lineIndexBuffer->length; i++) {
                Clay__WrapLine *line = Clay__WrapLineArraySlice_Get(&parent->wrapLines, Clay__int32_tArray_GetValue(lineIndexBuffer, i));
                float previousExtent = line->extent;
                if (Clay__FloatEqual(line->extent, largest)) {
                    line->extent += sizeToAdd;
                    if (line->extent <= 0) {
                        line->extent = 0;
                        Clay__int32_tArray_RemoveSwapback(lineIndexBuffer, i--);
                    }
                    sizeToDistribute -= (line->extent - previousExtent);
                }
            }
        }
    } else {
        while (sizeToDistribute > CLAY__EPSILON && lineIndexBuffer->length > 0) {
            float smallest = CLAY__MAXFLOAT;
            float secondSmallest = CLAY__MAXFLOAT;
            float sizeToAdd = sizeToDistribute;
            for (int32_t i = 0; i < lineIndexBuffer->length; i++) {
                float extent = Clay__WrapLineArraySlice_Get(&parent->wrapLines, Clay__int32_tArray_GetValue(lineIndexBuffer, i))->extent;
                if (Clay__FloatEqual(extent, smallest)) { continue; }
                if (extent < smallest) {
                    secondSmallest = smallest;
                    smallest = extent;
                }
                if (extent > smallest) {
                    secondSmallest = CLAY__MIN(secondSmallest, extent);
                    sizeToAdd = secondSmallest - smallest;
                }
            }
            sizeToAdd = CLAY__MIN(sizeToAdd, sizeToDistribute / lineIndexBuffer->length);
            for (int32_t i = 0; i < lineIndexBuffer->length; i++) {
                Clay__WrapLine *line = Clay__WrapLineArraySlice_Get(&parent->wrapLines, Clay__int32_tArray_GetValue(lineIndexBuffer, i));
                float previousExtent = line->extent;
                if (Clay__FloatEqual(line->extent, smallest)) {
                    line->extent += sizeToAdd;
                    sizeToDistribute -= (line->extent - previousExtent);
                }
            }
        }
    }
}

// Along-axis branch of Clay__SizeContainersAlongAxis for a wrapping parent:
// pack, then run upstream's shrink / grow distribution once per line. The two
// loops are copied from Clay__SizeContainersAlongAxis so that upstream code
// stays untouched. A TOP_TO_BOTTOM parent (column wrap) also settles its
// cross-axis bookkeeping here, because its children's widths are already final.
void Clay__WrapSizeAlongAxis(Clay_Context *context, Clay_LayoutElement *parent, bool xAxis, Clay__int32_tArray *resizableContainerBuffer) {
    Clay__WrapPackLines(context, parent, xAxis);
    Clay_LayoutConfig *layoutConfig = &parent->config.layout;
    float innerSize = Clay__WrapAxisSize(parent, xAxis) - Clay__WrapPadding(layoutConfig, xAxis);
    bool clipsAxis = xAxis ? parent->config.clip.horizontal : parent->config.clip.vertical;
    for (int32_t lineIndex = 0; lineIndex < parent->wrapLines.length; lineIndex++) {
        Clay__WrapLine *line = Clay__WrapLineArraySlice_Get(&parent->wrapLines, lineIndex);
        resizableContainerBuffer->length = 0;
        int32_t growContainerCount = 0;
        for (int32_t childOffset = line->start; childOffset < line->start + line->count; childOffset++) {
            int32_t childElementIndex = parent->children.elements[childOffset];
            Clay_LayoutElement *child = Clay_LayoutElementArray_Get(&context->layoutElements, childElementIndex);
            if (child->exiting) {
                continue;
            }
            if (Clay__WrapChildIsResizable(child, xAxis)) {
                Clay__int32_tArray_Add(resizableContainerBuffer, childElementIndex);
            }
            if (Clay__GetElementSizing(child, xAxis).type == CLAY__SIZING_TYPE_GROW) {
                growContainerCount++;
            }
        }
        float sizeToDistribute = innerSize - line->contentSize;
        if (sizeToDistribute < 0) {
            if (clipsAxis) {
                continue;
            }
            while (sizeToDistribute < -CLAY__EPSILON && resizableContainerBuffer->length > 0) {
                float largest = 0;
                float secondLargest = 0;
                float widthToAdd = sizeToDistribute;
                for (int childIndex = 0; childIndex < resizableContainerBuffer->length; childIndex++) {
                    Clay_LayoutElement *child = Clay_LayoutElementArray_Get(&context->layoutElements, Clay__int32_tArray_GetValue(resizableContainerBuffer, childIndex));
                    float childSize = xAxis ? child->dimensions.width : child->dimensions.height;
                    if (Clay__FloatEqual(childSize, largest)) { continue; }
                    if (childSize > largest) {
                        secondLargest = largest;
                        largest = childSize;
                    }
                    if (childSize < largest) {
                        secondLargest = CLAY__MAX(secondLargest, childSize);
                        widthToAdd = secondLargest - largest;
                    }
                }
                widthToAdd = CLAY__MAX(widthToAdd, sizeToDistribute / resizableContainerBuffer->length);
                for (int childIndex = 0; childIndex < resizableContainerBuffer->length; childIndex++) {
                    Clay_LayoutElement *child = Clay_LayoutElementArray_Get(&context->layoutElements, Clay__int32_tArray_GetValue(resizableContainerBuffer, childIndex));
                    float *childSize = xAxis ? &child->dimensions.width : &child->dimensions.height;
                    float minSize = xAxis ? child->minDimensions.width : child->minDimensions.height;
                    float previousWidth = *childSize;
                    if (Clay__FloatEqual(*childSize, largest)) {
                        *childSize += widthToAdd;
                        if (*childSize <= minSize) {
                            *childSize = minSize;
                            Clay__int32_tArray_RemoveSwapback(resizableContainerBuffer, childIndex--);
                        }
                        sizeToDistribute -= (*childSize - previousWidth);
                    }
                }
            }
        } else if (sizeToDistribute > 0 && growContainerCount > 0) {
            for (int childIndex = 0; childIndex < resizableContainerBuffer->length; childIndex++) {
                Clay_LayoutElement *child = Clay_LayoutElementArray_Get(&context->layoutElements, Clay__int32_tArray_GetValue(resizableContainerBuffer, childIndex));
                if (Clay__GetElementSizing(child, xAxis).type != CLAY__SIZING_TYPE_GROW) {
                    Clay__int32_tArray_RemoveSwapback(resizableContainerBuffer, childIndex--);
                }
            }
            while (sizeToDistribute > CLAY__EPSILON && resizableContainerBuffer->length > 0) {
                float smallest = CLAY__MAXFLOAT;
                float secondSmallest = CLAY__MAXFLOAT;
                float widthToAdd = sizeToDistribute;
                for (int childIndex = 0; childIndex < resizableContainerBuffer->length; childIndex++) {
                    Clay_LayoutElement *child = Clay_LayoutElementArray_Get(&context->layoutElements, Clay__int32_tArray_GetValue(resizableContainerBuffer, childIndex));
                    float childSize = xAxis ? child->dimensions.width : child->dimensions.height;
                    if (Clay__FloatEqual(childSize, smallest)) { continue; }
                    if (childSize < smallest) {
                        secondSmallest = smallest;
                        smallest = childSize;
                    }
                    if (childSize > smallest) {
                        secondSmallest = CLAY__MIN(secondSmallest, childSize);
                        widthToAdd = secondSmallest - smallest;
                    }
                }
                widthToAdd = CLAY__MIN(widthToAdd, sizeToDistribute / resizableContainerBuffer->length);
                for (int childIndex = 0; childIndex < resizableContainerBuffer->length; childIndex++) {
                    Clay_LayoutElement *child = Clay_LayoutElementArray_Get(&context->layoutElements, Clay__int32_tArray_GetValue(resizableContainerBuffer, childIndex));
                    float *childSize = xAxis ? &child->dimensions.width : &child->dimensions.height;
                    Clay_SizingAxis childSizing = Clay__GetElementSizing(child, xAxis);
                    float maxSize = childSizing.size.minMax.max;
                    float previousWidth = *childSize;
                    if (Clay__FloatEqual(*childSize, smallest)) {
                        *childSize += widthToAdd;
                        if (*childSize >= maxSize) {
                            *childSize = maxSize;
                            Clay__int32_tArray_RemoveSwapback(resizableContainerBuffer, childIndex--);
                        }
                        sizeToDistribute -= (*childSize - previousWidth);
                    }
                }
            }
        }
    }
    if (!xAxis) {
        Clay__WrapUpdateCrossContent(context, parent, true, !context->wrapColumnLinesValid);
        Clay__WrapDistributeExtents(context, parent, true, resizableContainerBuffer);
    }
}

// Cross-axis branch of Clay__SizeContainersAlongAxis for a wrapping parent:
// each child sizes against its line's extent instead of the parent's inner
// size. Mirrors the off-axis loop of Clay__SizeContainersAlongAxis, including
// the clip rule that lets children keep their content size. Returns whether it
// handled the parent; false means upstream's off-axis loop should run (no
// lines were packed).
//
// A column-wrap parent has no lines until the y pass, so in the first sweep's
// x pass its children are left alone at their content widths, exactly as a
// row's children keep their content heights until the y pass. Sizing them
// against the whole parent here would turn every GROW child's grown width into
// its column's "natural" width.
bool Clay__WrapSizeAcrossAxis(Clay_Context *context, Clay_LayoutElement *parent, bool xAxis, Clay__int32_tArray *lineIndexBuffer) {
    if (xAxis && !context->wrapColumnLinesValid) {
        return true;
    }
    if (parent->wrapLines.length == 0) {
        return false;
    }
    Clay__WrapDistributeExtents(context, parent, xAxis, lineIndexBuffer);
    bool clipsAxis = xAxis ? parent->config.clip.horizontal : parent->config.clip.vertical;
    for (int32_t lineIndex = 0; lineIndex < parent->wrapLines.length; lineIndex++) {
        Clay__WrapLine *line = Clay__WrapLineArraySlice_Get(&parent->wrapLines, lineIndex);
        float maxSize = line->extent;
        if (clipsAxis) {
            maxSize = CLAY__MAX(maxSize, line->naturalExtent);
        }
        for (int32_t childOffset = line->start; childOffset < line->start + line->count; childOffset++) {
            Clay_LayoutElement *childElement = Clay_LayoutElementArray_Get(&context->layoutElements, parent->children.elements[childOffset]);
            if (childElement->exiting || !Clay__WrapChildIsResizable(childElement, xAxis)) {
                continue;
            }
            Clay_SizingAxis childSizing = Clay__GetElementSizing(childElement, xAxis);
            float minSize = xAxis ? childElement->minDimensions.width : childElement->minDimensions.height;
            float *childSize = xAxis ? &childElement->dimensions.width : &childElement->dimensions.height;
            if (childSizing.type == CLAY__SIZING_TYPE_GROW) {
                *childSize = CLAY__MIN(maxSize, childSizing.size.minMax.max);
            }
            *childSize = CLAY__MAX(minSize, CLAY__MIN(*childSize, maxSize));
        }
    }
    return true;
}

// ---------------------------------------------------------------------------
// Final positioning: per-line cursors and dividers, called from
// Clay__CalculateFinalLayout.
// ---------------------------------------------------------------------------

// Largest live child of a line on the cross axis, from final child sizes.
float Clay__WrapLineCrossContent(Clay_Context *context, Clay_LayoutElement *parent, Clay__WrapLine *line, bool rows) {
    float largest = 0;
    for (int32_t childOffset = line->start; childOffset < line->start + line->count; childOffset++) {
        Clay_LayoutElement *child = Clay_LayoutElementArray_Get(&context->layoutElements, parent->children.elements[childOffset]);
        if (child->exiting) {
            continue;
        }
        largest = CLAY__MAX(largest, Clay__WrapAxisSize(child, !rows));
    }
    return largest;
}

// How far the cross cursor moves past a line: its extent, or its final content
// when that is larger. A line's extent can shrink below its content when the
// parent is shorter than its stacked lines and a child could not follow (a
// clipping parent leaves children alone; a FIXED or min-clamped child cannot
// shrink), and then the next line must start below that child rather than over
// it; the parent overflows instead, as upstream's rows do.
float Clay__WrapLineAdvance(Clay_Context *context, Clay_LayoutElement *parent, Clay__WrapLine *line, bool rows) {
    return CLAY__MAX(line->extent, Clay__WrapLineCrossContent(context, parent, line, rows));
}

// Replaces the on-axis alignment, scroll content size and child DFS push of
// Clay__CalculateFinalLayout for a wrapping parent. Per line: the line's final
// content is aligned on the wrap axis like upstream aligns a whole row, and
// each child is aligned inside its line's extent on the cross axis.
void Clay__WrapPositionChildren(Clay_Context *context, Clay__LayoutElementTreeNodeArray *dfsBuffer, Clay_LayoutElement *currentElement, Clay_BoundingBox currentElementBoundingBox, Clay_Vector2 scrollOffset, Clay__ScrollContainerDataInternal *scrollContainerData) {
    Clay_LayoutConfig *layoutConfig = &currentElement->config.layout;
    bool rows = layoutConfig->layoutDirection == CLAY_LEFT_TO_RIGHT;
    float alongInner = Clay__WrapAxisSize(currentElement, rows) - Clay__WrapPadding(layoutConfig, rows);
    float alongContent = 0;
    float crossContent = 0;
    float crossCursor = rows ? (float)layoutConfig->padding.top : (float)layoutConfig->padding.left;
    dfsBuffer->length += currentElement->children.length;
    for (int32_t lineIndex = 0; lineIndex < currentElement->wrapLines.length; lineIndex++) {
        Clay__WrapLine *line = Clay__WrapLineArraySlice_Get(&currentElement->wrapLines, lineIndex);
        float lineContent = 0;
        for (int32_t childOffset = line->start; childOffset < line->start + line->count; childOffset++) {
            Clay_LayoutElement *childElement = Clay_LayoutElementArray_Get(&context->layoutElements, currentElement->children.elements[childOffset]);
            if (childElement->exiting) continue;
            lineContent += Clay__WrapAxisSize(childElement, rows);
        }
        lineContent += (float)(CLAY__MAX(line->count - 1, 0) * layoutConfig->childGap);
        float extraSpace = alongInner - lineContent;
        if (rows) {
            switch (layoutConfig->childAlignment.x) {
                case CLAY_ALIGN_X_LEFT: extraSpace = 0; break;
                case CLAY_ALIGN_X_CENTER: extraSpace /= 2; break;
                default: break;
            }
        } else {
            switch (layoutConfig->childAlignment.y) {
                case CLAY_ALIGN_Y_TOP: extraSpace = 0; break;
                case CLAY_ALIGN_Y_CENTER: extraSpace /= 2; break;
                default: break;
            }
        }
        extraSpace = CLAY__MAX(0, extraSpace);
        float alongCursor = (rows ? (float)layoutConfig->padding.left : (float)layoutConfig->padding.top) + extraSpace;
        float lineCrossContent = Clay__WrapLineCrossContent(context, currentElement, line, rows);
        for (int32_t childOffset = line->start; childOffset < line->start + line->count; childOffset++) {
            Clay_LayoutElement *childElement = Clay_LayoutElementArray_Get(&context->layoutElements, currentElement->children.elements[childOffset]);
            float whiteSpaceAroundChild = line->extent - Clay__WrapAxisSize(childElement, !rows);
            float crossOffset = crossCursor;
            if (rows) {
                switch (layoutConfig->childAlignment.y) {
                    case CLAY_ALIGN_Y_TOP: break;
                    case CLAY_ALIGN_Y_CENTER: crossOffset += whiteSpaceAroundChild / 2; break;
                    case CLAY_ALIGN_Y_BOTTOM: crossOffset += whiteSpaceAroundChild; break;
                }
            } else {
                switch (layoutConfig->childAlignment.x) {
                    case CLAY_ALIGN_X_LEFT: break;
                    case CLAY_ALIGN_X_CENTER: crossOffset += whiteSpaceAroundChild / 2; break;
                    case CLAY_ALIGN_X_RIGHT: crossOffset += whiteSpaceAroundChild; break;
                }
            }
            Clay_Vector2 childPosition;
            if (rows) {
                childPosition = CLAY__INIT(Clay_Vector2) { currentElementBoundingBox.x + alongCursor + scrollOffset.x, currentElementBoundingBox.y + crossOffset + scrollOffset.y };
            } else {
                childPosition = CLAY__INIT(Clay_Vector2) { currentElementBoundingBox.x + crossOffset + scrollOffset.x, currentElementBoundingBox.y + alongCursor + scrollOffset.y };
            }
            uint32_t newNodeIndex = dfsBuffer->length - 1 - childOffset;
            dfsBuffer->internalArray[newNodeIndex] = CLAY__INIT(Clay__LayoutElementTreeNode) {
                .layoutElement = childElement,
                .position = childPosition,
                .nextChildOffset = { .x = (float)childElement->config.layout.padding.left, .y = (float)childElement->config.layout.padding.top },
            };
            context->treeNodeVisited.internalArray[newNodeIndex] = false;
            if (!childElement->exiting) {
                alongCursor += Clay__WrapAxisSize(childElement, rows) + (float)layoutConfig->childGap;
            }
        }
        crossCursor += CLAY__MAX(line->extent, lineCrossContent) + (float)layoutConfig->childGap;
        alongContent = CLAY__MAX(alongContent, lineContent);
        crossContent += lineCrossContent;
    }
    crossContent += (float)(CLAY__MAX(currentElement->wrapLines.length - 1, 0) * layoutConfig->childGap);
    if (scrollContainerData) {
        Clay_Dimensions contentSize = rows ? CLAY__INIT(Clay_Dimensions) { alongContent, crossContent } : CLAY__INIT(Clay_Dimensions) { crossContent, alongContent };
        scrollContainerData->contentSize = CLAY__INIT(Clay_Dimensions) { contentSize.width + (float)(layoutConfig->padding.left + layoutConfig->padding.right), contentSize.height + (float)(layoutConfig->padding.top + layoutConfig->padding.bottom) };
    }
}

// Between-children dividers for a wrapping parent. Within a line they follow
// upstream's formula but span only the line's band; between lines a divider
// spans the parent's full size, mirroring upstream's other-direction formula.
// Bands tile the parent: line i runs from the previous line's edge plus half a
// gap to the next line's edge minus half a gap, with the outer lines reaching
// the parent's edges.
void Clay__WrapEmitDividers(Clay_Context *context, Clay_LayoutElement *currentElement, Clay_BoundingBox currentElementBoundingBox, Clay_Vector2 scrollOffset) {
    Clay_LayoutConfig *layoutConfig = &currentElement->config.layout;
    Clay_BorderElementConfig *borderConfig = &currentElement->config.border;
    bool rows = layoutConfig->layoutDirection == CLAY_LEFT_TO_RIGHT;
    float halfGap = layoutConfig->childGap / 2;
    float halfWidth = borderConfig->width.betweenChildren / 2;
    float crossSize = Clay__WrapAxisSize(currentElement, !rows);
    float crossCursor = rows ? (float)layoutConfig->padding.top : (float)layoutConfig->padding.left;
    int32_t lineCount = currentElement->wrapLines.length;
    for (int32_t lineIndex = 0; lineIndex < lineCount; lineIndex++) {
        Clay__WrapLine *line = Clay__WrapLineArraySlice_Get(&currentElement->wrapLines, lineIndex);
        float advance = Clay__WrapLineAdvance(context, currentElement, line, rows);
        float bandStart = lineIndex == 0 ? 0 : crossCursor - halfGap;
        float bandEnd = crossSize;
        if (lineIndex < lineCount - 1) {
            bandEnd = crossCursor + advance + halfGap;
        } else if (lineIndex > 0) {
            // Rigid children can push later lines past the parent's edge; the
            // band then ends at the line's own end so a divider never gets a
            // negative size. A single line keeps upstream's parent-edge band.
            bandEnd = CLAY__MAX(bandEnd, crossCursor + advance);
        }
        if (lineIndex > 0) {
            Clay_BoundingBox dividerBox;
            if (rows) {
                dividerBox = CLAY__INIT(Clay_BoundingBox) { currentElementBoundingBox.x + scrollOffset.x, currentElementBoundingBox.y + (crossCursor - halfGap) + scrollOffset.y - halfWidth, currentElement->dimensions.width, (float)borderConfig->width.betweenChildren };
            } else {
                dividerBox = CLAY__INIT(Clay_BoundingBox) { currentElementBoundingBox.x + (crossCursor - halfGap) + scrollOffset.x - halfWidth, currentElementBoundingBox.y + scrollOffset.y, (float)borderConfig->width.betweenChildren, currentElement->dimensions.height };
            }
            Clay__AddRenderCommand(CLAY__INIT(Clay_RenderCommand) {
                .boundingBox = dividerBox,
                .renderData = { .rectangle = { .backgroundColor = borderConfig->color } },
                .userData = currentElement->config.userData,
                .id = Clay__HashNumber(currentElement->id, 2 * currentElement->children.length + 1 + lineIndex).id,
                .commandType = CLAY_RENDER_COMMAND_TYPE_RECTANGLE,
            });
        }
        float alongOffset = (rows ? (float)layoutConfig->padding.left : (float)layoutConfig->padding.top) - halfGap;
        for (int32_t childOffset = line->start; childOffset < line->start + line->count; childOffset++) {
            Clay_LayoutElement *childElement = Clay_LayoutElementArray_Get(&context->layoutElements, currentElement->children.elements[childOffset]);
            if (childOffset > line->start) {
                Clay_BoundingBox dividerBox;
                if (rows) {
                    dividerBox = CLAY__INIT(Clay_BoundingBox) { currentElementBoundingBox.x + alongOffset + scrollOffset.x - halfWidth, currentElementBoundingBox.y + bandStart + scrollOffset.y, (float)borderConfig->width.betweenChildren, bandEnd - bandStart };
                } else {
                    dividerBox = CLAY__INIT(Clay_BoundingBox) { currentElementBoundingBox.x + bandStart + scrollOffset.x, currentElementBoundingBox.y + alongOffset + scrollOffset.y - halfWidth, bandEnd - bandStart, (float)borderConfig->width.betweenChildren };
                }
                Clay__AddRenderCommand(CLAY__INIT(Clay_RenderCommand) {
                    .boundingBox = dividerBox,
                    .renderData = { .rectangle = { .backgroundColor = borderConfig->color } },
                    .userData = currentElement->config.userData,
                    .id = Clay__HashNumber(currentElement->id, currentElement->children.length + 1 + childOffset).id,
                    .commandType = CLAY_RENDER_COMMAND_TYPE_RECTANGLE,
                });
            }
            alongOffset += Clay__WrapAxisSize(childElement, rows) + (float)layoutConfig->childGap;
        }
        crossCursor += advance + (float)layoutConfig->childGap;
    }
}
