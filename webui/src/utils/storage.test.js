import assert from "node:assert/strict";
import { afterEach, beforeEach, test } from "node:test";
import { loadAppState, saveAppState } from "./storage.js";

const previousWindow = globalThis.window;
let values;

beforeEach(() => {
    values = new Map();
    globalThis.window = {
        localStorage: {
            getItem: (key) => values.get(key) ?? null,
            setItem: (key, value) => values.set(key, value),
            removeItem: (key) => values.delete(key),
        },
    };
});

afterEach(() => {
    if (previousWindow === undefined) {
        delete globalThis.window;
    } else {
        globalThis.window = previousWindow;
    }
});

test("new and existing preferences default to automatic screenshot dimensions", () => {
    assert.equal(loadAppState().screenshotSizeMode, "auto");
    values.set("minfo:webui:state:v1", JSON.stringify({ screenshotVariant: "jpg" }));
    const state = loadAppState();
    assert.equal(state.screenshotSizeMode, "auto");
    assert.equal(state.screenshotVariant, "jpg");
});

test("all screenshot size modes survive saving and reloading", () => {
    for (const screenshotSizeMode of ["auto", "original"]) {
        saveAppState({ ...loadAppState(), screenshotSizeMode });
        assert.equal(loadAppState().screenshotSizeMode, screenshotSizeMode);
    }
});

test("invalid saved screenshot size modes fall back to automatic", () => {
    for (const screenshotSizeMode of ["invalid", "display"]) {
        values.set("minfo:webui:state:v1", JSON.stringify({ screenshotSizeMode }));
        assert.equal(loadAppState().screenshotSizeMode, "auto");
    }
});

test("HDR conversion can remain disabled after saving and reloading", () => {
    assert.equal(loadAppState().screenshotHDRProcessor, "libplacebo");
    for (const screenshotHDRProcessor of ["off", "zscale", "libplacebo"]) {
        saveAppState({ ...loadAppState(), screenshotHDRProcessor });
        assert.equal(loadAppState().screenshotHDRProcessor, screenshotHDRProcessor);
    }
    values.set("minfo:webui:state:v1", JSON.stringify({ screenshotHDRProcessor: "invalid" }));
    assert.equal(loadAppState().screenshotHDRProcessor, "libplacebo");
});
