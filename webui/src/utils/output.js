export async function copyText(text) {
    if (navigator.clipboard && typeof navigator.clipboard.writeText === "function") {
        try {
            await navigator.clipboard.writeText(text);
            return;
        } catch {}
    }

    const textarea = document.createElement("textarea");
    textarea.value = text;
    textarea.setAttribute("readonly", "");
    textarea.style.position = "fixed";
    textarea.style.top = "0";
    textarea.style.left = "-9999px";
    textarea.style.opacity = "0";
    document.body.appendChild(textarea);
    textarea.focus();
    textarea.select();
    textarea.setSelectionRange(0, textarea.value.length);

    let copied = false;
    try {
        copied = document.execCommand("copy");
    } finally {
        textarea.remove();
    }

    if (!copied) {
        throw new Error("当前环境不支持剪贴板写入。");
    }
}

export function extractDirectLinks(text) {
    if (typeof text !== "string" || text.trim() === "") {
        return [];
    }

    const lines = text.split("\n");
    const links = [];
    const seen = new Set();

    for (const line of lines) {
        const url = normalizeDirectLink(line);
        if (!url || seen.has(url)) {
            continue;
        }
        seen.add(url);
        links.push(url);
    }

    return links;
}

export function normalizeOutputLinks(items) {
    if (!Array.isArray(items)) {
        return [];
    }

    const links = [];
    const seen = new Set();

    for (const item of items) {
        const url = normalizeDirectLink(item?.url);
        if (!url || seen.has(url)) {
            continue;
        }
        seen.add(url);
        links.push({
            id: typeof item?.id === "string" && item.id.trim() !== "" ? item.id : buildLinkId(),
            url,
            filename: typeof item?.filename === "string" ? item.filename : "",
            size: normalizeLinkSize(item?.size),
            isLossy: item?.isLossy === true,
            lossyTooltip: typeof item?.lossyTooltip === "string" ? item.lossyTooltip : "",
        });
    }

    return links;
}

export function mergeOutputLinks(existingItems, incomingLinks) {
    const currentItems = normalizeOutputLinks(existingItems);
    const seen = new Set(currentItems.map((item) => item.url));
    const additions = [];
    let duplicateCount = 0;

    for (const link of incomingLinks) {
        const normalizedLink = normalizeIncomingLink(link);
        const url = normalizedLink.url;
        if (!url) {
            continue;
        }
        if (seen.has(url)) {
            duplicateCount += 1;
            continue;
        }
        seen.add(url);
        additions.push({
            id: buildLinkId(),
            url,
            filename: normalizedLink.filename,
            size: normalizedLink.size,
            isLossy: normalizedLink.isLossy,
            lossyTooltip: normalizedLink.lossyTooltip,
        });
    }

    return {
        items: [...currentItems, ...additions],
        addedCount: additions.length,
        duplicateCount,
    };
}

export function buildCopyText(outputText, linkItems) {
    const text = typeof outputText === "string" ? outputText.trim() : "";
    const links = normalizeOutputLinks(linkItems).map((item) => item.url);
    const parts = [];

    if (text !== "") {
        parts.push(text);
    }
    if (links.length > 0) {
        parts.push(links.join("\n"));
    }

    return parts.join("\n\n").trim();
}

export function buildLinkText(linkItems) {
    return normalizeOutputLinks(linkItems)
        .map((item) => item.url)
        .join("\n")
        .trim();
}

export function buildBBCodeText(linkItems) {
    return normalizeOutputLinks(linkItems)
        .map((item) => `[img]${item.url}[/img]`)
        .join("\n")
        .trim();
}

function normalizeDirectLink(value) {
    if (typeof value !== "string") {
        return "";
    }

    const url = value.trim();
    if (url === "") {
        return "";
    }
    if (!url.startsWith("http://") && !url.startsWith("https://")) {
        return "";
    }
    if (/[\s[\]()<>"]/.test(url)) {
        return "";
    }

    return url;
}

function normalizeIncomingLink(value) {
    if (typeof value === "string") {
        return {
            url: normalizeDirectLink(value),
            filename: "",
            size: 0,
            isLossy: false,
            lossyTooltip: "",
        };
    }

    if (!value || typeof value !== "object") {
        return {
            url: "",
            filename: "",
            size: 0,
            isLossy: false,
            lossyTooltip: "",
        };
    }

    return {
        url: normalizeDirectLink(value.url),
        filename: typeof value.filename === "string" ? value.filename.trim() : "",
        size: normalizeLinkSize(value.size),
        isLossy: value.isLossy === true,
        lossyTooltip: typeof value.lossyTooltip === "string" ? value.lossyTooltip : "",
    };
}

function normalizeLinkSize(value) {
    const size = Number.parseInt(`${value ?? ""}`.trim(), 10);
    if (!Number.isFinite(size) || size <= 0) {
        return 0;
    }
    return size;
}

function buildLinkId() {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
        return crypto.randomUUID();
    }
    return `shot-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}
