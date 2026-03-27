import { normalizeOutputLinks } from "./output";

const STORAGE_KEY = "minfo:webui:state:v1";
const DEFAULT_STATE = {
    path: "",
    browserDir: "",
    screenshotVariant: "png",
    bdinfoMode: "code",
    outputText: "",
    linkItems: [],
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

function normalizeState(value) {
    const source = value && typeof value === "object" ? value : {};

    return {
        path: typeof source.path === "string" ? source.path : DEFAULT_STATE.path,
        browserDir: typeof source.browserDir === "string" ? source.browserDir : DEFAULT_STATE.browserDir,
        screenshotVariant: normalizeVariant(source.screenshotVariant),
        bdinfoMode: normalizeBDInfoMode(source.bdinfoMode),
        outputText: typeof source.outputText === "string" ? source.outputText : DEFAULT_STATE.outputText,
        linkItems: normalizeOutputLinks(source.linkItems),
    };
}

function normalizeVariant(value) {
    return ["png", "jpg", "fast"].includes(value) ? value : DEFAULT_STATE.screenshotVariant;
}

function normalizeBDInfoMode(value) {
    return value === "full" ? "full" : DEFAULT_STATE.bdinfoMode;
}

function isStorageAvailable() {
    return typeof window !== "undefined" && typeof window.localStorage !== "undefined";
}
