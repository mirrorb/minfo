import assert from "node:assert/strict";
import { test } from "node:test";
import { mergeOutputLinks, normalizeOutputLinks } from "./output.js";

test("lossless original links survive appending and normalizing image results", () => {
    const first = {
        url: "https://example.com/first.png",
        filename: "00h01m02s.png",
        isLossy: true,
        originalDownloadURL: "/api/screenshot-original?token=first",
    };
    const second = { ...first, url: "https://example.com/second.png", originalDownloadURL: "/api/screenshot-original?token=second" };
    const { items } = mergeOutputLinks([first], [second]);
    assert.deepEqual(items.map((item) => item.originalDownloadURL), [first.originalDownloadURL, second.originalDownloadURL]);
    assert.deepEqual(normalizeOutputLinks(items).map((item) => item.originalDownloadURL), [first.originalDownloadURL, second.originalDownloadURL]);
    assert.ok(items.every((item) => item.isLossy));
});
