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

#ifndef CLAY_ORACLE_UPSTREAM
// ---------------------------------------------------------------------------
// Extension scenes: claygo child wrap (layout.wrapChildren)
// ---------------------------------------------------------------------------
// Only compiled into the patched `oracle` binary. Names carry the ext_ prefix
// so the Go parity test can tell them from the upstream corpus. Mirrored by
// extensionScenes in scenes_ext_test.go.

static Clay_Color ext_chip_color(int i) {
    static const Clay_Color palette[3] = { { 200, 80, 80, 255 }, { 80, 200, 80, 255 }, { 80, 80, 200, 255 } };
    return palette[i % 3];
}

// A fixed-size chip with the i-th palette color.
static void ext_chip(int i, float w, float h) {
    CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(w), CLAY_SIZING_FIXED(h) } }, .backgroundColor = ext_chip_color(i) });
}

// 14 chips of 118 in a 650-wide strip: inner 634 takes five per line
// (5*118 + 4*8 = 622), so lines of 5, 5 and 4. Fit height = 16 + 3*28 + 2*8.
static Clay_RenderCommandArray scene_ext_wrap_rows_basic(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(650), CLAY_SIZING_FIT(0) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 8,
            .wrapChildren = true,
            .wrapLineGap = 8,
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        for (int i = 0; i < 14; i++) ext_chip(i, 118, 28);
    }
    return Clay_EndLayout(0.0f);
}

// Two identical strips whose chips fit on one line, the first wrapping and the
// second not. Their commands must match apart from ids (the identity
// property in docs/child-wrap-spec.md).
static Clay_RenderCommandArray scene_ext_wrap_rows_single_line(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIT(0), CLAY_SIZING_FIT(0) }, .childGap = 16, .layoutDirection = CLAY_TOP_TO_BOTTOM } }) {
        for (int strip = 0; strip < 2; strip++) {
            CLAY_AUTO_ID({
                .layout = {
                    .sizing = { CLAY_SIZING_FIXED(650), CLAY_SIZING_FIT(0) },
                    .padding = CLAY_PADDING_ALL(8),
                    .childGap = 8,
                    .childAlignment = { CLAY_ALIGN_X_CENTER, CLAY_ALIGN_Y_CENTER },
                    .wrapChildren = strip == 0,
                    .wrapLineGap = 8,
                },
                .backgroundColor = { 30, 30, 36, 255 },
            }) {
                ext_chip(0, 118, 28);
                ext_chip(1, 118, 40);
                ext_chip(2, 118, 28);
            }
        }
    }
    return Clay_EndLayout(0.0f);
}

// GROW chips pack at their minimum width and then share their own line's
// slack, smallest first: [100,150] → 188,188; [120,200] → 176,200;
// [90, 80..100] → 276,100 (the capped chip stops at 100).
static Clay_RenderCommandArray scene_ext_wrap_rows_grow(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(400), CLAY_SIZING_FIT(0) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 8,
            .wrapChildren = true,
            .wrapLineGap = 8,
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_GROW(100), CLAY_SIZING_FIXED(30) } }, .backgroundColor = ext_chip_color(0) });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_GROW(150), CLAY_SIZING_FIXED(30) } }, .backgroundColor = ext_chip_color(1) });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_GROW(120), CLAY_SIZING_FIXED(30) } }, .backgroundColor = ext_chip_color(2) });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_GROW(200), CLAY_SIZING_FIXED(30) } }, .backgroundColor = ext_chip_color(0) });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_GROW(90), CLAY_SIZING_FIXED(30) } }, .backgroundColor = ext_chip_color(1) });
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_GROW(80, 100), CLAY_SIZING_FIXED(30) } }, .backgroundColor = ext_chip_color(2) });
    }
    return Clay_EndLayout(0.0f);
}

// PERCENT children resolve against upstream's basis (500 - 20 - 4*10 = 440,
// so 220 and 110) before packing: [220,100,100] then [110,200].
static Clay_RenderCommandArray scene_ext_wrap_rows_percent(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(500), CLAY_SIZING_FIT(0) },
            .padding = CLAY_PADDING_ALL(10),
            .childGap = 10,
            .wrapChildren = true,
            .wrapLineGap = 10,
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_PERCENT(0.5f), CLAY_SIZING_FIXED(40) } }, .backgroundColor = ext_chip_color(0) });
        ext_chip(1, 100, 40);
        ext_chip(2, 100, 40);
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_PERCENT(0.25f), CLAY_SIZING_FIXED(40) } }, .backgroundColor = ext_chip_color(0) });
        ext_chip(1, 200, 40);
    }
    return Clay_EndLayout(0.0f);
}

