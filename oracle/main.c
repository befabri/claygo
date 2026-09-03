// Oracle harness: runs nicbarker/clay against a small corpus of layout scenes
// and dumps each scene's Clay_RenderCommandArray as JSON to stdout. The
// resulting JSON is committed under claygo/testdata/ and used as the
// reference output that the pure-Go port must reproduce.
//
// Build:   make
// Usage:   ./oracle <scene-name>     # writes JSON for one scene to stdout
//          ./oracle --list           # prints all scene names
//
// All scenes use a deterministic fake text measurement function so that
// outputs are byte-identical across runs and platforms, and so the Go side
// can use the same measurement to compare apples to apples.

#define CLAY_IMPLEMENTATION
// The default build compiles the extended header (clay.h + patches/, generated
// by the Makefile) and knows every scene. -DCLAY_ORACLE_UPSTREAM compiles the
// verbatim clay.h and only the upstream scenes; `make verify` uses both.
#ifdef CLAY_ORACLE_UPSTREAM
#include "clay.h"
#else
#include "clay_ext.h"
#endif

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// ---------------------------------------------------------------------------
// Deterministic fake text measurement
// ---------------------------------------------------------------------------
// char_w  = floor(fontSize * 0.55)
// line_h  = lineHeight if > 0 else fontSize + 4
// width   = chars * char_w + max(0, chars-1) * letterSpacing
// Newlines split words; we measure the full string here (Clay slices it per
// word internally). Real renderers will obviously do better, but for golden
// outputs we want a fixed function both sides agree on.
static Clay_Dimensions measure_text(Clay_StringSlice text, Clay_TextElementConfig *cfg, void *user) {
    (void)user;
    float char_w = (float)(int)((float)cfg->fontSize * 0.55f);
    int chars = text.length;
    float gaps = chars > 0 ? (float)(chars - 1) : 0.0f;
    float w = (float)chars * char_w + gaps * (float)cfg->letterSpacing;
    float h = (float)(cfg->lineHeight > 0 ? cfg->lineHeight : (uint16_t)(cfg->fontSize + 4));
    return (Clay_Dimensions){ w, h };
}

static void error_handler(Clay_ErrorData err) {
    fprintf(stderr, "[clay error] type=%d %.*s\n", (int)err.errorType,
            err.errorText.length, err.errorText.chars);
}

// ---------------------------------------------------------------------------
// Deterministic transition handlers
// ---------------------------------------------------------------------------
// Byte-identical to the Go port's linearXInterpolator / exitSlideOff used in
// transitions_test.go, so multi-frame transition scenes produce matching
// golden output. On the first exit frame elapsedTime == 0, so the linear
// interpolation collapses to the initial position (no float drift).

static bool linear_x_interpolator(Clay_TransitionCallbackArguments args) {
    if (args.duration <= 0) {
        if (args.current && (args.properties & CLAY_TRANSITION_PROPERTY_X))
            args.current->boundingBox.x = args.target.boundingBox.x;
        return true;
    }
    float t = args.elapsedTime / args.duration;
    if (t >= 1) t = 1;
    if (args.current && (args.properties & CLAY_TRANSITION_PROPERTY_X))
        args.current->boundingBox.x = args.initial.boundingBox.x +
            (args.target.boundingBox.x - args.initial.boundingBox.x) * t;
    return t >= 1;
}

static Clay_TransitionData exit_slide_off(Clay_TransitionData initialState, Clay_TransitionProperty properties) {
    (void)properties;
    initialState.boundingBox.x = -500;
    return initialState;
}

// ---------------------------------------------------------------------------
// JSON dumping
// ---------------------------------------------------------------------------
// The schema is intentionally minimal and stable. The Go port produces the
// same shape via encoding/json on equivalent structs.

static void j_color(Clay_Color c) {
    printf("[%.9g,%.9g,%.9g,%.9g]", c.r, c.g, c.b, c.a);
}

static void j_corner(Clay_CornerRadius r) {
    printf("[%.9g,%.9g,%.9g,%.9g]", r.topLeft, r.topRight, r.bottomLeft, r.bottomRight);
}

static void j_bbox(Clay_BoundingBox b) {
    printf("{\"x\":%.9g,\"y\":%.9g,\"w\":%.9g,\"h\":%.9g}", b.x, b.y, b.width, b.height);
}

static const char *cmd_type_name(Clay_RenderCommandType t) {
    switch (t) {
        case CLAY_RENDER_COMMAND_TYPE_NONE:                return "NONE";
        case CLAY_RENDER_COMMAND_TYPE_RECTANGLE:           return "RECTANGLE";
        case CLAY_RENDER_COMMAND_TYPE_BORDER:              return "BORDER";
        case CLAY_RENDER_COMMAND_TYPE_TEXT:                return "TEXT";
        case CLAY_RENDER_COMMAND_TYPE_IMAGE:               return "IMAGE";
        case CLAY_RENDER_COMMAND_TYPE_SCISSOR_START:       return "SCISSOR_START";
        case CLAY_RENDER_COMMAND_TYPE_SCISSOR_END:         return "SCISSOR_END";
        case CLAY_RENDER_COMMAND_TYPE_OVERLAY_COLOR_START: return "OVERLAY_COLOR_START";
        case CLAY_RENDER_COMMAND_TYPE_OVERLAY_COLOR_END:   return "OVERLAY_COLOR_END";
        case CLAY_RENDER_COMMAND_TYPE_CUSTOM:              return "CUSTOM";
        default:                                           return "UNKNOWN";
    }
}

