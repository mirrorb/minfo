const STORAGE_KEY = "minfo:webui:state:v1";
const ACTIVE_TASK_STORAGE_KEY = "minfo:webui:active-task:v1";
const DEFAULT_STATE = {
    path: "",
    browserDir: "",
    screenshotVariant: "png",
    screenshotSubtitleMode: "auto",
    screenshotHDRProcessor: "libplacebo",
    screenshotCount: 4,
    uploadProxyURL: "",
    configExpanded: false,
    bdinfoMode: "code",
    torrentOptions: {
        format: "v1",
        pieceLength: 4 << 20,
        private: true,
        trackerURL: "",
        webSeedURL: "",
        comment: "",
        source: "",
    },
};

export function loadAppState() {
    if (!isStorageAvailable()) {
        return { ...DEFAULT_STATE };
    }

    try {
        const raw = window.localStorage.getItem(STORAGE_KEY);
        if (!raw) {
            return { ...DEFAULT_STATE };
        }
        return normalizeState(JSON.parse(raw));
    } catch {
        return { ...DEFAULT_STATE };
    }
}

export function saveAppState(state) {
    if (!isStorageAvailable()) {
        return;
    }

    try {
        const normalizedState = normalizeState(state);
        window.localStorage.setItem(STORAGE_KEY, JSON.stringify(normalizedState));
    } catch {}
}

export function loadActiveTask() {
    if (!isStorageAvailable()) {
        return null;
    }

    try {
        const raw = window.localStorage.getItem(ACTIVE_TASK_STORAGE_KEY);
        if (!raw) {
            return null;
        }
        return normalizeActiveTask(JSON.parse(raw));
    } catch {
        return null;
    }
}

export function saveActiveTask(task) {
    if (!isStorageAvailable()) {
        return;
    }

    try {
        const normalizedTask = normalizeActiveTask(task);
        if (!normalizedTask) {
            window.localStorage.removeItem(ACTIVE_TASK_STORAGE_KEY);
            return;
        }
        window.localStorage.setItem(ACTIVE_TASK_STORAGE_KEY, JSON.stringify(normalizedTask));
    } catch {}
}

export function clearActiveTask() {
    if (!isStorageAvailable()) {
        return;
    }
    try {
        window.localStorage.removeItem(ACTIVE_TASK_STORAGE_KEY);
    } catch {}
}

function normalizeState(value) {
    const source = value && typeof value === "object" ? value : {};

    return {
        path: typeof source.path === "string" ? source.path : DEFAULT_STATE.path,
        browserDir: typeof source.browserDir === "string" ? source.browserDir : DEFAULT_STATE.browserDir,
        screenshotVariant: normalizeVariant(source.screenshotVariant),
        screenshotSubtitleMode: normalizeSubtitleMode(source.screenshotSubtitleMode),
        screenshotHDRProcessor: normalizeHDRProcessor(source.screenshotHDRProcessor),
        screenshotCount: normalizeScreenshotCount(source.screenshotCount),
        uploadProxyURL: normalizeUploadProxyURL(source.uploadProxyURL),
        configExpanded: source.configExpanded === true,
        bdinfoMode: normalizeBDInfoMode(source.bdinfoMode),
        torrentOptions: normalizeTorrentOptions(source.torrentOptions),
    };
}

function normalizeActiveTask(value) {
    const source = value && typeof value === "object" ? value : null;
    if (!source) {
        return null;
    }

    const jobType = normalizeTaskJobType(source.jobType);
    const action = normalizeTaskAction(source.action);
    const panel = normalizeTaskPanel(source.panel);
    const jobId = typeof source.jobId === "string" ? source.jobId.trim() : "";
    const logLabel = typeof source.logLabel === "string" ? source.logLabel.trim() : "";

    if (jobType === "" || action === "" || panel === "" || jobId === "" || logLabel === "") {
        return null;
    }

    return {
        jobType,
        action,
        panel,
        jobId,
        logLabel,
    };
}

function normalizeVariant(value) {
    return ["png", "jpg"].includes(value) ? value : DEFAULT_STATE.screenshotVariant;
}

function normalizeTaskJobType(value) {
    return value === "info" || value === "screenshot" || value === "torrent" ? value : "";
}

function normalizeTaskAction(value) {
    switch (value) {
        case "mediainfo":
        case "bdinfo":
        case "download-shots":
        case "output-links":
        case "append-links":
        case "rerender-jpg":
        case "make-torrent":
            return value;
        default:
            return "";
    }
}

function normalizeTaskPanel(value) {
    return value === "output" || value === "links" ? value : "";
}

function normalizeSubtitleMode(value) {
    return value === "off" ? "off" : DEFAULT_STATE.screenshotSubtitleMode;
}

function normalizeHDRProcessor(value) {
    return value === "zscale" ? "zscale" : DEFAULT_STATE.screenshotHDRProcessor;
}

function normalizeBDInfoMode(value) {
    return value === "full" ? "full" : DEFAULT_STATE.bdinfoMode;
}

function normalizeScreenshotCount(value) {
    const parsed = Number.parseInt(`${value ?? ""}`.trim(), 10);
    if (!Number.isFinite(parsed)) {
        return DEFAULT_STATE.screenshotCount;
    }
    return Math.min(10, Math.max(1, parsed));
}

function normalizeUploadProxyURL(value) {
    return typeof value === "string" ? value.trim() : DEFAULT_STATE.uploadProxyURL;
}

function normalizeTorrentOptions(value) {
    const source = value && typeof value === "object" ? value : {};
    const defaults = DEFAULT_STATE.torrentOptions;
    return {
        format: source.format === "v1" ? "v1" : defaults.format,
        pieceLength: normalizeTorrentPieceLength(source.pieceLength),
        private: source.private !== false,
        trackerURL: typeof source.trackerURL === "string" ? source.trackerURL : defaults.trackerURL,
        webSeedURL: typeof source.webSeedURL === "string" ? source.webSeedURL : defaults.webSeedURL,
        comment: typeof source.comment === "string" ? source.comment : defaults.comment,
        source: typeof source.source === "string" ? source.source : defaults.source,
    };
}

function normalizeTorrentPieceLength(value) {
    const parsed = Number.parseInt(`${value ?? ""}`.trim(), 10);
    const allowed = [
        16 << 10,
        32 << 10,
        64 << 10,
        128 << 10,
        256 << 10,
        512 << 10,
        1 << 20,
        2 << 20,
        4 << 20,
        8 << 20,
        16 << 20,
        32 << 20,
        64 << 20,
    ];
    return allowed.includes(parsed) ? parsed : DEFAULT_STATE.torrentOptions.pieceLength;
}

function isStorageAvailable() {
    return typeof window !== "undefined" && typeof window.localStorage !== "undefined";
}