// Six chips of mixed sizes in a 300x200 box pack as [90,120,60], [100,80],
// [110] with natural extents 30, 40, 28; the 74 px of vertical slack goes to
// the lines smallest first, then each child aligns inside its line.
static Clay_RenderCommandArray scene_ext_wrap_rows_aligned(Clay_ChildAlignment alignment) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(300), CLAY_SIZING_FIXED(200) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 6,
            .childAlignment = alignment,
            .wrapChildren = true,
            .wrapLineGap = 6,
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        ext_chip(0, 90, 20);
        ext_chip(1, 120, 30);
        ext_chip(2, 60, 24);
        ext_chip(3, 100, 20);
        ext_chip(4, 80, 40);
        ext_chip(5, 110, 28);
    }
    return Clay_EndLayout(0.0f);
}

static Clay_RenderCommandArray scene_ext_wrap_rows_align_center(void) {
    return scene_ext_wrap_rows_aligned((Clay_ChildAlignment){ CLAY_ALIGN_X_CENTER, CLAY_ALIGN_Y_CENTER });
}

static Clay_RenderCommandArray scene_ext_wrap_rows_align_right_bottom(void) {
    return scene_ext_wrap_rows_aligned((Clay_ChildAlignment){ CLAY_ALIGN_X_RIGHT, CLAY_ALIGN_Y_BOTTOM });
}

// Odd gap (9 → halfGap 4) with between-children dividers: within a line the
// divider spans that line's band, between lines it spans the parent's width.
// 7 chips of 80 in inner 284 pack three per line.
static Clay_RenderCommandArray scene_ext_wrap_rows_gap_borders(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(300), CLAY_SIZING_FIT(0) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 9,
            .wrapChildren = true,
            .wrapLineGap = 9,
        },
        .backgroundColor = { 30, 30, 36, 255 },
        .border = {
            .color = { 240, 200, 80, 255 },
            .width = { .betweenChildren = 2 },
        },
    }) {
        for (int i = 0; i < 7; i++) ext_chip(i, 80, 30);
    }
    return Clay_EndLayout(0.0f);
}

// A child that alone exceeds the inner width gets its own line: the FIT text
// box shrinks to the inner width and its text wraps; the FIXED chip cannot
// shrink and overflows.
static Clay_RenderCommandArray scene_ext_wrap_rows_lone_wide(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(200), CLAY_SIZING_FIT(0) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 8,
            .wrapChildren = true,
            .wrapLineGap = 8,
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        ext_chip(0, 60, 30);
        CLAY_AUTO_ID({
            .layout = { .sizing = { CLAY_SIZING_FIT(0), CLAY_SIZING_FIT(0) }, .padding = CLAY_PADDING_ALL(4) },
            .backgroundColor = { 80, 80, 120, 255 },
        }) {
            CLAY_TEXT(CLAY_STRING("The quick brown fox jumps over"), CLAY_TEXT_CONFIG({
                .textColor = { 240, 240, 240, 255 },
                .fontSize = 16,
                .wrapMode = CLAY_TEXT_WRAP_WORDS,
            }));
        }
        ext_chip(2, 300, 30);
    }
    return Clay_EndLayout(0.0f);
}

