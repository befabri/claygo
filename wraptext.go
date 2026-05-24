package claygo

// wraptext.go ports the text-wrapping pass from Clay__CalculateFinalLayout
// (oracle/clay.h ~line 2587-2639). After sizing(x) has resolved every text
// element's final width, this pass iterates measured words and packs them
// into lines that fit. Each TextElementData.WrappedLines slice points at
// the corresponding segment of Context.wrappedTextLines.
//
// The output of this pass is two-fold:
//  1. WrappedLines on each text leaf, consumed during render-command
//     emission to produce one TEXT command per line.
//  2. The text leaf's Dimensions.Height is set to lineCount * lineHeight,
//     allowing the y-axis sizing pass to grow parents accordingly.

// wrapTextElements builds the WrappedLines view for every text leaf
// collected during sizing(x). Must be called between sizing(x) and
// sizing(y) so the y pass sees the final wrapped heights.
func (c *Context) wrapTextElements() {
	for ti := int32(0); ti < c.textElements.Length; ti++ {
		elementIdx := c.textElements.GetValue(ti)
		element := c.layoutElements.Get(elementIdx)
		textData := &element.TextElementData
		textCfg := &element.TextConfig

		// Anchor this leaf's WrappedLines view at the current pool tail.
		startIdx := c.wrappedTextLines.Length
		textData.WrappedLines.Length = 0
		// Empty slice view initially; populated as lines accumulate.
		textData.WrappedLines.Data = c.wrappedTextLines.Data[startIdx:startIdx]

		cache := c.measureTextCached(textData.Text.Text, textCfg)
		if cache == nil {
			continue
		}

		lineHeight := textData.PreferredDimensions.Height
		if textCfg.LineHeight > 0 {
			lineHeight = float32(textCfg.LineHeight)
		}

		// Fast path: no newlines, fits on one line. Mirrors C 2604-2609.
		if !cache.ContainsNewlines && textData.PreferredDimensions.Width <= element.Dimensions.Width {
			c.wrappedTextLines.Add(WrappedTextLine{
				Dimensions: element.Dimensions,
				Line:       textData.Text,
			})
			textData.WrappedLines.Length++
			textData.WrappedLines.Data = c.wrappedTextLines.Data[startIdx : startIdx+textData.WrappedLines.Length]
			continue
		}

		// Slow path: word-by-word wrap. Mirrors C 2610-2638. Words form a
		// linked list (MeasuredWord.Next) starting at MeasuredWordsStartIndex.
		spaceWidth := float32(0)
		if c.measureText != nil {
			spaceWidth = c.measureText(StringSlice{Text: " ", Base: " "}, textCfg, c.measureTextData).Width
		}

		var lineWidth float32
		var lineLengthChars int32
		var lineStartOffset int32
		wordIndex := cache.MeasuredWordsStartIndex

		text := textData.Text.Text
		containerWidth := element.Dimensions.Width
		letterSpacing := float32(textCfg.LetterSpacing)

		for wordIndex != -1 {
			if c.wrappedTextLines.Length > c.wrappedTextLines.Capacity-1 {
				break
			}
			word := c.measuredWords.Get(wordIndex)

			// Lone-word-too-wide: emit the word as its own line so the user
			// at least sees it, even though it'll clip.
			if lineLengthChars == 0 && lineWidth+word.Width > containerWidth {
				c.wrappedTextLines.Add(WrappedTextLine{
					Dimensions: Dimensions{Width: word.Width, Height: lineHeight},
					Line:       String{Text: text[word.StartOffset : word.StartOffset+word.Length]},
				})
				textData.WrappedLines.Length++
				wordIndex = word.Next
				lineStartOffset = word.StartOffset + word.Length
				continue
			}

			// Newline marker (length == 0) OR next word doesn't fit:
			// flush the current line.
			if word.Length == 0 || lineWidth+word.Width > containerWidth {
				// Trim trailing space if present. Matches C's `text[MAX(idx, 0)]`:
				// when lineStartOffset+lineLengthChars==0 (newline at very start of
				// content), C reads text[0]; we replicate by clamping idx to 0.
				finalCharIsSpace := false
				idx := lineStartOffset + lineLengthChars - 1
				if idx < 0 {
					idx = 0
				}
				if int(idx) < len(text) && text[idx] == ' ' {
					finalCharIsSpace = true
				}
				lineDim := Dimensions{Width: lineWidth, Height: lineHeight}
				lineStr := text[lineStartOffset : lineStartOffset+lineLengthChars]
				if finalCharIsSpace {
					lineDim.Width -= spaceWidth
					lineStr = text[lineStartOffset : lineStartOffset+lineLengthChars-1]
				}
				c.wrappedTextLines.Add(WrappedTextLine{
					Dimensions: lineDim,
					Line:       String{Text: lineStr},
				})
				textData.WrappedLines.Length++

				// Newline + empty-line case advance past the marker; otherwise
				// the overflowing word starts the next line.
				if lineLengthChars == 0 || word.Length == 0 {
					wordIndex = word.Next
				}
				lineWidth = 0
				lineLengthChars = 0
				lineStartOffset = word.StartOffset
				continue
			}

			// Word fits — append it to the current line.
			lineWidth += word.Width + letterSpacing
			lineLengthChars += word.Length
			wordIndex = word.Next
		}

		// Trailing line: any leftover chars get one final line. Trim the
		// final letterSpacing that was added speculatively.
		if lineLengthChars > 0 {
			c.wrappedTextLines.Add(WrappedTextLine{
				Dimensions: Dimensions{Width: lineWidth - letterSpacing, Height: lineHeight},
				Line:       String{Text: text[lineStartOffset : lineStartOffset+lineLengthChars]},
			})
			textData.WrappedLines.Length++
		}

		textData.WrappedLines.Data = c.wrappedTextLines.Data[startIdx : startIdx+textData.WrappedLines.Length]
		element.Dimensions.Height = lineHeight * float32(textData.WrappedLines.Length)
	}
}