static void j_text_escaped(const char *s, int len) {
    printf("\"");
    for (int i = 0; i < len; i++) {
        unsigned char c = (unsigned char)s[i];
        switch (c) {
            case '"':  printf("\\\""); break;
            case '\\': printf("\\\\"); break;
            case '\n': printf("\\n");  break;
            case '\r': printf("\\r");  break;
            case '\t': printf("\\t");  break;
            default:
                if (c < 0x20) printf("\\u%04x", c);
                else          putchar(c);
        }
    }
    printf("\"");
}

static void dump_command(Clay_RenderCommand *cmd) {
    printf("{");
    printf("\"type\":\"%s\",", cmd_type_name(cmd->commandType));
    printf("\"bbox\":");
    j_bbox(cmd->boundingBox);
    printf(",\"zIndex\":%d", (int)cmd->zIndex);
    printf(",\"id\":%u", cmd->id);

    switch (cmd->commandType) {
        case CLAY_RENDER_COMMAND_TYPE_RECTANGLE:
            printf(",\"backgroundColor\":");
            j_color(cmd->renderData.rectangle.backgroundColor);
            printf(",\"cornerRadius\":");
            j_corner(cmd->renderData.rectangle.cornerRadius);
            break;
        case CLAY_RENDER_COMMAND_TYPE_BORDER:
            printf(",\"color\":");
            j_color(cmd->renderData.border.color);
            printf(",\"cornerRadius\":");
            j_corner(cmd->renderData.border.cornerRadius);
            printf(",\"width\":[%u,%u,%u,%u,%u]",
                   cmd->renderData.border.width.left,
                   cmd->renderData.border.width.right,
                   cmd->renderData.border.width.top,
                   cmd->renderData.border.width.bottom,
                   cmd->renderData.border.width.betweenChildren);
            break;
        case CLAY_RENDER_COMMAND_TYPE_TEXT:
            printf(",\"text\":");
            j_text_escaped(cmd->renderData.text.stringContents.chars,
                           cmd->renderData.text.stringContents.length);
            printf(",\"color\":");
            j_color(cmd->renderData.text.textColor);
            printf(",\"fontId\":%u,\"fontSize\":%u,\"letterSpacing\":%u,\"lineHeight\":%u",
                   cmd->renderData.text.fontId,
                   cmd->renderData.text.fontSize,
                   cmd->renderData.text.letterSpacing,
                   cmd->renderData.text.lineHeight);
            break;
        case CLAY_RENDER_COMMAND_TYPE_SCISSOR_START:
        case CLAY_RENDER_COMMAND_TYPE_SCISSOR_END:
            printf(",\"horizontal\":%s,\"vertical\":%s",
                   cmd->renderData.clip.horizontal ? "true" : "false",
                   cmd->renderData.clip.vertical   ? "true" : "false");
            break;
        case CLAY_RENDER_COMMAND_TYPE_OVERLAY_COLOR_START:
        case CLAY_RENDER_COMMAND_TYPE_OVERLAY_COLOR_END:
            printf(",\"color\":");
            j_color(cmd->renderData.overlayColor.color);
            break;
        case CLAY_RENDER_COMMAND_TYPE_IMAGE:
            printf(",\"backgroundColor\":");
            j_color(cmd->renderData.image.backgroundColor);
            printf(",\"cornerRadius\":");
            j_corner(cmd->renderData.image.cornerRadius);
            break;
        default:
            break;
    }
    printf("}");
}

static void dump_array(Clay_RenderCommandArray arr) {
    printf("{\"commands\":[");
    for (int32_t i = 0; i < arr.length; i++) {
        if (i > 0) printf(",");
        dump_command(Clay_RenderCommandArray_Get(&arr, i));
    }
    printf("]}\n");
}

// ---------------------------------------------------------------------------
// Test scenes
// ---------------------------------------------------------------------------
// Each scene is a self-contained function that calls Clay_BeginLayout,
// declares its tree, and returns the result of Clay_EndLayout. All scenes
// run inside a 1280x720 viewport.

static Clay_RenderCommandArray scene_rect_solid(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_GROW(0) },
        },
        .backgroundColor = { 40, 40, 48, 255 },
    });
    return Clay_EndLayout(0.0f);
}

static Clay_RenderCommandArray scene_padded_rect(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_GROW(0) },
            .padding = CLAY_PADDING_ALL(16),
        },
        .backgroundColor = { 40, 40, 48, 255 },
    }) {
        CLAY_AUTO_ID({
            .layout = {
                .sizing = { CLAY_SIZING_FIXED(200), CLAY_SIZING_FIXED(100) },
            },
            .backgroundColor = { 200, 80, 80, 255 },
        });
    }
    return Clay_EndLayout(0.0f);
}

static Clay_RenderCommandArray scene_row_3_fixed(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_GROW(0) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 8,
            .layoutDirection = CLAY_LEFT_TO_RIGHT,
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(100), CLAY_SIZING_FIXED(60) } },
               .backgroundColor = { 200, 80, 80, 255 } });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(150), CLAY_SIZING_FIXED(60) } },
               .backgroundColor = { 80, 200, 80, 255 } });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(80),  CLAY_SIZING_FIXED(60) } },
               .backgroundColor = { 80, 80, 200, 255 } });
    }
    return Clay_EndLayout(0.0f);
}