// Chips sized by their labels (7 px per char at font 14 plus 12 padding)
// packing into a 320-wide strip, vertically centered within their lines.
static Clay_RenderCommandArray scene_ext_wrap_rows_text_children(void) {
    static const char *labels[8] = { "Workspaces", "Layout", "Window", "Status", "Alerts", "System", "Clock", "Battery" };
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(320), CLAY_SIZING_FIT(0) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 8,
            .childAlignment = { CLAY_ALIGN_X_LEFT, CLAY_ALIGN_Y_CENTER },
            .wrapChildren = true,
            .wrapLineGap = 8,
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        for (int i = 0; i < 8; i++) {
            CLAY_AUTO_ID({
                .layout = { .sizing = { CLAY_SIZING_FIT(0), CLAY_SIZING_FIT(0) }, .padding = CLAY_PADDING_ALL(6) },
                .backgroundColor = ext_chip_color(i),
            }) {
                Clay_String label = { .length = (int32_t)strlen(labels[i]), .chars = labels[i] };
                CLAY_TEXT(label, CLAY_TEXT_CONFIG({
                    .textColor = { 240, 240, 240, 255 },
                    .fontSize = 14,
                    .wrapMode = CLAY_TEXT_WRAP_WORDS,
                }));
            }
        }
    }
    return Clay_EndLayout(0.0f);
}

// A FIT-width strip next to a 900-wide sidebar in a GROW row: the row shrinks
// the strip toward its wrap minimum (padding + widest chip), so 8 chips of 100
// that would take 854 px on one line wrap inside the 356 px left over.
static Clay_RenderCommandArray scene_ext_wrap_rows_fit_in_grow(void) {
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
        CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(900), CLAY_SIZING_FIXED(200) } }, .backgroundColor = { 60, 60, 80, 255 } });
        CLAY_AUTO_ID({
            .layout = {
                .sizing = { CLAY_SIZING_FIT(0), CLAY_SIZING_FIT(0) },
                .padding = CLAY_PADDING_ALL(6),
                .childGap = 6,
                .wrapChildren = true,
                .wrapLineGap = 6,
            },
            .backgroundColor = { 80, 80, 120, 255 },
        }) {
            for (int i = 0; i < 8; i++) ext_chip(i, 100, 30);
        }
    }
    return Clay_EndLayout(0.0f);
}

// A clipping wrap parent shorter than its three lines: lines keep their
// natural height (children are not squashed) and scroll by the child offset.
static Clay_RenderCommandArray scene_ext_wrap_rows_clip_scroll(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(300), CLAY_SIZING_FIXED(80) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 8,
            .wrapChildren = true,
            .wrapLineGap = 8,
        },
        .backgroundColor = { 30, 30, 36, 255 },
        .clip = { .horizontal = true, .vertical = true, .childOffset = { 0, -30 } },
    }) {
        for (int i = 0; i < 9; i++) ext_chip(i, 80, 30);
    }
    return Clay_EndLayout(0.0f);
}

// A wrapping strip inside a wrapping strip. The inner FIT strip (436 px on one
// line) gets its own outer line, shrinks to the outer inner width and wraps
// its own chips 7 + 1.
static Clay_RenderCommandArray scene_ext_wrap_rows_nested(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(400), CLAY_SIZING_FIT(0) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 8,
            .wrapChildren = true,
            .wrapLineGap = 8,
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        ext_chip(0, 120, 30);
        ext_chip(1, 120, 30);
        CLAY_AUTO_ID({
            .layout = {
                .sizing = { CLAY_SIZING_FIT(0), CLAY_SIZING_FIT(0) },
                .padding = CLAY_PADDING_ALL(4),
                .childGap = 4,
                .wrapChildren = true,
                .wrapLineGap = 4,
            },
            .backgroundColor = { 80, 80, 120, 255 },
        }) {
            for (int i = 0; i < 8; i++) ext_chip(i, 50, 20);
        }
        ext_chip(2, 120, 30);
        ext_chip(0, 120, 30);
    }
    return Clay_EndLayout(0.0f);
}