// propagateTextHeights walks the layout tree post-order, recomputing parent
// heights from their children. Necessary after text wrapping grows leaves
// taller than they were at close-element time. Mirrors C 2649-2697.
func (c *Context) propagateTextHeights() {
	type frame struct {
		element *LayoutElement
		visited bool
	}
	for rootIdx := int32(0); rootIdx < c.layoutElementTreeRoots.Length; rootIdx++ {
		treeRoot := c.layoutElementTreeRoots.Get(rootIdx)
		stack := []frame{{element: c.layoutElements.Get(treeRoot.LayoutElementIndex)}}

		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			cur := top.element

			if !top.visited {
				top.visited = true
				if cur.IsTextElement || cur.Children.Length == 0 {
					stack = stack[:len(stack)-1]
					continue
				}
				// Push children (order doesn't matter for the post-order
				// recomputation since we only read child heights, not order).
				for i := int32(0); i < cur.Children.Length; i++ {
					stack = append(stack, frame{element: c.layoutElements.Get(cur.Children.Data[i])})
				}
				continue
			}

			stack = stack[:len(stack)-1]

			layoutCfg := &cur.Config.Layout
			switch layoutCfg.LayoutDirection {
			case LeftToRight:
				// Cross-axis (height) = max(child.height + padding, current)
				for j := int32(0); j < cur.Children.Length; j++ {
					child := c.layoutElements.Get(cur.Children.Data[j])
					childHWithPad := child.Dimensions.Height +
						float32(layoutCfg.Padding.Top) + float32(layoutCfg.Padding.Bottom)
					if cur.Dimensions.Height > childHWithPad {
						childHWithPad = cur.Dimensions.Height
					}
					cur.Dimensions.Height = clampFloat32(childHWithPad,
						layoutCfg.Sizing.Height.MinMax.Min, layoutCfg.Sizing.Height.MinMax.Max)
				}
			case TopToBottom:
				// On-axis (height) = padding + sum(child.height) + gaps, clamped.
				contentHeight := float32(layoutCfg.Padding.Top) + float32(layoutCfg.Padding.Bottom)
				for j := int32(0); j < cur.Children.Length; j++ {
					child := c.layoutElements.Get(cur.Children.Data[j])
					contentHeight += child.Dimensions.Height
				}
				contentHeight += float32(maxInt32(cur.Children.Length-1, 0)) * float32(layoutCfg.ChildGap)
				cur.Dimensions.Height = clampFloat32(contentHeight,
					layoutCfg.Sizing.Height.MinMax.Min, layoutCfg.Sizing.Height.MinMax.Max)
			}
		}
	}
}