static Clay_RenderCommandArray scene_row_3_grow(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_GROW(0) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 8,
            .layoutDirection = CLAY_LEFT_TO_RIGHT,
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_FIXED(60) } },
               .backgroundColor = { 200, 80, 80, 255 } });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_FIXED(60) } },
               .backgroundColor = { 80, 200, 80, 255 } });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_FIXED(60) } },
               .backgroundColor = { 80, 80, 200, 255 } });
    }
    return Clay_EndLayout(0.0f);
}

// TOP_TO_BOTTOM mirror of row_3_grow: 3 children share 688 px on the y axis
// (720 - 16 padding - 2*8 gaps). Exercises the y-axis branch of the sizing
// solver and the vertical layout path.
static Clay_RenderCommandArray scene_col_3_grow(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_GROW(0) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 8,
            .layoutDirection = CLAY_TOP_TO_BOTTOM,
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(200), CLAY_SIZING_GROW(0) } },
               .backgroundColor = { 200, 80, 80, 255 } });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(200), CLAY_SIZING_GROW(0) } },
               .backgroundColor = { 80, 200, 80, 255 } });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(200), CLAY_SIZING_GROW(0) } },
               .backgroundColor = { 80, 80, 200, 255 } });
    }
    return Clay_EndLayout(0.0f);
}

// 1 FIXED + 2 GROW siblings. GROW must only divide leftover space:
// inner = 1280 - 2*8 padding = 1264; leftover = 1264 - 300 fixed - 2*8 gaps =
// 948; each GROW = 948 / 2 = 474. (The earlier "466" math was wrong: it
// double-counted the right padding and miscounted the gaps.)
static Clay_RenderCommandArray scene_mixed_fixed_grow(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_GROW(0) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 8,
            .layoutDirection = CLAY_LEFT_TO_RIGHT,
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(300), CLAY_SIZING_FIXED(100) } },
               .backgroundColor = { 200, 80, 80, 255 } });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_FIXED(100) } },
               .backgroundColor = { 80, 200, 80, 255 } });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_FIXED(100) } },
               .backgroundColor = { 80, 80, 200, 255 } });
    }
    return Clay_EndLayout(0.0f);
}

// Child with PERCENT(0.5) on both axes. Avail = 1248 x 688 (after 16 padding).
// Child should be 624 x 344.
static Clay_RenderCommandArray scene_percent_half(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_GROW(0) },
            .padding = CLAY_PADDING_ALL(16),
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        CLAY_AUTO_ID({
            .layout = { .sizing = { CLAY_SIZING_PERCENT(0.5f), CLAY_SIZING_PERCENT(0.5f) } },
            .backgroundColor = { 200, 80, 80, 255 },
        });
    }
    return Clay_EndLayout(0.0f);
}

// GROW with min/max bounds. First child capped at max=200, second is
// unbounded GROW that should absorb the rest.
static Clay_RenderCommandArray scene_min_max_clamp(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_GROW(0) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 8,
            .layoutDirection = CLAY_LEFT_TO_RIGHT,
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_GROW(50, 200), CLAY_SIZING_FIXED(100) } },
               .backgroundColor = { 200, 80, 80, 255 } });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_FIXED(100) } },
               .backgroundColor = { 80, 200, 80, 255 } });
    }
    return Clay_EndLayout(0.0f);
}

// Parent uses FIT sizing on both axes; final size must hug its children
// plus padding+gaps. Expected: 8+100+8+150+8 = 274 wide, 8+max(60,80)+8 = 96 tall.
static Clay_RenderCommandArray scene_fit_to_children(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIT(0), CLAY_SIZING_FIT(0) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 8,
            .layoutDirection = CLAY_LEFT_TO_RIGHT,
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(100), CLAY_SIZING_FIXED(60) } },
               .backgroundColor = { 200, 80, 80, 255 } });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(150), CLAY_SIZING_FIXED(80) } },
               .backgroundColor = { 80, 200, 80, 255 } });
    }
    return Clay_EndLayout(0.0f);
}

// ChildAlignment center on both axes. Child should land at
// x=(1280-200)/2=540, y=(720-100)/2=310.
static Clay_RenderCommandArray scene_align_center(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_GROW(0) },
            .childAlignment = { CLAY_ALIGN_X_CENTER, CLAY_ALIGN_Y_CENTER },
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        CLAY_AUTO_ID({
            .layout = { .sizing = { CLAY_SIZING_FIXED(200), CLAY_SIZING_FIXED(100) } },
            .backgroundColor = { 200, 80, 80, 255 },
        });
    }
    return Clay_EndLayout(0.0f);
}

// ChildAlignment right + bottom, with 16px padding. Child anchored at
// x=1280-16-200=1064, y=720-16-100=604.
static Clay_RenderCommandArray scene_align_right_bottom(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_GROW(0) },
            .padding = CLAY_PADDING_ALL(16),
            .childAlignment = { CLAY_ALIGN_X_RIGHT, CLAY_ALIGN_Y_BOTTOM },
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        CLAY_AUTO_ID({
            .layout = { .sizing = { CLAY_SIZING_FIXED(200), CLAY_SIZING_FIXED(100) } },
            .backgroundColor = { 200, 80, 80, 255 },
        });
    }
    return Clay_EndLayout(0.0f);
}