// Five chips on two lines; chip B has an exit transition. Frames 2 and 3 omit
// it, so its clone sits at B's slot while packing skips it: A, C and D now
// share line one and E alone is line two. Frame 3 is dumped, with the clone
// sliding at x = 96 - 0.1 * 596.
static void ext_declare_wrap_exit(bool withB) {
    Clay_TransitionElementConfig transition = {
        .handler = linear_x_interpolator,
        .duration = 1.0f,
        .properties = CLAY_TRANSITION_PROPERTY_X,
        .exit = { .setFinalState = exit_slide_off },
    };
    Clay_BeginLayout();
    CLAY(CLAY_ID("WrapExitStrip"), {
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(300), CLAY_SIZING_FIT(0) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 8,
            .wrapChildren = true,
            .wrapLineGap = 8,
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        CLAY(CLAY_ID("WrapExitA"), { .layout = { .sizing = { CLAY_SIZING_FIXED(80), CLAY_SIZING_FIXED(30) } }, .backgroundColor = ext_chip_color(0) });
        if (withB) {
            CLAY(CLAY_ID("WrapExitB"), { .layout = { .sizing = { CLAY_SIZING_FIXED(80), CLAY_SIZING_FIXED(30) } }, .backgroundColor = ext_chip_color(1), .transition = transition });
        }
        CLAY(CLAY_ID("WrapExitC"), { .layout = { .sizing = { CLAY_SIZING_FIXED(80), CLAY_SIZING_FIXED(30) } }, .backgroundColor = ext_chip_color(2) });
        CLAY(CLAY_ID("WrapExitD"), { .layout = { .sizing = { CLAY_SIZING_FIXED(80), CLAY_SIZING_FIXED(30) } }, .backgroundColor = ext_chip_color(0) });
        CLAY(CLAY_ID("WrapExitE"), { .layout = { .sizing = { CLAY_SIZING_FIXED(80), CLAY_SIZING_FIXED(30) } }, .backgroundColor = ext_chip_color(1) });
    }
}

static Clay_RenderCommandArray scene_ext_wrap_exit_transition(void) {
    ext_declare_wrap_exit(true);
    Clay_EndLayout(ORACLE_TRANSITION_DELTA);
    ext_declare_wrap_exit(false);
    Clay_EndLayout(ORACLE_TRANSITION_DELTA);
    ext_declare_wrap_exit(false);
    return Clay_EndLayout(ORACLE_TRANSITION_DELTA);
}

// Column wrap: nine fixed boxes stack top to bottom inside 184 px of inner
// height and break into columns of [40,60,50], [30,70,40], [40,60,20]; the
// FIT width becomes the stacked column widths (120 + 110 + 100 plus gaps).
static Clay_RenderCommandArray scene_ext_wrap_cols_fixed_height(void) {
    static const float sizes[9][2] = { {100, 40}, {80, 60}, {120, 50}, {60, 30}, {90, 70}, {110, 40}, {70, 40}, {100, 60}, {80, 20} };
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIT(0), CLAY_SIZING_FIXED(200) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 8,
            .layoutDirection = CLAY_TOP_TO_BOTTOM,
            .wrapChildren = true,
            .wrapLineGap = 8,
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        for (int i = 0; i < 9; i++) ext_chip(i, sizes[i][0], sizes[i][1]);
    }
    return Clay_EndLayout(0.0f);
}

