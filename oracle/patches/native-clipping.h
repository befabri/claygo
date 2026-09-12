// Native clipping corrections, independent of optional extensions.
// Included by 0000-native-clipping.patch. See UPSTREAM.md, Native corrections.
#ifndef CLAY__ARRAY_DEFINE
#error "native-clipping.h must be included by the patched clay.h"
#endif

void Clay__AddRenderCommand(Clay_RenderCommand command);

static Clay_LayoutElementHashMapItem *Clay__ClipElement(uint32_t id) {
    if (!id) return NULL;
    Clay_Context *c = Clay_GetCurrentContext();
    Clay_LayoutElementHashMapItem *item = Clay__GetHashMapItem(id);
    if (item == &Clay_LayoutElementHashMapItem_DEFAULT || item->generation <= c->generation ||
        !item->layoutElement || item->layoutElement->id != id) return NULL;
    return item;
}

static bool Clay__ClipContains(Clay_BoundingBox box, Clay_Vector2 point, bool horizontal, bool vertical) {
    return (!horizontal || (point.x >= box.x && point.x < box.x + box.width)) &&
           (!vertical || (point.y >= box.y && point.y < box.y + box.height));
}

static bool Clay__ClipPointerWithinNative(uint32_t id) {
    Clay_Context *c = Clay_GetCurrentContext();
    for (int32_t remaining = c->layoutElements.capacity; id; remaining--) {
        if (!remaining) return false;
        Clay_LayoutElementHashMapItem *item = Clay__ClipElement(id);
        if (!item || item->layoutElement->clipLayoutPass != c->clipLayoutPass) return false;
        Clay_ClipElementConfig config = item->layoutElement->config.clip;
        if (!Clay__ClipContains(item->boundingBox, c->pointerInfo.position, config.horizontal, config.vertical)) return false;
        id = item->layoutElement->clipAncestorId;
    }
    return true;
}

static void Clay__ClipBeginLayout(void) {
    Clay_Context *c = Clay_GetCurrentContext();
    c->clipLayoutPass++;
    c->clipCommands.length = 0;
}

static void Clay__ClipResolveCommands(void) {
    Clay_Context *c = Clay_GetCurrentContext();
    for (int32_t i = 0; i < c->clipCommands.length; i++) {
        Clay__ClipCommand ref = c->clipCommands.internalArray[i];
        Clay_BoundingBox box = {0};
        Clay_LayoutElementHashMapItem *item = Clay__ClipElement(ref.elementId);
        if (item && item->layoutElement->clipLayoutPass == c->clipLayoutPass) box = item->boundingBox;
        c->renderCommands.internalArray[ref.index].boundingBox = box;
    }
}

static void Clay__ClipEmitReference(Clay_LayoutElement *root, int16_t z, uint32_t elementId, Clay_ClipRenderData axes, uint32_t ordinal) {
    Clay_Context *c = Clay_GetCurrentContext();
    if (axes.horizontal && axes.vertical) axes = (Clay_ClipRenderData){0};
    int32_t index = c->renderCommands.length;
    Clay__AddRenderCommand((Clay_RenderCommand){
        .renderData = {.clip = axes},
        .id = Clay__HashNumber(root->id, root->children.length + 10 + 2 * ordinal).id,
        .zIndex = z, .commandType = CLAY_RENDER_COMMAND_TYPE_SCISSOR_START,
    });
    if (c->renderCommands.length > index) {
        Clay__ClipCommandArray_Add(&c->clipCommands, (Clay__ClipCommand){elementId, index});
    }
}

static uint32_t Clay__ClipBeginNativeRoot(Clay_LayoutElement *element, Clay__LayoutElementTreeRoot *root,
                                  Clay_Vector2 *position, bool generate) {
    Clay_Context *c = Clay_GetCurrentContext();
    if (c->externalScrollHandlingEnabled) {
        Clay_LayoutElementHashMapItem *item = Clay__ClipElement(root->clipElementId);
        if (item) {
            Clay_ClipElementConfig config = item->layoutElement->config.clip;
            if (config.horizontal) position->x += config.childOffset.x;
            if (config.vertical) position->y += config.childOffset.y;
        }
    }
    if (!generate) return 0;
    uint32_t count = 0, id = root->clipElementId;
    for (int32_t remaining = c->layoutElements.capacity; id; remaining--) {
        Clay_LayoutElementHashMapItem *item = Clay__ClipElement(id);
        if (!item || !remaining) {
            Clay__ClipEmitReference(element, root->zIndex, 0, (Clay_ClipRenderData){true, true}, count++);
            break;
        }
        Clay_ClipElementConfig config = item->layoutElement->config.clip;
        // An exit snapshot may retain a now-disabled clip. Keep walking so
        // active outer ancestors still constrain the tree.
        if (config.horizontal || config.vertical) {
            Clay__ClipEmitReference(element, root->zIndex, id,
                (Clay_ClipRenderData){config.horizontal, config.vertical}, count++);
        }
        id = item->layoutElement->clipAncestorId;
    }
    return count;
}

static void Clay__ClipEndRoot(Clay_LayoutElement *root, uint32_t count) {
    while (count) {
        count--;
        Clay__AddRenderCommand((Clay_RenderCommand){
            .id = Clay__HashNumber(root->id, root->children.length + 11 + 2 * count).id,
            .commandType = CLAY_RENDER_COMMAND_TYPE_SCISSOR_END,
        });
    }
}

static void Clay__ClipBeginOwned(Clay_LayoutElement *element, Clay_BoundingBox box, int16_t z) {
    Clay_ClipElementConfig config = element->config.clip;
    if (!config.horizontal && !config.vertical) return;
    Clay__AddRenderCommand((Clay_RenderCommand){
        .boundingBox = box,
        .renderData = {.clip = {.horizontal = config.horizontal, .vertical = config.vertical}},
        .userData = element->config.userData, .id = element->id, .zIndex = z,
        .commandType = CLAY_RENDER_COMMAND_TYPE_SCISSOR_START,
    });
}

static void Clay__ClipEndOwned(Clay_LayoutElement *element, int32_t rootChildCount) {
    if (element->config.clip.horizontal || element->config.clip.vertical) {
        Clay__AddRenderCommand((Clay_RenderCommand){
            .id = Clay__HashNumber(element->id, rootChildCount + 11).id,
            .commandType = CLAY_RENDER_COMMAND_TYPE_SCISSOR_END,
        });
    }
}

// Emit pending offscreen owners only when a descendant reaches the screen.
static void Clay__ClipFlushPendingOwned(Clay__LayoutElementTreeNodeArray *dfs, int16_t z) {
    for (int32_t i = 0; i < dfs->length - 1; i++) {
        Clay__LayoutElementTreeNode *node = &dfs->internalArray[i];
        if (!node->clipPending) continue;
        node->clipPending = false;
        Clay_LayoutElementHashMapItem *item = Clay__ClipElement(node->layoutElement->id);
        Clay__ClipBeginOwned(node->layoutElement, item ? item->boundingBox : (Clay_BoundingBox){0}, z);
    }
}
