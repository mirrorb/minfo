const ISO_BROWSER_PREFIX = "ISO:";

export function normalizeComparePath(value) {
    if (!value) {
        return "";
    }
    if (value === "/" || value === "\\") {
        return "/";
    }
    return value.replace(/\\/g, "/").replace(/\/+$/, "").toLowerCase();
}

export function withTrailingSeparator(value) {
    if (value === "") {
        return "";
    }
    if (value.endsWith("/") || value.endsWith("\\")) {
        return value;
    }
    const separator = value.includes("\\") && !value.includes("/") ? "\\" : "/";
    return `${value}${separator}`;
}

export function cleanPath(value) {
    if (!value) {
        return "";
    }
    if (value === "/" || value === "\\") {
        return value;
    }
    return value.replace(/[\\/]+$/, "");
}

export function getEntryName(value) {
    const normalized = value.replace(/[\\/]+$/, "");
    if (normalized === "") {
        return value;
    }
    const parts = normalized.split(/[\\/]/);
    return parts[parts.length - 1] || normalized;
}

export function buildEntries(items) {
    const result = [];
    for (const raw of items) {
        let rawPath = "";
        let explicitDir = false;
        let size = 0;
        let duration = "";

        if (typeof raw === "string") {
            rawPath = raw.trim();
        } else if (raw && typeof raw === "object" && typeof raw.path === "string") {
            rawPath = raw.path.trim();
            explicitDir = raw.isDir === true;
            const parsedSize = Number.parseInt(`${raw.size ?? ""}`.trim(), 10);
            size = Number.isFinite(parsedSize) && parsedSize > 0 ? parsedSize : 0;
            duration = typeof raw.duration === "string" ? raw.duration.trim() : "";
        }

        if (rawPath === "") {
            continue;
        }
        const isDir = explicitDir || rawPath.endsWith("/") || rawPath.endsWith("\\");
        const clean = cleanPath(rawPath);
        result.push({
            path: clean,
            name: getEntryName(rawPath),
            isDir,
            isISO: !isDir && isISOFilePath(clean),
            isMPLS: !isDir && isMPLSFilePath(clean),
            isVideo: !isDir && isVideoFilePath(clean),
            duration: isDir ? "" : duration,
            size: isDir ? 0 : size,
        });
    }

    result.sort((left, right) => {
        if (left.isDir !== right.isDir) {
            return left.isDir ? -1 : 1;
        }
        return left.name.localeCompare(right.name, "zh-CN");
    });

    return result;
}

export function formatFileSize(value) {
    const size = Number.parseInt(`${value ?? ""}`.trim(), 10);
    if (!Number.isFinite(size) || size <= 0) {
        return "";
    }

    const units = ["B", "KB", "MB", "GB", "TB"];
    let current = size;
    let index = 0;
    while (current >= 1024 && index < units.length - 1) {
        current /= 1024;
        index += 1;
    }
    const digits = current >= 100 || index === 0 ? 0 : current >= 10 ? 1 : 2;
    return `${current.toFixed(digits)} ${units[index]}`;
}

export function filterEntries(entries, keyword) {
    const normalizedKeyword = keyword.trim().toLowerCase();
    if (normalizedKeyword === "") {
        return entries;
    }

    return entries.filter((entry) => {
        const name = (entry.name || "").toLowerCase();
        const full = (entry.path || "").toLowerCase();
        return name.includes(normalizedKeyword) || full.includes(normalizedKeyword);
    });
}

export function getParentDirectory(dir, root) {
    const normalized = cleanPath(dir);
    if (normalized === "" || normalized === "/") {
        return normalized;
    }

    const virtualISO = parseVirtualISOPath(normalized);
    if (virtualISO) {
        const { isoPath, innerPath } = virtualISO;
        if (innerPath === "/") {
            const slash = Math.max(isoPath.lastIndexOf("/"), isoPath.lastIndexOf("\\"));
            if (slash <= 0) {
                return root || "";
            }
            return isoPath.slice(0, slash);
        }

        const parts = innerPath.split("/").filter(Boolean);
        if (parts.length <= 1) {
            return buildVirtualISOPath(isoPath, "/");
        }
        return buildVirtualISOPath(isoPath, `/${parts.slice(0, -1).join("/")}`);
    }

    const slash = Math.max(normalized.lastIndexOf("/"), normalized.lastIndexOf("\\"));
    if (slash <= 0) {
        return root || "";
    }
    return normalized.slice(0, slash);
}

export function canNavigateUp(browserDir, browserRoot, browserRoots) {
    if (!browserDir) {
        return false;
    }

    const root = normalizeComparePath(browserRoot);
    const current = normalizeComparePath(browserDir);
    if (root === "") {
        return true;
    }
    if (current !== root) {
        return true;
    }
    return browserRoots.length > 1;
}

export function buildVirtualISOPath(isoPath, innerPath = "/") {
    const cleanedISO = cleanPath(isoPath);
    const cleanedInner = normalizeVirtualISOInnerPath(innerPath);
    if (cleanedInner === "/") {
        return `${ISO_BROWSER_PREFIX}${cleanedISO}!`;
    }
    return `${ISO_BROWSER_PREFIX}${cleanedISO}!${cleanedInner}`;
}

function isISOFilePath(value) {
    return /\.iso$/i.test(value || "");
}

function isMPLSFilePath(value) {
    return /\.mpls$/i.test(value || "");
}

function isVideoFilePath(value) {
    return /\.(m2ts|mts|mkv|mp4|m4v|mov|avi|wmv|flv|mpg|mpeg|m2v|ts|vob|ifo|webm)$/i.test(value || "");
}

function parseVirtualISOPath(value) {
    if (!value || !value.startsWith(ISO_BROWSER_PREFIX)) {
        return null;
    }

    const rest = value.slice(ISO_BROWSER_PREFIX.length);
    if (rest === "") {
        return null;
    }

    let isoPath = "";
    let innerPath = "/";
    if (rest.endsWith("!")) {
        isoPath = rest.slice(0, -1);
    } else {
        const boundary = rest.indexOf("!/");
        if (boundary < 0) {
            return null;
        }
        isoPath = rest.slice(0, boundary);
        innerPath = rest.slice(boundary + 1);
    }

    isoPath = cleanPath(isoPath);
    if (!isISOFilePath(isoPath)) {
        return null;
    }

    return {
        isoPath,
        innerPath: normalizeVirtualISOInnerPath(innerPath),
    };
}

function normalizeVirtualISOInnerPath(value) {
    const normalized = `${value || ""}`.replace(/\\/g, "/");
    const parts = normalized.split("/").filter((part) => part !== "" && part !== ".");
    const stack = [];
    for (const part of parts) {
        if (part === "..") {
            stack.pop();
            continue;
        }
        stack.push(part);
    }
    return stack.length === 0 ? "/" : `/${stack.join("/")}`;
}