// Column wrap with GROW in both directions, which needs the second sizing
// sweep: the text boxes first take the whole 384 px inner width, the y pass
// packs two columns and grows the GROW-height boxes inside each, then the
// second x pass shrinks the two 384-wide columns to 188 each and the text
// boxes follow their column.
static Clay_RenderCommandArray scene_ext_wrap_cols_grow_height(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(400), CLAY_SIZING_FIXED(160) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 8,
            .layoutDirection = CLAY_TOP_TO_BOTTOM,
            .wrapChildren = true,
            .wrapLineGap = 8,
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        for (int i = 0; i < 6; i++) {
            if (i % 2 == 0) {
                CLAY_AUTO_ID({
                    .layout = { .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_FIT(0) }, .padding = CLAY_PADDING_ALL(4) },
                    .backgroundColor = ext_chip_color(i),
                }) {
                    CLAY_TEXT(CLAY_STRING("Quick brown fox"), CLAY_TEXT_CONFIG({
                        .textColor = { 240, 240, 240, 255 },
                        .fontSize = 16,
                        .wrapMode = CLAY_TEXT_WRAP_WORDS,
                    }));
                }
            } else {
                CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(60), CLAY_SIZING_GROW(20) } }, .backgroundColor = ext_chip_color(i) });
            }
        }
    }
    return Clay_EndLayout(0.0f);
}
// Clipping wrap parents keep upstream's padding-only minimum on the clipped
// axis. Top: a scrolling wrap pane (three lines, 122 tall) inside a 120-tall
// column shrinks to 66 so the fixed footer fits. Bottom: a column-wrap pane
// whose four columns stack to 280 sits in a 200-wide row next to a fixed box;
// the second sizing sweep shrinks it to 116 and the re-pack must not grow it
// back.
static Clay_RenderCommandArray scene_ext_wrap_clip_shrink(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIT(0), CLAY_SIZING_FIT(0) }, .childGap = 8, .layoutDirection = CLAY_TOP_TO_BOTTOM } }) {
        CLAY_AUTO_ID({
            .layout = {
                .sizing = { CLAY_SIZING_FIXED(300), CLAY_SIZING_FIXED(120) },
                .padding = CLAY_PADDING_ALL(8),
                .childGap = 8,
                .layoutDirection = CLAY_TOP_TO_BOTTOM,
            },
            .backgroundColor = { 30, 30, 36, 255 },
        }) {
            CLAY_AUTO_ID({
                .layout = {
                    .sizing = { CLAY_SIZING_GROW(0), CLAY_SIZING_FIT(0) },
                    .padding = CLAY_PADDING_ALL(8),
                    .childGap = 8,
                    .wrapChildren = true,
                    .wrapLineGap = 8,
                },
                .backgroundColor = { 80, 80, 120, 255 },
                .clip = { .horizontal = true, .vertical = true },
            }) {
                for (int i = 0; i < 9; i++) ext_chip(i, 80, 30);
            }
            CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(100), CLAY_SIZING_FIXED(30) } }, .backgroundColor = { 60, 60, 80, 255 } });
        }
        CLAY_AUTO_ID({
            .layout = {
                .sizing = { CLAY_SIZING_FIXED(200), CLAY_SIZING_FIXED(150) },
                .padding = CLAY_PADDING_ALL(8),
                .childGap = 8,
                .layoutDirection = CLAY_LEFT_TO_RIGHT,
            },
            .backgroundColor = { 30, 30, 36, 255 },
        }) {
            CLAY_AUTO_ID({
                .layout = {
                    .sizing = { CLAY_SIZING_FIT(0), CLAY_SIZING_GROW(0) },
                    .padding = CLAY_PADDING_ALL(8),
                    .childGap = 8,
                    .layoutDirection = CLAY_TOP_TO_BOTTOM,
                    .wrapChildren = true,
                    .wrapLineGap = 8,
                },
                .backgroundColor = { 80, 80, 120, 255 },
                .clip = { .horizontal = true, .vertical = true },
            }) {
                for (int i = 0; i < 8; i++) ext_chip(i, 60, 40);
            }
            CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIXED(60), CLAY_SIZING_FIXED(100) } }, .backgroundColor = { 60, 60, 80, 255 } });
        }
    }
    return Clay_EndLayout(0.0f);
}
// A parent shorter than its stacked lines: 120x40 with gap 10 holds three
// lines of 50x30 FIXED chips (natural 30 each, 110 stacked), so the line
// extents are squashed to 0 but the chips cannot follow. Each line must start
// below the previous line's chips (y = 40, 80), overflowing the parent, not
// over them, and the dividers of the lines pushed past the parent's edge must
// end at their own line's end rather than at the parent's (never negative).
static Clay_RenderCommandArray scene_ext_wrap_rows_short_parent(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(120), CLAY_SIZING_FIXED(40) },
            .childGap = 10,
            .wrapChildren = true,
            .wrapLineGap = 10,
        },
        .backgroundColor = { 30, 30, 36, 255 },
        .border = {
            .color = { 240, 200, 80, 255 },
            .width = { .betweenChildren = 2 },
        },
    }) {
        for (int i = 0; i < 6; i++) ext_chip(i, 50, 30);
    }
    return Clay_EndLayout(0.0f);
}

