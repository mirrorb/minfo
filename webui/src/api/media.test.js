import assert from "node:assert/strict";
import { test } from "node:test";
import { createScreenshotJob } from "./media.js";

test("ZIP, upload and timestamp rerender requests carry the selected size mode", async (t) => {
    const requests = [];
    t.mock.method(globalThis, "fetch", async (url, options) => {
        requests.push({ url, form: options.body });
        return {
            ok: true,
            json: async () => ({ ok: true, job_id: "screenshot-job", status: "pending" }),
        };
    });

    for (const sizeMode of ["auto", "original"]) {
        await createScreenshotJob("/media/video.mkv", "png", "auto", "libplacebo", sizeMode, 4, "zip");
        await createScreenshotJob("/media/video.mkv", "png", "auto", "libplacebo", sizeMode, 4, "links", "", "pixhost.cc");
        await createScreenshotJob("/media/video.mkv", "jpg", "auto", "libplacebo", sizeMode, 1, "links", "", "pixhost.cc", ["00:01:02"]);
    }

    assert.equal(requests.length, 6);
    for (let index = 0; index < requests.length; index += 1) {
        const { url, form } = requests[index];
        assert.equal(url, "/api/screenshot-jobs");
        assert.equal(form.get("size_mode"), ["auto", "original"][Math.floor(index / 3)]);
        assert.equal(form.get("path"), "/media/video.mkv");
        assert.equal(form.get("mode"), index % 3 === 0 ? "zip" : "links");
        if (index % 3 === 2) {
            assert.equal(form.get("variant"), "jpg");
            assert.deepEqual(form.getAll("timestamp"), ["00:01:02"]);
        }
    }
});