// A single TEXT element inside a FIT parent. Exercises text measurement +
// TEXT render command emission. With our deterministic measurement,
// "Hello World" = 11 chars * floor(16*0.55)=8 = 88px wide, line height 20px.
// Parent fits to 8+88+8 = 104 wide, 8+20+8 = 36 tall.
static Clay_RenderCommandArray scene_text_simple(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIT(0), CLAY_SIZING_FIT(0) },
            .padding = CLAY_PADDING_ALL(8),
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        CLAY_TEXT(CLAY_STRING("Hello World"), CLAY_TEXT_CONFIG({
            .textColor = { 240, 240, 240, 255 },
            .fontSize = 16,
        }));
    }
    return Clay_EndLayout(0.0f);
}

// Element with corner radius on its background rectangle. Exercises the
// cornerRadius field of RECTANGLE render commands.
static Clay_RenderCommandArray scene_corner_radius(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = { .sizing = { CLAY_SIZING_FIXED(200), CLAY_SIZING_FIXED(100) } },
        .backgroundColor = { 200, 80, 80, 255 },
        .cornerRadius = { 12, 12, 12, 12 },
    });
    return Clay_EndLayout(0.0f);
}

// Element with uniform border on all sides + corner radius. Emits a BORDER
// command (with the same bounding box as the parent).
static Clay_RenderCommandArray scene_border_basic(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = { .sizing = { CLAY_SIZING_FIXED(200), CLAY_SIZING_FIXED(100) } },
        .backgroundColor = { 30, 30, 36, 255 },
        .cornerRadius = { 8, 8, 8, 8 },
        .border = {
            .color = { 240, 200, 80, 255 },
            .width = { .left = 2, .right = 2, .top = 2, .bottom = 2 },
        },
    });
    return Clay_EndLayout(0.0f);
}

// Element with .border.width.betweenChildren > 0. Emits per-divider
// RECTANGLE commands between siblings (vertical dividers for LEFT_TO_RIGHT).
static Clay_RenderCommandArray scene_border_between_children(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIT(0), CLAY_SIZING_FIXED(60) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 8,
            .layoutDirection = CLAY_LEFT_TO_RIGHT,
        },
        .backgroundColor = { 30, 30, 36, 255 },
        .border = {
            .color = { 240, 200, 80, 255 },
            .width = { .betweenChildren = 2 },
        },
    }) {
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(60), CLAY_SIZING_FIXED(40) } },
               .backgroundColor = { 200, 80, 80, 255 } });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(60), CLAY_SIZING_FIXED(40) } },
               .backgroundColor = { 80, 200, 80, 255 } });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(60), CLAY_SIZING_FIXED(40) } },
               .backgroundColor = { 80, 80, 200, 255 } });
    }
    return Clay_EndLayout(0.0f);
}

// Parent with overlayColor wraps a child; the renderer should produce
// OVERLAY_COLOR_START before the child, and OVERLAY_COLOR_END after.
static Clay_RenderCommandArray scene_overlay_color(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIT(0), CLAY_SIZING_FIT(0) },
            .padding = CLAY_PADDING_ALL(8),
        },
        .backgroundColor = { 30, 30, 36, 255 },
        .overlayColor = { 255, 0, 0, 128 },
    }) {
        CLAY_AUTO_ID({
            .layout = { .sizing = { CLAY_SIZING_FIXED(100), CLAY_SIZING_FIXED(60) } },
            .backgroundColor = { 200, 80, 80, 255 },
        });
    }
    return Clay_EndLayout(0.0f);
}

// Element with an image declaration. Emits IMAGE render command. The
// imageData pointer is opaque (not dumped to JSON); we pass an arbitrary
// non-null sentinel so the emission path is exercised.
static Clay_RenderCommandArray scene_image_basic(void) {
    Clay_BeginLayout();
    static int fake_image_data = 1; // any non-null pointer
    CLAY_AUTO_ID({
        .layout = { .sizing = { CLAY_SIZING_FIXED(200), CLAY_SIZING_FIXED(120) } },
        .backgroundColor = { 255, 255, 255, 255 }, // tint
        .cornerRadius = { 4, 4, 4, 4 },
        .image = { .imageData = &fake_image_data },
    });
    return Clay_EndLayout(0.0f);
}

// Long text inside a narrow FIT parent, with TEXT_WRAP_WORDS. The text
// should wrap into multiple lines.
//
// Deterministic measurer: chars*8 (fontSize 16, charW=8). "The quick brown
// fox jumps over the lazy dog" is 43 chars = 344px unwrapped. We constrain
// the parent to a 200px width and require word-wrap, producing several
// shorter lines.
static Clay_RenderCommandArray scene_text_wrap_words(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(200), CLAY_SIZING_FIT(0) },
            .padding = CLAY_PADDING_ALL(8),
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        CLAY_TEXT(CLAY_STRING("The quick brown fox jumps over the lazy dog"),
            CLAY_TEXT_CONFIG({
                .textColor = { 240, 240, 240, 255 },
                .fontSize = 16,
                .wrapMode = CLAY_TEXT_WRAP_WORDS,
            }));
    }
    return Clay_EndLayout(0.0f);
}

// Element with explicit aspect ratio. Sized FIXED width, no height set —
// the aspect ratio resolver should compute height = width / aspectRatio.
// At width=200 and aspectRatio=2.0, expected height = 100.
static Clay_RenderCommandArray scene_aspect_ratio(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = {
                CLAY_SIZING_FIXED(200),
                CLAY_SIZING_FIT(0), // height will be derived
            },
        },
        .backgroundColor = { 200, 80, 80, 255 },
        .aspectRatio = { .aspectRatio = 2.0f },
    });
    return Clay_EndLayout(0.0f);
}