// Two column-wrap panes with GROW-width cells, the second one clipping.
// Clipping must not change GROW sizing: in both, four 30-tall cells break
// into two columns of two, and each cell takes its column's 188 px, because
// the first sweep leaves column children at their content width instead of
// growing them to the pane and mistaking that for the column's content.
static Clay_RenderCommandArray scene_ext_wrap_cols_clip_grow(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIT(0), CLAY_SIZING_FIT(0) }, .childGap = 8, .layoutDirection = CLAY_TOP_TO_BOTTOM } }) {
        for (int pane = 0; pane < 2; pane++) {
            CLAY_AUTO_ID({
                .layout = {
                    .sizing = { CLAY_SIZING_FIXED(400), CLAY_SIZING_FIXED(100) },
                    .padding = CLAY_PADDING_ALL(8),
                    .childGap = 8,
                    .layoutDirection = CLAY_TOP_TO_BOTTOM,
                    .wrapChildren = true,
                    .wrapLineGap = 8,
                },
                .backgroundColor = { 30, 30, 36, 255 },
                .clip = { .horizontal = pane == 1, .vertical = pane == 1 },
            }) {
                for (int i = 0; i < 4; i++) {
                    CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_GROW(60), CLAY_SIZING_FIXED(30) } }, .backgroundColor = ext_chip_color(i) });
                }
            }
        }
    }
    return Clay_EndLayout(0.0f);
}
// wrapLineGap is independent of childGap. Seven 85 px chips in a 300-wide
// strip (inner 284) take three per line (3*85 + 2*8 = 271), so lines of 3, 3
// and 1. The first strip stacks them 24 apart, the second 0 apart, which
// childGap alone could never express: fit heights 16 + 3*30 + 2*24 = 154 and
// 16 + 3*30 = 106.
static Clay_RenderCommandArray scene_ext_wrap_rows_line_gap(void) {
    static const uint16_t lineGaps[2] = { 24, 0 };
    Clay_BeginLayout();
    CLAY_AUTO_ID({ .layout = { .sizing = { CLAY_SIZING_FIT(0), CLAY_SIZING_FIT(0) }, .childGap = 16, .layoutDirection = CLAY_TOP_TO_BOTTOM } }) {
        for (int strip = 0; strip < 2; strip++) {
            CLAY_AUTO_ID({
                .layout = {
                    .sizing = { CLAY_SIZING_FIXED(300), CLAY_SIZING_FIT(0) },
                    .padding = CLAY_PADDING_ALL(8),
                    .childGap = 8,
                    .wrapChildren = true,
                    .wrapLineGap = lineGaps[strip],
                },
                .backgroundColor = { 30, 30, 36, 255 },
            }) {
                for (int i = 0; i < 7; i++) ext_chip(i, 85, 30);
            }
        }
    }
    return Clay_EndLayout(0.0f);
}

// Cross-axis slack with a line gap that is not the child gap. Six chips pack
// as [90,120], [60,100,80], [110] in inner 284; natural extents 30, 40 and 28
// stack to 98 + 2*20 of line gap = 138, so 204 - 138 = 66 px of slack goes to
// the lines smallest first before each child aligns inside its line.
static Clay_RenderCommandArray scene_ext_wrap_rows_line_gap_slack(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(300), CLAY_SIZING_FIXED(220) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 8,
            .childAlignment = { CLAY_ALIGN_X_CENTER, CLAY_ALIGN_Y_CENTER },
            .wrapChildren = true,
            .wrapLineGap = 20,
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        ext_chip(0, 90, 20);
        ext_chip(1, 120, 30);
        ext_chip(2, 60, 24);
        ext_chip(3, 100, 20);
        ext_chip(4, 80, 40);
        ext_chip(5, 110, 28);
    }
    return Clay_EndLayout(0.0f);
}

// Both gaps odd and different, so the two halvings cannot be confused for one
// another: childGap 7 centers within-line dividers at half 3, wrapLineGap 13
// centers between-line dividers and the band edges at half 6. Seven 80 px
// chips in inner 284 pack three per line; fit height 16 + 3*30 + 2*13 = 132.
static Clay_RenderCommandArray scene_ext_wrap_rows_line_gap_borders(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(300), CLAY_SIZING_FIT(0) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 7,
            .wrapChildren = true,
            .wrapLineGap = 13,
        },
        .backgroundColor = { 30, 30, 36, 255 },
        .border = {
            .color = { 240, 200, 80, 255 },
            .width = { .betweenChildren = 2 },
        },
    }) {
        for (int i = 0; i < 7; i++) ext_chip(i, 80, 30);
    }
    return Clay_EndLayout(0.0f);
}

