// C reference for the opt-in floating clip-scopes extension.
// Included after native-clipping.h by 0002-clip-scopes.patch.
#ifndef CLAY__ARRAY_DEFINE
#error "clip-scopes.h must be included by the patched clay.h"
#endif

static void Clay__ClipBeginFrame(void) {
    Clay_Context *c = Clay_GetCurrentContext();
    Clay__ClipScopeArray previous = c->clipScopes;
    c->clipScopes = c->previousClipScopes;
    c->previousClipScopes = previous;
    c->clipScopes.length = 0;
    c->warnMaxClipScopesExceeded = false;
}

static void Clay__ClipCopyScopes(Clay_LayoutElement *element) {
    if (element->isTextElement) return;
    Clay_Context *c = Clay_GetCurrentContext();
    Clay_FloatingElementConfig *config = &element->config.floating;
    Clay_ClipScopeSlice supplied = config->clipScopes;
    config->clipScopes = (Clay_ClipScopeSlice){0};
    if (config->attachTo == CLAY_ATTACH_TO_NONE) return;
    int32_t count = 0;
    for (int32_t i = 0; i < supplied.length; i++) {
        if (supplied.scopes[i].horizontal || supplied.scopes[i].vertical) count++;
    }
    if (count > c->clipScopes.capacity - c->clipScopes.length) {
        element->clipScopesInvalid = true;
        if (!c->warnMaxClipScopesExceeded) {
            c->warnMaxClipScopesExceeded = true;
            c->errorHandler.errorHandlerFunction((Clay_ErrorData){
                .errorType = CLAY_ERROR_TYPE_ELEMENTS_CAPACITY_EXCEEDED,
                .errorText = CLAY_STRING("Clay ran out of clip-scope storage. Raise Clay_SetMaxElementCount and reinitialize."),
                .userData = c->errorHandler.userData,
            });
        }
        return;
    }
    if (!count) return;
    int32_t start = c->clipScopes.length;
    for (int32_t i = 0; i < supplied.length; i++) {
        Clay_ClipScope scope = supplied.scopes[i];
        if (scope.horizontal || scope.vertical) Clay__ClipScopeArray_Add(&c->clipScopes, scope);
    }
    config->clipScopes = (Clay_ClipScopeSlice){count, &c->clipScopes.internalArray[start]};
}

static bool Clay__ClipPointerAllowsRoot(Clay_LayoutElement *element) {
    Clay_Context *c = Clay_GetCurrentContext();
    if (element->clipScopesInvalid) return false;
    Clay_ClipScopeSlice scopes = element->config.floating.clipScopes;
    for (int32_t i = 0; i < scopes.length; i++) {
        Clay_ClipScope scope = scopes.scopes[i];
        Clay_LayoutElementHashMapItem *item = Clay__ClipElement(scope.elementId.id);
        if (!item || item->layoutElement->clipLayoutPass != c->clipLayoutPass ||
            !Clay__ClipContains(item->boundingBox, c->pointerInfo.position, scope.horizontal, scope.vertical)) return false;
    }
    return true;
}

static uint32_t Clay__ClipAppendScopes(Clay_LayoutElement *element, int16_t z, uint32_t count, bool generate) {
    if (!generate) return count;
    if (element->clipScopesInvalid) {
        Clay__ClipEmitReference(element, z, 0, (Clay_ClipRenderData){true, true}, count++);
        return count;
    }
    Clay_ClipScopeSlice scopes = element->config.floating.clipScopes;
    for (int32_t i = 0; i < scopes.length; i++) {
        Clay_ClipScope scope = scopes.scopes[i];
        Clay__ClipEmitReference(element, z, scope.elementId.id,
            (Clay_ClipRenderData){scope.horizontal, scope.vertical}, count++);
    }
    return count;
}


// Element IDs are unsigned; Clay__IntToString would display large IDs as negative.
static Clay_String Clay__ClipDebugId(uint32_t id) {
    Clay_Context *c = Clay_GetCurrentContext();
    if (c->dynamicStringData.capacity - c->dynamicStringData.length < 10) return CLAY_STRING("(ID unavailable)");
    char *chars = c->dynamicStringData.internalArray + c->dynamicStringData.length;
    int32_t length = 0;
    do {
        chars[length++] = (char)('0' + id % 10);
        id /= 10;
    } while (id);
    for (int32_t i = 0; i < length / 2; i++) {
        char swap = chars[i];
        chars[i] = chars[length - 1 - i];
        chars[length - 1 - i] = swap;
    }
    c->dynamicStringData.length += length;
    return (Clay_String){.length = length, .chars = chars};
}

static void Clay__ClipDebugScopes(Clay_LayoutElement *element, Clay_TextElementConfig title, Clay_TextElementConfig text) {
    Clay_ClipScopeSlice scopes = element->config.floating.clipScopes;
    if (!scopes.length && !element->clipScopesInvalid) return;
    CLAY_TEXT(CLAY_STRING("Clip Scopes"), title);
    if (element->clipScopesInvalid) {
        CLAY_TEXT(CLAY_STRING("Hidden: clip-scope capacity exceeded"), text);
        return;
    }
    for (int32_t i = 0; i < scopes.length; i++) {
        Clay_ClipScope scope = scopes.scopes[i];
        Clay_LayoutElementHashMapItem *target = Clay__ClipElement(scope.elementId.id);
        Clay_String name = scope.elementId.stringId;
        if (!name.length && target) name = target->elementId.stringId;
        CLAY_AUTO_ID({}) {
            CLAY_TEXT(CLAY_STRING("Target: "), text);
            if (name.length) {
                CLAY_TEXT(name, text);
                CLAY_TEXT(CLAY_STRING(" ("), text);
            }
            CLAY_TEXT(Clay__ClipDebugId(scope.elementId.id), text);
            if (name.length) CLAY_TEXT(CLAY_STRING(")"), text);
            if (!target) CLAY_TEXT(CLAY_STRING(" [missing]"), text);
        }
        CLAY_AUTO_ID({}) {
            CLAY_TEXT(CLAY_STRING("Horizontal: "), text);
            CLAY_TEXT(scope.horizontal ? CLAY_STRING("true") : CLAY_STRING("false"), text);
            CLAY_TEXT(CLAY_STRING(", Vertical: "), text);
            CLAY_TEXT(scope.vertical ? CLAY_STRING("true") : CLAY_STRING("false"), text);
        }
    }
}