// Three-level nesting: outer (GROW) → middle (FIT padding 16) → inner
// (FIXED 100x60). Confirms the DFS and sizing solver descend properly.
static Clay_RenderCommandArray scene_nested_3_levels(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = { .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_GROW(0) } },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        CLAY_AUTO_ID({
            .layout = {
                .sizing = { CLAY_SIZING_FIT(0), CLAY_SIZING_FIT(0) },
                .padding = CLAY_PADDING_ALL(16),
            },
            .backgroundColor = { 80, 80, 120, 255 },
        }) {
            CLAY_AUTO_ID({
                .layout = { .sizing = { CLAY_SIZING_FIXED(100), CLAY_SIZING_FIXED(60) } },
                .backgroundColor = { 200, 80, 80, 255 },
            });
        }
    }
    return Clay_EndLayout(0.0f);
}

// Clip container with horizontal+vertical clipping enabled, containing a
// child larger than the parent. The renderer should emit SCISSOR_START
// around the parent and SCISSOR_END after.
static Clay_RenderCommandArray scene_clip_overflow(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = { .sizing = { CLAY_SIZING_FIXED(120), CLAY_SIZING_FIXED(80) } },
        .backgroundColor = { 30, 30, 36, 255 },
        .clip = { .horizontal = true, .vertical = true },
    }) {
        // Child is larger than parent (200x150 inside a 120x80 container).
        CLAY_AUTO_ID({
            .layout = { .sizing = { CLAY_SIZING_FIXED(200), CLAY_SIZING_FIXED(150) } },
            .backgroundColor = { 200, 80, 80, 255 },
        });
    }
    return Clay_EndLayout(0.0f);
}

// Floating element attached to its parent's bottom edge. Produces a new
// tree root; the floating child renders at the parent's right-bottom
// corner with the configured offset.
static Clay_RenderCommandArray scene_floating_parent(void) {
    Clay_BeginLayout();
    CLAY(CLAY_ID("Anchor"), {
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(300), CLAY_SIZING_FIXED(200) },
            .padding = CLAY_PADDING_ALL(8),
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        CLAY_AUTO_ID({
            .layout = { .sizing = { CLAY_SIZING_FIXED(100), CLAY_SIZING_FIXED(60) } },
            .backgroundColor = { 200, 80, 80, 255 },
            .floating = {
                .attachTo = CLAY_ATTACH_TO_PARENT,
                .attachPoints = {
                    .parent = CLAY_ATTACH_POINT_LEFT_BOTTOM,
                    .element = CLAY_ATTACH_POINT_LEFT_TOP,
                },
            },
        });
    }
    return Clay_EndLayout(0.0f);
}

// border_between_children with TOP_TO_BOTTOM layout — verifies horizontal
// divider rectangles spanning content width at gaps between siblings.
// Mirror of border_between_children but column-direction.
static Clay_RenderCommandArray scene_border_between_children_col(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(80), CLAY_SIZING_FIT(0) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 8,
            .layoutDirection = CLAY_TOP_TO_BOTTOM,
        },
        .backgroundColor = { 30, 30, 36, 255 },
        .border = {
            .color = { 240, 200, 80, 255 },
            .width = { .betweenChildren = 2 },
        },
    }) {
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(60), CLAY_SIZING_FIXED(40) } },
               .backgroundColor = { 200, 80, 80, 255 } });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(60), CLAY_SIZING_FIXED(40) } },
               .backgroundColor = { 80, 200, 80, 255 } });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(60), CLAY_SIZING_FIXED(40) } },
               .backgroundColor = { 80, 80, 200, 255 } });
    }
    return Clay_EndLayout(0.0f);
}

// Text wrapping with TextAlignment=CENTER. Each line's x is centered
// within the container's inner width. Verifies alignment offset math.
static Clay_RenderCommandArray scene_text_align_center(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(200), CLAY_SIZING_FIT(0) },
            .padding = CLAY_PADDING_ALL(8),
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        CLAY_TEXT(CLAY_STRING("The quick brown fox jumps over the lazy dog"),
            CLAY_TEXT_CONFIG({
                .textColor = { 240, 240, 240, 255 },
                .fontSize = 16,
                .wrapMode = CLAY_TEXT_WRAP_WORDS,
                .textAlignment = CLAY_TEXT_ALIGN_CENTER,
            }));
    }
    return Clay_EndLayout(0.0f);
}

// Text wrapping with TextAlignment=RIGHT. Each line's x is offset to align
// the right edge of the line text with the right edge of the container.
static Clay_RenderCommandArray scene_text_align_right(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(200), CLAY_SIZING_FIT(0) },
            .padding = CLAY_PADDING_ALL(8),
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        CLAY_TEXT(CLAY_STRING("The quick brown fox jumps over the lazy dog"),
            CLAY_TEXT_CONFIG({
                .textColor = { 240, 240, 240, 255 },
                .fontSize = 16,
                .wrapMode = CLAY_TEXT_WRAP_WORDS,
                .textAlignment = CLAY_TEXT_ALIGN_RIGHT,
            }));
    }
    return Clay_EndLayout(0.0f);
}