// Column wrap with the axes swapped: childGap 6 separates cells down a
// column, wrapLineGap 20 separates the columns across. Seven cells 50 tall in
// inner 184 take three per column (3*50 + 2*6 = 162), so columns of 3, 3 and
// 1, and the 304 px of inner width leaves slack for the columns to share.
static Clay_RenderCommandArray scene_ext_wrap_cols_line_gap(void) {
    static const float widths[7] = { 60, 70, 50, 80, 55, 65, 45 };
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(320), CLAY_SIZING_FIXED(200) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 6,
            .layoutDirection = CLAY_TOP_TO_BOTTOM,
            .wrapChildren = true,
            .wrapLineGap = 20,
        },
        .backgroundColor = { 30, 30, 36, 255 },
    }) {
        for (int i = 0; i < 7; i++) ext_chip(i, widths[i], 50);
    }
    return Clay_EndLayout(0.0f);
}

// A clipping wrap pane whose scroll content must stack on the line gap, not
// the child gap: nine 80 px chips in inner 284 make three lines of 30, so the
// content is 3*80 + 2*8 + 16 wide and 3*30 + 2*22 + 16 = 150 tall, and the
// third line sits at 8 + 2*(30 + 22).
static Clay_RenderCommandArray scene_ext_wrap_rows_line_gap_scroll(void) {
    Clay_BeginLayout();
    CLAY_AUTO_ID({
        .layout = {
            .sizing = { CLAY_SIZING_FIXED(300), CLAY_SIZING_FIXED(80) },
            .padding = CLAY_PADDING_ALL(8),
            .childGap = 8,
            .wrapChildren = true,
            .wrapLineGap = 22,
        },
        .backgroundColor = { 30, 30, 36, 255 },
        .clip = { .horizontal = true, .vertical = true, .childOffset = { 0, -30 } },
    }) {
        for (int i = 0; i < 9; i++) ext_chip(i, 80, 30);
    }
    return Clay_EndLayout(0.0f);
}

#endif // CLAY_ORACLE_UPSTREAM

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
#ifndef CLAY_ORACLE_UPSTREAM
    { "ext_wrap_rows_basic",              scene_ext_wrap_rows_basic              },
    { "ext_wrap_rows_single_line",        scene_ext_wrap_rows_single_line        },
    { "ext_wrap_rows_grow",               scene_ext_wrap_rows_grow               },
    { "ext_wrap_rows_percent",            scene_ext_wrap_rows_percent            },
    { "ext_wrap_rows_align_center",       scene_ext_wrap_rows_align_center       },
    { "ext_wrap_rows_align_right_bottom", scene_ext_wrap_rows_align_right_bottom },
    { "ext_wrap_rows_gap_borders",        scene_ext_wrap_rows_gap_borders        },
    { "ext_wrap_rows_lone_wide",          scene_ext_wrap_rows_lone_wide          },
    { "ext_wrap_rows_text_children",      scene_ext_wrap_rows_text_children      },
    { "ext_wrap_rows_fit_in_grow",        scene_ext_wrap_rows_fit_in_grow        },
    { "ext_wrap_rows_clip_scroll",        scene_ext_wrap_rows_clip_scroll        },
    { "ext_wrap_rows_nested",             scene_ext_wrap_rows_nested             },
    { "ext_wrap_exit_transition",         scene_ext_wrap_exit_transition         },
    { "ext_wrap_cols_fixed_height",       scene_ext_wrap_cols_fixed_height       },
    { "ext_wrap_cols_grow_height",        scene_ext_wrap_cols_grow_height        },
    { "ext_wrap_clip_shrink",             scene_ext_wrap_clip_shrink             },
    { "ext_wrap_rows_short_parent",       scene_ext_wrap_rows_short_parent       },
    { "ext_wrap_cols_clip_grow",          scene_ext_wrap_cols_clip_grow          },
    { "ext_wrap_rows_line_gap",           scene_ext_wrap_rows_line_gap           },
    { "ext_wrap_rows_line_gap_slack",     scene_ext_wrap_rows_line_gap_slack     },
    { "ext_wrap_rows_line_gap_borders",   scene_ext_wrap_rows_line_gap_borders   },
    { "ext_wrap_cols_line_gap",           scene_ext_wrap_cols_line_gap           },
    { "ext_wrap_rows_line_gap_scroll",    scene_ext_wrap_rows_line_gap_scroll    },
#endif
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
