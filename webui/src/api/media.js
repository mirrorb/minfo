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

export async function requestInfo(path, url, fields = {}, options = {}) {
    const response = await postForm(url, attachLogSession({ path, ...fields }, options));
    const data = await safeReadJSON(response);
    if (!response.ok || !data.ok) {
        throw buildResponseError(data.error || "请求失败。", data);
    }
    return data;
}

export async function prepareScreenshotZipDownload(path, variant, subtitleMode, count, options = {}) {
    const response = await postForm("/api/screenshots", attachLogSession({ path, mode: "zip", variant, subtitle_mode: subtitleMode, count, prepare_download: "1" }, options));
    const data = await safeReadJSON(response);
    if (!response.ok || !data.ok || typeof data.output !== "string" || data.output.trim() === "") {
        throw buildResponseError(data.error || "截图请求失败。", data);
    }
    return {
        downloadURL: new URL(data.output, window.location.origin).toString(),
        logs: typeof data.logs === "string" ? data.logs : "",
    };
}

export function startPreparedDownload(url) {
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.style.display = "none";
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
}

export async function requestScreenshotLinks(path, variant, subtitleMode, count, options = {}) {
    const response = await postForm("/api/screenshots", attachLogSession({ path, mode: "links", variant, subtitle_mode: subtitleMode, count }, options));
    const data = await safeReadJSON(response);
    if (!response.ok || !data.ok) {
        throw buildResponseError(data.error || "图床链接请求失败。", data);
    }
    return data;
}

export function createLiveLogStream(label) {
    if (typeof window === "undefined" || typeof window.WebSocket !== "function") {
        return {
            sessionId: "",
            close: () => {},
            hasLiveMessages: () => false,
        };
    }

    const sessionId = createLogSessionId();
    const url = new URL("/api/logs", window.location.origin);
    url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    url.searchParams.set("session", sessionId);

    let messageCount = 0;
    let closed = false;
    let warned = false;
    let socket = null;

    try {
        socket = new window.WebSocket(url.toString());
    } catch (error) {
        console.warn(`[${label}] 无法创建实时日志连接。`, error);
        return {
            sessionId: "",
            close: () => {},
            hasLiveMessages: () => false,
        };
    }

    const close = () => {
        if (closed) {
            return;
        }
        closed = true;
        if (socket.readyState === window.WebSocket.CONNECTING || socket.readyState === window.WebSocket.OPEN) {
            socket.close();
        }
    };

    socket.addEventListener("message", (event) => {
        const payload = parseLogPayload(event.data);
        if (!payload) {
            return;
        }
        if (payload.type === "end") {
            close();
            return;
        }
        if (payload.type !== "line") {
            return;
        }

        messageCount += 1;
        const timestampPrefix = typeof payload.timestamp === "string" && payload.timestamp !== "" ? `[${payload.timestamp}] ` : "";
        if (typeof payload.message === "string" && payload.message !== "") {
            console.log(`[${label}] ${timestampPrefix}${payload.message}`);
            return;
        }
        console.log(`[${label}] ${timestampPrefix}`.trimEnd());
    });

    socket.addEventListener("error", () => {
        if (warned) {
            return;
        }
        warned = true;
        console.warn(`[${label}] 实时日志连接异常，将回退到请求完成后的日志。`);
    });

    socket.addEventListener("close", () => {
        closed = true;
    });

    return {
        sessionId,
        close,
        hasLiveMessages: () => messageCount > 0,
    };
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
    return error;
}

function attachLogSession(fields = {}, options = {}) {
    if (typeof options.logSession !== "string" || options.logSession.trim() === "") {
        return fields;
    }
    return { ...fields, log_session: options.logSession.trim() };
}

function createLogSessionId() {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
        return crypto.randomUUID();
    }
    return `log-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

function parseLogPayload(value) {
    if (typeof value !== "string" || value.trim() === "") {
        return null;
    }
    try {
        return JSON.parse(value);
    } catch {
        return null;
    }
}