// Two floating siblings with explicit Z indices. The z=1 root renders
// after the z=2 root in declaration order, but after the bubble sort
// by zIndex the z=1 commands appear before the z=2 commands in the
// output. Pins the tree-root z-sort comparator.
static Clay_RenderCommandArray scene_floating_z_sort(void) {
    Clay_BeginLayout();
    CLAY(CLAY_ID("AnchorZ"), {
        .layout = { .sizing = { CLAY_SIZING_FIXED(300), CLAY_SIZING_FIXED(200) } },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        // First declared, but zIndex=2 → renders LAST in output.
        CLAY_AUTO_ID({
            .layout = { .sizing = { CLAY_SIZING_FIXED(80), CLAY_SIZING_FIXED(60) } },
            .backgroundColor = { 200, 80, 80, 255 },
            .floating = {
                .zIndex = 2,
                .attachTo = CLAY_ATTACH_TO_PARENT,
                .attachPoints = { .parent = CLAY_ATTACH_POINT_LEFT_TOP, .element = CLAY_ATTACH_POINT_LEFT_TOP },
            },
        });
        // Second declared, but zIndex=1 → renders BEFORE the red.
        CLAY_AUTO_ID({
            .layout = { .sizing = { CLAY_SIZING_FIXED(80), CLAY_SIZING_FIXED(60) } },
            .backgroundColor = { 80, 200, 80, 255 },
            .floating = {
                .zIndex = 1,
                .attachTo = CLAY_ATTACH_TO_PARENT,
                .attachPoints = { .parent = CLAY_ATTACH_POINT_LEFT_TOP, .element = CLAY_ATTACH_POINT_LEFT_TOP },
                .offset = { 100, 0 },
            },
        });
    }
    return Clay_EndLayout(0.0f);
}

// Clip container with a non-zero ChildOffset — children should be
// translated by the offset inside the SCISSOR pair. Verifies
// Clip.ChildOffset arithmetic.
static Clay_RenderCommandArray scene_clip_scroll_offset(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = { .sizing = { CLAY_SIZING_FIXED(120), CLAY_SIZING_FIXED(80) } },
        .backgroundColor = { 30, 30, 36, 255 },
        .clip = {
            .horizontal = true,
            .vertical = true,
            .childOffset = { -20, -10 },
        },
    }) {
        CLAY_AUTO_ID({
            .layout = { .sizing = { CLAY_SIZING_FIXED(200), CLAY_SIZING_FIXED(150) } },
            .backgroundColor = { 200, 80, 80, 255 },
        });
    }
    return Clay_EndLayout(0.0f);
}

// Floating element nested inside a clip parent. The floating element
// becomes its own tree root that inherits the clip ancestor and emits
// SCISSOR_START/END around its subtree to mask against the clip's bbox.
static Clay_RenderCommandArray scene_floating_in_clip(void) {
    Clay_BeginLayout();
    CLAY(CLAY_ID("ClipParent"), {
        .layout = { .sizing = { CLAY_SIZING_FIXED(200), CLAY_SIZING_FIXED(150) } },
        .backgroundColor = { 30, 30, 36, 255 },
        .clip = { .horizontal = true, .vertical = true },
    }) {
        // Floating child stays clipped to the parent because clipTo defaults
        // to CLAY_CLIP_TO_NONE in upstream — to inherit the clip ancestor we
        // explicitly set CLAY_CLIP_TO_ATTACHED_PARENT.
        CLAY_AUTO_ID({
            .layout = { .sizing = { CLAY_SIZING_FIXED(80), CLAY_SIZING_FIXED(60) } },
            .backgroundColor = { 200, 80, 80, 255 },
            .floating = {
                .attachTo = CLAY_ATTACH_TO_PARENT,
                .attachPoints = { .parent = CLAY_ATTACH_POINT_LEFT_TOP, .element = CLAY_ATTACH_POINT_LEFT_TOP },
                .clipTo = CLAY_CLIP_TO_ATTACHED_PARENT,
                .offset = { 10, 10 },
            },
        });
    }
    return Clay_EndLayout(0.0f);
}

// Text wrap inside a clip container — the wrap pass runs first, then the
// scissor wraps each TEXT command. Exercises the wrap × scissor interaction.
static Clay_RenderCommandArray scene_clip_text_wrap(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(160), CLAY_SIZING_FIXED(40) },
            .padding = CLAY_PADDING_ALL(4),
        },
        .backgroundColor = { 30, 30, 36, 255 },
        .clip = { .horizontal = true, .vertical = true },
    }) {
        CLAY_TEXT(CLAY_STRING("The quick brown fox jumps over the lazy dog"),
            CLAY_TEXT_CONFIG({
                .textColor = { 240, 240, 240, 255 },
                .fontSize = 16,
                .wrapMode = CLAY_TEXT_WRAP_WORDS,
            }));
    }
    return Clay_EndLayout(0.0f);
}

// Element with image data + corner radius + border + overlay — exercises
// the full set of render commands in one element. The Go port must emit
// in order: OVERLAY_COLOR_START, IMAGE, RECTANGLE (background), BORDER,
// OVERLAY_COLOR_END.
static Clay_RenderCommandArray scene_image_full_stack(void) {
    Clay_BeginLayout();
    static int fake_image_data = 1;
    CLAY_AUTO_ID({
        .layout = { .sizing = { CLAY_SIZING_FIXED(200), CLAY_SIZING_FIXED(120) } },
        .backgroundColor = { 255, 255, 255, 255 },
        .overlayColor = { 0, 0, 255, 80 },
        .cornerRadius = { 8, 8, 8, 8 },
        .border = {
            .color = { 240, 200, 80, 255 },
            .width = { .left = 2, .right = 2, .top = 2, .bottom = 2 },
        },
        .image = { .imageData = &fake_image_data },
    });
    return Clay_EndLayout(0.0f);
}

