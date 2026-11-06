import assert from "node:assert/strict";
import { test } from "node:test";
import { effectScope, ref } from "vue";
import { useOriginalPNGCleanup } from "./useOriginalPNGCleanup.js";

const url = (letter) => `/api/screenshot-original?token=${letter.repeat(32)}`;

test("removed entries are cleaned while appended and shared originals remain available", (t) => {
    const requests = [];
    t.mock.method(globalThis, "fetch", async (url, options) => {
        requests.push({ url, ...options });
        return { ok: true };
    });
    const items = ref([{ originalDownloadURL: url("a") }]);
    const scope = effectScope();
    t.after(() => scope.stop());
    scope.run(() => useOriginalPNGCleanup(items));
    items.value.push({ originalDownloadURL: url("b") });
    assert.equal(requests.length, 0, "appending must retain existing originals");
    items.value = [{ originalDownloadURL: url("b") }, { originalDownloadURL: url("b") }];
    assert.deepEqual(requests.map((item) => item.url), [url("a")]);
    items.value.pop();
    assert.equal(requests.length, 1, "removing a duplicate must retain the shared original");
    scope.stop();
    assert.deepEqual(requests.map((item) => item.url), [url("a"), url("b")]);
    assert.ok(requests.every((item) => item.method === "DELETE" && item.keepalive === true));
});

test("clearing results cleans the old batch and hiding the window cleans the remaining batch", (t) => {
    const deleted = [];
    t.mock.method(globalThis, "fetch", async (url) => {
        deleted.push(url);
        return { ok: true };
    });
    const items = ref([{ originalDownloadURL: url("a") }, { originalDownloadURL: url("b") }]);
    const scope = effectScope();
    t.after(() => scope.stop());
    scope.run(() => useOriginalPNGCleanup(items));
    items.value = [];
    assert.deepEqual(deleted, [url("a"), url("b")]);
    items.value = [{ originalDownloadURL: url("c") }];
    scope.stop();
    assert.deepEqual(deleted, [url("a"), url("b"), url("c")]);
});

test("page exit releases originals once and removes its listener when the window unmounts", (t) => {
    const previousWindow = globalThis.window;
    globalThis.window = new EventTarget();
    t.after(() => {
        if (previousWindow === undefined) delete globalThis.window;
        else globalThis.window = previousWindow;
    });
    const deleted = [];
    t.mock.method(globalThis, "fetch", async (url) => {
        deleted.push(url);
        return { ok: true };
    });
    const scope = effectScope();
    t.after(() => scope.stop());
    scope.run(() => useOriginalPNGCleanup(ref([{ originalDownloadURL: url("a") }])));
    globalThis.window.dispatchEvent(new Event("pagehide"));
    scope.stop();
    globalThis.window.dispatchEvent(new Event("pagehide"));
    assert.deepEqual(deleted, [url("a")]);
});
