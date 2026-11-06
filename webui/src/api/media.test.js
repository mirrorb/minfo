import assert from "node:assert/strict";
import { test } from "node:test";
import { createScreenshotJob, deleteOriginalPNG, downloadOriginalPNG, fetchScreenshotJob } from "./media.js";

test("ZIP, upload and timestamp rerender requests carry the selected render options", async (t) => {
    const requests = [];
    t.mock.method(globalThis, "fetch", async (url, options) => {
        requests.push({ url, form: options.body });
        return {
            ok: true,
            json: async () => ({ ok: true, job_id: "screenshot-job", status: "pending" }),
        };
    });

    for (const sizeMode of ["auto", "original"]) {
        for (const hdrProcessor of ["libplacebo", "zscale", "off"]) {
            await createScreenshotJob("/media/video.mkv", "png", "auto", hdrProcessor, sizeMode, 4, "zip");
            await createScreenshotJob("/media/video.mkv", "png", "auto", hdrProcessor, sizeMode, 4, "links", "", "pixhost.cc");
            await createScreenshotJob("/media/video.mkv", "jpg", "auto", hdrProcessor, sizeMode, 1, "links", "", "pixhost.cc", ["00:01:02"]);
        }
    }

    assert.equal(requests.length, 18);
    for (let index = 0; index < requests.length; index += 1) {
        const { url, form } = requests[index];
        assert.equal(url, "/api/screenshot-jobs");
        assert.equal(form.get("size_mode"), ["auto", "original"][Math.floor(index / 9)]);
        assert.equal(form.get("hdr_processor"), ["libplacebo", "zscale", "off"][Math.floor(index / 3) % 3]);
        assert.equal(form.get("path"), "/media/video.mkv");
        assert.equal(form.get("mode"), index % 3 === 0 ? "zip" : "links");
        if (index % 3 === 2) {
            assert.equal(form.get("variant"), "jpg");
            assert.deepEqual(form.getAll("timestamp"), ["00:01:02"]);
        }
    }
});

test("lossless original download URL is retained in screenshot job results", async (t) => {
    const originalURL = "/api/screenshot-original?token=original";
    t.mock.method(globalThis, "fetch", async () => ({
        ok: true,
        json: async () => ({ ok: true, link_items: [{ url: "https://example.com/shot.png", original_download_url: originalURL }] }),
    }));
    const job = await fetchScreenshotJob("job");
    assert.equal(job.linkItems[0].originalDownloadURL, originalURL);
});

test("original cleanup only deletes local token URLs", async (t) => {
    const requests = [];
    t.mock.method(globalThis, "fetch", async (url, options) => {
        requests.push({ url, options });
        return { ok: true };
    });
    for (const url of ["", "https://example.com/shot.png", "/api/screenshot-jobs/job", "/api/screenshot-original?token=../file"]) {
        await deleteOriginalPNG(url);
    }
    assert.equal(requests.length, 0);
    const url = `/api/screenshot-original?token=${"a".repeat(32)}`;
    await deleteOriginalPNG(url);
    assert.equal(requests.length, 1);
    assert.equal(requests[0].url, url);
    assert.equal(requests[0].options.method, "DELETE");
    assert.equal(requests[0].options.keepalive, true);
});

test("expired lossless originals report an error without downloading a response as PNG", async (t) => {
    t.mock.method(globalThis, "fetch", async () => ({
        ok: false,
        json: async () => ({ error: "无损原图已过期或不存在，请重新生成截图。" }),
    }));
    await assert.rejects(downloadOriginalPNG("/api/screenshot-original?token=expired", "shot.png"), /无损原图已过期/);
});

test("lossless PNG download uses original bytes and screenshot filename", async (t) => {
    const bytes = new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10]);
    const originalURL = "/api/screenshot-original?token=original";
    let downloadedBlob;
    let clicked = false;
    const anchor = { style: {}, click: () => { clicked = true; }, remove() {} };
    const previousDocument = globalThis.document;
    globalThis.document = { createElement: () => anchor, body: { appendChild() {} } };
    t.after(() => {
        if (previousDocument === undefined) delete globalThis.document;
        else globalThis.document = previousDocument;
    });
    t.mock.method(globalThis, "fetch", async (url) => {
        assert.equal(url, originalURL);
        return { ok: true, blob: async () => new Blob([bytes], { type: "image/png" }) };
    });
    t.mock.method(URL, "createObjectURL", (blob) => {
        downloadedBlob = blob;
        return "blob:lossless-original";
    });
    await downloadOriginalPNG(originalURL, "00h01m02s.png");
    assert.ok(clicked);
    assert.equal(anchor.download, "00h01m02s.png");
    assert.equal(anchor.href, "blob:lossless-original");
    assert.deepEqual(new Uint8Array(await downloadedBlob.arrayBuffer()), bytes);
});