// 7 GROW children sharing 1216 px (1280 - 16 padding - 6*8 gaps). 1216/7 is
// non-integer, exercising whatever fractional layout policy Clay applies.
static Clay_RenderCommandArray scene_grow_7_nonint(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_FIXED(80) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 8,
            .layoutDirection = CLAY_LEFT_TO_RIGHT,
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        for (int i = 0; i < 7; i++) {
            CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_FIXED(60) } },
                   .backgroundColor = { 200, 80, 80, 255 } });
        }
    }
    return Clay_EndLayout(0.0f);
}

// ---------------------------------------------------------------------------
// Multi-frame exit-transition scenes
// ---------------------------------------------------------------------------
// These scenes run multiple frames against the same context (the harness only
// initializes once per scene). Frame 1 declares the tree; later frames omit it
// so exit transitions can be captured at first-frame, mid-exit, and completion
// points.
//
// They pin upstream's nested-exit behavior: when an exiting parent's subtree
// is cloned, a nested child that ALSO has its own exit transition has its
// transition record removed and is then skipped by the render pass
// (clay.h:2933-2937), whereas a plain child is cloned and rendered.

#define ORACLE_TRANSITION_DELTA 0.1f

static void declare_exit_single(float duration) {
    Clay_TransitionElementConfig transition = {
        .handler = linear_x_interpolator,
        .duration = duration,
        .properties = CLAY_TRANSITION_PROPERTY_X,
        .exit = { .setFinalState = exit_slide_off },
    };
    Clay_BeginLayout();
    CLAY(CLAY_ID("ExitSingle"), {
        .layout = { .sizing = { CLAY_SIZING_FIXED(100), CLAY_SIZING_FIXED(100) } },
        .backgroundColor = { 80, 80, 80, 255 },
        .transition = transition,
    });
}

// Frame 3 should render the exiting rectangle at x=-50: frame 2 starts EXITING
// with elapsedTime=0, then frame 3 applies elapsedTime=0.1 over duration=1.0.
static Clay_RenderCommandArray scene_exit_single_mid(void) {
    declare_exit_single(1.0f);
    Clay_EndLayout(ORACLE_TRANSITION_DELTA);

    Clay_BeginLayout();
    Clay_EndLayout(ORACLE_TRANSITION_DELTA);

    Clay_BeginLayout();
    return Clay_EndLayout(ORACLE_TRANSITION_DELTA);
}

// Duration equals the per-frame delta. Frame 3 completes the exit between the
// two transition layout passes, so the visible pass should emit no rectangle.
static Clay_RenderCommandArray scene_exit_single_completed(void) {
    declare_exit_single(ORACLE_TRANSITION_DELTA);
    Clay_EndLayout(ORACLE_TRANSITION_DELTA);

    Clay_BeginLayout();
    Clay_EndLayout(ORACLE_TRANSITION_DELTA);

    Clay_BeginLayout();
    return Clay_EndLayout(ORACLE_TRANSITION_DELTA);
}

// Parent + child where BOTH configure an exit transition. Frame 2 should emit
// ONLY the parent rectangle — the nested child is removed and skipped.
static Clay_RenderCommandArray scene_exit_nested_child_with_exit(void) {
    Clay_TransitionElementConfig transition = {
        .handler = linear_x_interpolator,
        .duration = 1.0f,
        .properties = CLAY_TRANSITION_PROPERTY_X,
        .exit = { .setFinalState = exit_slide_off },
    };
    Clay_BeginLayout();
    CLAY(CLAY_ID("NEParent"), {
        .layout = { .sizing = { CLAY_SIZING_FIXED(120), CLAY_SIZING_FIXED(120) }, .layoutDirection = CLAY_TOP_TO_BOTTOM },
        .backgroundColor = { 100, 100, 200, 255 },
        .transition = transition,
    }) {
        CLAY(CLAY_ID("NEChild"), {
            .layout = { .sizing = { CLAY_SIZING_FIXED(80), CLAY_SIZING_FIXED(40) } },
            .backgroundColor = { 200, 50, 50, 255 },
            .transition = transition,
        });
    }
    Clay_EndLayout(ORACLE_TRANSITION_DELTA);

    Clay_BeginLayout();
    return Clay_EndLayout(ORACLE_TRANSITION_DELTA);
}

// Parent with an exit transition, child with NONE. Frame 2 should emit BOTH
// the parent and the (cloned) child rectangle.
static Clay_RenderCommandArray scene_exit_nested_child_plain(void) {
    Clay_TransitionElementConfig transition = {
        .handler = linear_x_interpolator,
        .duration = 1.0f,
        .properties = CLAY_TRANSITION_PROPERTY_X,
        .exit = { .setFinalState = exit_slide_off },
    };
    Clay_BeginLayout();
    CLAY(CLAY_ID("NPParent"), {
        .layout = { .sizing = { CLAY_SIZING_FIXED(120), CLAY_SIZING_FIXED(120) }, .layoutDirection = CLAY_TOP_TO_BOTTOM },
        .backgroundColor = { 100, 100, 200, 255 },
        .transition = transition,
    }) {
        CLAY(CLAY_ID("NPChild"), {
            .layout = { .sizing = { CLAY_SIZING_FIXED(80), CLAY_SIZING_FIXED(40) } },
            .backgroundColor = { 200, 50, 50, 255 },
        });
    }
    Clay_EndLayout(ORACLE_TRANSITION_DELTA);

    Clay_BeginLayout();
    return Clay_EndLayout(ORACLE_TRANSITION_DELTA);
}

