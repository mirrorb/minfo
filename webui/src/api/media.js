export async function fetchDirectory(prefix = "", signal) {
    const url = new URL("/api/path", window.location.origin);
    if (prefix !== "") {
        url.searchParams.set("prefix", prefix);
    }

    const response = await fetch(url.toString(), { signal });
    const data = await response.json();
    if (!response.ok || !data.ok || !Array.isArray(data.items)) {
        throw new Error(data.error || "读取路径失败。");
    }
    return data;
}

export async function createInfoJob(path, kind, fields = {}) {
    const response = await postForm("/api/info-jobs", { path, kind, ...fields });
    const data = normalizeInfoJobPayload(await safeReadJSON(response));
    if (!response.ok || !data.ok || typeof data.jobId !== "string" || data.jobId.trim() === "") {
        throw buildResponseError(data.error || "信息任务创建失败。", data);
    }
    return data;
}

export async function fetchInfoJob(jobId) {
    const response = await fetch(`/api/info-jobs/${encodeURIComponent(jobId)}`, {
        cache: "no-store",
        headers: {
            "Cache-Control": "no-store",
        },
    });
    const data = normalizeInfoJobPayload(await safeReadJSON(response));
    if (!response.ok || !data.ok) {
        throw buildResponseError(data.error || "信息任务状态读取失败。", data);
    }
    return data;
}

export async function cancelInfoJob(jobId) {
    const response = await fetch(`/api/info-jobs/${encodeURIComponent(jobId)}`, {
        method: "DELETE",
        cache: "no-store",
        headers: {
            "Cache-Control": "no-store",
        },
    });
    const data = normalizeInfoJobPayload(await safeReadJSON(response));
    if (!response.ok || !data.ok) {
        throw buildResponseError(data.error || "信息任务取消失败。", data);
    }
    return data;
}

export async function createScreenshotJob(path, variant, subtitleMode, count, mode) {
    const response = await postForm("/api/screenshot-jobs", { path, mode, variant, subtitle_mode: subtitleMode, count });
    const data = normalizeScreenshotJobPayload(await safeReadJSON(response));
    if (!response.ok || !data.ok || typeof data.jobId !== "string" || data.jobId.trim() === "") {
        throw buildResponseError(data.error || "截图任务创建失败。", data);
    }
    return data;
}

export async function fetchScreenshotJob(jobId) {
    const response = await fetch(`/api/screenshot-jobs/${encodeURIComponent(jobId)}`, {
        cache: "no-store",
        headers: {
            "Cache-Control": "no-store",
        },
    });
    const data = normalizeScreenshotJobPayload(await safeReadJSON(response));
    if (!response.ok || !data.ok) {
        throw buildResponseError(data.error || "截图任务状态读取失败。", data);
    }
    return data;
}

export async function cancelScreenshotJob(jobId) {
    const response = await fetch(`/api/screenshot-jobs/${encodeURIComponent(jobId)}`, {
        method: "DELETE",
        cache: "no-store",
        headers: {
            "Cache-Control": "no-store",
        },
    });
    const data = normalizeScreenshotJobPayload(await safeReadJSON(response));
    if (!response.ok || !data.ok) {
        throw buildResponseError(data.error || "截图任务取消失败。", data);
    }
    return data;
}

export function startPreparedDownload(url) {
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.style.display = "none";
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
}

async function postForm(url, fields = {}) {
    const form = new FormData();
    for (const [key, value] of Object.entries(fields)) {
        if (value !== undefined && value !== null && `${value}` !== "") {
            form.append(key, `${value}`);
        }
    }
    return fetch(url, { method: "POST", body: form });
}

async function safeReadJSON(response) {
    try {
        return await response.json();
    } catch {
        return {};
    }
}

function buildResponseError(message, data = {}) {
    const error = new Error(message);
    if (typeof data.logs === "string" && data.logs.trim() !== "") {
        error.logs = data.logs;
    }
    if (Array.isArray(data.logEntries) && data.logEntries.length > 0) {
        error.logEntries = data.logEntries;
    }
    return error;
}

function normalizeLogEntries(entries) {
    if (!Array.isArray(entries)) {
        return [];
    }
    return entries
        .filter((entry) => entry && typeof entry === "object")
        .map((entry) => ({
            timestamp: typeof entry.timestamp === "string" ? entry.timestamp : "",
            message: typeof entry.message === "string" ? entry.message : "",
        }))
        .filter((entry) => entry.timestamp !== "" || entry.message !== "");
}

function normalizeInfoJobPayload(data = {}) {
    return {
        ...data,
        ok: data.ok === true,
        jobId: typeof data.job_id === "string" ? data.job_id : "",
        status: typeof data.status === "string" ? data.status : "",
        kind: typeof data.kind === "string" ? data.kind : "",
        output: typeof data.output === "string" ? data.output : "",
        error: typeof data.error === "string" ? data.error : "",
        logs: typeof data.logs === "string" ? data.logs : "",
        logEntries: normalizeLogEntries(data.log_entries),
        progress: normalizeTaskProgress(data.progress),
    };
}

function normalizeScreenshotJobPayload(data = {}) {
    return {
        ...data,
        ok: data.ok === true,
        jobId: typeof data.job_id === "string" ? data.job_id : "",
        status: typeof data.status === "string" ? data.status : "",
        mode: typeof data.mode === "string" ? data.mode : "",
        output: typeof data.output === "string" ? data.output : "",
        downloadURL: typeof data.download_url === "string" ? data.download_url : "",
        error: typeof data.error === "string" ? data.error : "",
        logs: typeof data.logs === "string" ? data.logs : "",
        logEntries: normalizeLogEntries(data.log_entries),
        progress: normalizeTaskProgress(data.progress),
        linkItems: normalizeLinkItems(data.link_items),
        pngLossyFiles: Array.isArray(data.png_lossy_files) ? data.png_lossy_files.filter((item) => typeof item === "string" && item.trim() !== "") : [],
        pngLossyIndexes: Array.isArray(data.png_lossy_indexes)
            ? data.png_lossy_indexes
                .map((item) => Number.parseInt(`${item}`, 10))
                .filter((item) => Number.isFinite(item) && item >= 0)
            : [],
    };
}

function normalizeLinkItems(items) {
    if (!Array.isArray(items)) {
        return [];
    }

    return items
        .filter((item) => item && typeof item === "object")
        .map((item) => ({
            url: typeof item.url === "string" ? item.url : "",
            filename: typeof item.filename === "string" ? item.filename : "",
            size: Number.isFinite(item.size) && item.size > 0 ? Math.round(item.size) : 0,
        }))
        .filter((item) => item.url.trim() !== "");
}

function normalizeTaskProgress(progress) {
    if (!progress || typeof progress !== "object") {
        return null;
    }

    const percent = Number.isFinite(progress.percent)
        ? Math.min(100, Math.max(0, Number(progress.percent)))
        : 0;
    const stage = typeof progress.stage === "string" ? progress.stage : "";
    const detail = typeof progress.detail === "string" ? progress.detail : "";
    const current = Number.isFinite(progress.current) && progress.current > 0 ? Math.round(progress.current) : 0;
    const total = Number.isFinite(progress.total) && progress.total > 0 ? Math.round(progress.total) : 0;
    const indeterminate = progress.indeterminate === true;

    if (percent === 0 && stage === "" && detail === "" && current === 0 && total === 0 && !indeterminate) {
        return null;
    }

    return {
        percent,
        stage,
        detail,
        current,
        total,
        indeterminate,
    };
}