// Odd childGap and betweenChildren width: upstream halves both with integer
// division (`childGap / 2` on a uint16_t), so gap 7 → 3 and width 3 → 1. Pins
// that truncation, which even gaps never exercise.
static Clay_RenderCommandArray scene_border_between_children_odd_gap(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIT(0), CLAY_SIZING_FIXED(60) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 7,
            .layoutDirection = CLAY_LEFT_TO_RIGHT,
        },
        .backgroundColor = { 30, 30, 36, 255 },
        .border = {
            .color = { 240, 200, 80, 255 },
            .width = { .betweenChildren = 3 },
        },
    }) {
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(60), CLAY_SIZING_FIXED(40) } },
               .backgroundColor = { 200, 80, 80, 255 } });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(60), CLAY_SIZING_FIXED(40) } },
               .backgroundColor = { 80, 200, 80, 255 } });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(60), CLAY_SIZING_FIXED(40) } },
               .backgroundColor = { 80, 80, 200, 255 } });
    }
    return Clay_EndLayout(0.0f);
}


// ---------------------------------------------------------------------------
// Scene dispatch
// ---------------------------------------------------------------------------

typedef struct {
    const char *name;
    Clay_RenderCommandArray (*fn)(void);
} Scene;

static Scene SCENES[] = {
    { "rect_solid",                 scene_rect_solid                 },
    { "padded_rect",                scene_padded_rect                },
    { "row_3_fixed",                scene_row_3_fixed                },
    { "row_3_grow",                 scene_row_3_grow                 },
    { "col_3_grow",                 scene_col_3_grow                 },
    { "mixed_fixed_grow",           scene_mixed_fixed_grow           },
    { "percent_half",               scene_percent_half               },
    { "min_max_clamp",              scene_min_max_clamp              },
    { "fit_to_children",            scene_fit_to_children            },
    { "align_center",               scene_align_center               },
    { "align_right_bottom",         scene_align_right_bottom         },
    { "text_simple",                scene_text_simple                },
    { "corner_radius",              scene_corner_radius              },
    { "border_basic",               scene_border_basic               },
    { "border_between_children",    scene_border_between_children    },
    { "border_between_children_col",scene_border_between_children_col},
    { "overlay_color",              scene_overlay_color              },
    { "image_basic",                scene_image_basic                },
    { "image_full_stack",           scene_image_full_stack           },
    { "text_wrap_words",            scene_text_wrap_words            },
    { "text_align_center",          scene_text_align_center          },
    { "text_align_right",           scene_text_align_right           },
    { "aspect_ratio",               scene_aspect_ratio               },
    { "nested_3_levels",            scene_nested_3_levels            },
    { "clip_overflow",              scene_clip_overflow              },
    { "clip_scroll_offset",         scene_clip_scroll_offset         },
    { "clip_text_wrap",             scene_clip_text_wrap             },
    { "floating_parent",            scene_floating_parent            },
    { "floating_z_sort",            scene_floating_z_sort            },
    { "floating_in_clip",           scene_floating_in_clip           },
    { "grow_7_nonint",              scene_grow_7_nonint              },
    { "exit_nested_child_with_exit",scene_exit_nested_child_with_exit},
    { "exit_nested_child_plain",    scene_exit_nested_child_plain    },
    { "exit_single_mid",            scene_exit_single_mid            },
    { "exit_single_completed",      scene_exit_single_completed      },
    { "border_between_children_odd_gap", scene_border_between_children_odd_gap },
};
static const int SCENE_COUNT = (int)(sizeof(SCENES) / sizeof(SCENES[0]));

static void run_scene(Scene s) {
    uint32_t need = Clay_MinMemorySize();
    void *mem = malloc(need);
    Clay_Arena arena = Clay_CreateArenaWithCapacityAndMemory(need, mem);
    Clay_Initialize(arena,
                    (Clay_Dimensions){ 1280, 720 },
                    (Clay_ErrorHandler){ error_handler, NULL });
    Clay_SetMeasureTextFunction(measure_text, NULL);
    Clay_RenderCommandArray cmds = s.fn();
    dump_array(cmds);
    free(mem);
}

int main(int argc, char **argv) {
    if (argc < 2) {
        fprintf(stderr, "usage: %s <scene-name>\n   or: %s --list\n", argv[0], argv[0]);
        return 2;
    }
    if (strcmp(argv[1], "--list") == 0) {
        for (int i = 0; i < SCENE_COUNT; i++) printf("%s\n", SCENES[i].name);
        return 0;
    }
    for (int i = 0; i < SCENE_COUNT; i++) {
        if (strcmp(argv[1], SCENES[i].name) == 0) {
            run_scene(SCENES[i]);
            return 0;
        }
    }
    fprintf(stderr, "unknown scene: %s\n", argv[1]);
    return 1;
}
