<template>
    <div class="field">
        <label for="path-search">媒体选择</label>
        <div class="path-picker">
            <div class="browser integrated">
                <div class="browser-toolbar">
                    <nav class="browser-breadcrumbs" aria-label="当前路径">
                        <button
                            v-for="item in breadcrumbItems"
                            :key="item.path || 'roots'"
                            class="browser-crumb"
                            type="button"
                            :disabled="busy || browserLoading || item.current"
                            @click="$emit('navigate-path', item.path)"
                        >
                            {{ item.label }}
                        </button>
                    </nav>
                </div>

                <div class="browser-search">
                    <div class="search-actions">
                        <button
                            class="ghost icon-btn"
                            :disabled="busy || browserLoading || !canNavigateUp"
                            title="上一级"
                            aria-label="上一级"
                            @click="$emit('navigate-up')"
                        >
                            <svg viewBox="0 0 24 24" aria-hidden="true">
                                <path d="M12 5 5.8 11.2l1.4 1.4 3.8-3.8V19h2V8.8l3.8 3.8 1.4-1.4L12 5Z" />
                            </svg>
                        </button>
                        <button
                            class="ghost icon-btn"
                            :disabled="busy || browserLoading"
                            title="刷新"
                            aria-label="刷新"
                            @click="$emit('refresh')"
                        >
                            <svg viewBox="0 0 24 24" aria-hidden="true">
                                <path d="M18.4 8.2A7.2 7.2 0 0 0 6.2 5.7L4.6 4.1V9h4.9L7.6 7.1a5.2 5.2 0 1 1-1.3 5.2h-2a7.2 7.2 0 1 0 14.1-4.1Z" />
                            </svg>
                        </button>
                    </div>
                    <div class="browser-search-box">
                        <svg class="browser-search-icon" viewBox="0 0 24 24" aria-hidden="true">
                            <path d="M10.8 4.5a6.3 6.3 0 1 1 0 12.6 6.3 6.3 0 0 1 0-12.6Zm0 1.8a4.5 4.5 0 1 0 0 9 4.5 4.5 0 0 0 0-9Zm5.3 9 3.7 3.7-1.3 1.3-3.7-3.7 1.3-1.3Z" />
                        </svg>
                        <input
                            id="path-search"
                            :value="searchKeyword"
                            type="text"
                            placeholder="模糊搜索当前目录"
                            @input="$emit('update:searchKeyword', $event.target.value)"
                        />
                    </div>
                </div>

                <div v-if="browserError !== ''" class="browser-error">
                    {{ browserError }}
                </div>

                <div class="browser-list">
                    <div v-if="browserLoading" class="browser-row empty">加载中...</div>
                    <div v-else-if="entries.length === 0" class="browser-row empty">当前目录无匹配项</div>
                    <div
                        v-for="entry in entries"
                        :key="entry.path"
                        class="browser-row"
                        :class="{
                            selected: normalizeComparePath(path) === normalizeComparePath(entry.path),
                            directory: entry.isDir,
                            locked: busy || browserLoading,
                        }"
                        @click="$emit('update:path', entry.path)"
                        @dblclick="$emit('open-entry', entry)"
                    >
                        <div class="browser-row-main">
                            <span class="browser-row-icon" :class="`browser-row-icon-${entryIconType(entry)}`" aria-hidden="true">
                                <svg v-if="entryIconType(entry) === 'folder'" viewBox="0 0 24 24">
                                    <path d="M3.5 6.5A2.5 2.5 0 0 1 6 4h4.2l2 2H18a2.5 2.5 0 0 1 2.5 2.5v1H3.5v-3Z" />
                                    <path d="M3.5 9h17l-1.2 8.2a2.5 2.5 0 0 1-2.5 2.1H7.2a2.5 2.5 0 0 1-2.5-2.1L3.5 9Z" />
                                </svg>
                                <svg v-else-if="entryIconType(entry) === 'disc'" viewBox="0 0 24 24">
                                    <path d="M12 3.5a8.5 8.5 0 1 0 0 17 8.5 8.5 0 0 0 0-17Zm0 5.4a3.1 3.1 0 1 1 0 6.2 3.1 3.1 0 0 1 0-6.2Z" />
                                    <path d="M12 11a1 1 0 1 0 0 2 1 1 0 0 0 0-2Z" />
                                </svg>
                                <svg v-else-if="entryIconType(entry) === 'video'" viewBox="0 0 24 24">
                                    <path d="M5.5 5h13A2.5 2.5 0 0 1 21 7.5v9a2.5 2.5 0 0 1-2.5 2.5h-13A2.5 2.5 0 0 1 3 16.5v-9A2.5 2.5 0 0 1 5.5 5Zm2 2.2v9.6l7.8-4.8-7.8-4.8Z" />
                                </svg>
                                <svg v-else viewBox="0 0 24 24">
                                    <path d="M6.5 3.5h7.8l4.2 4.2v12.8h-12v-17Zm7.2 1.8v3h3l-3-3Z" />
                                    <path d="M8.5 12h7v1.5h-7V12Zm0 3h7v1.5h-7V15Z" />
                                </svg>
                            </span>
                            <span class="browser-row-name">{{ entry.name }}</span>
                        </div>
                        <div class="browser-row-side">
                            <span v-if="showEntryDuration(entry)" class="browser-row-duration">{{ entry.duration }}</span>
                            <span v-if="showEntrySize(entry)" class="browser-row-size">{{ formatEntrySize(entry.size) }}</span>
                            <button
                                v-if="entry.isISO"
                                class="ghost browser-enter-btn"
                                type="button"
                                :disabled="busy || browserLoading"
                                title="进入 ISO"
                                aria-label="进入 ISO"
                                @click.stop="$emit('enter-entry', entry)"
                            >
                                进入
                            </button>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    </div>
</template>

<script setup>
import { computed } from "vue";
import { formatFileSize } from "../utils/path-browser";

const props = defineProps({
    path: { type: String, required: true },
    searchKeyword: { type: String, required: true },
    busy: { type: Boolean, required: true },
    browserDir: { type: String, required: true },
    browserError: { type: String, required: true },
    browserLoading: { type: Boolean, required: true },
    canNavigateUp: { type: Boolean, required: true },
    entries: { type: Array, required: true },
});

defineEmits(["update:path", "update:searchKeyword", "navigate-up", "navigate-path", "refresh", "open-entry", "enter-entry"]);

const breadcrumbItems = computed(() => buildBreadcrumbItems(props.browserDir));

const normalizeComparePath = (value) => {
    if (!value) {
        return "";
    }
    if (value === "/" || value === "\\") {
        return "/";
    }
    return value.replace(/\\/g, "/").replace(/\/+$/, "").toLowerCase();
};

const entryIconType = (entry) => {
    if (entry?.isDir) {
        return "folder";
    }
    if (entry?.isISO) {
        return "disc";
    }
    if (entry?.isMPLS || entry?.isVideo) {
        return "video";
    }
    return "file";
};

const showEntryDuration = (entry) => typeof entry?.duration === "string" && entry.duration !== "";

const showEntrySize = (entry) => !entry?.isDir && Number.isFinite(entry?.size) && entry.size > 0;

const formatEntrySize = (value) => formatFileSize(value);

function buildBreadcrumbItems(value) {
    const normalized = typeof value === "string" ? value.replace(/\\/g, "/").replace(/\/+$/, "") : "";
    if (normalized === "") {
        return [{ label: "可用挂载路径", path: "", current: true }];
    }

    const absolute = normalized.startsWith("/");
    const parts = normalized.split("/").filter(Boolean);
    if (parts.length === 0) {
        return [{ label: "/", path: "/", current: true }];
    }

    return parts.map((part, index) => {
        const prefix = absolute ? "/" : "";
        const path = prefix + parts.slice(0, index + 1).join("/");
        return {
            label: part,
            path,
            current: index === parts.length - 1,
        };
    });
}
</script>

<style scoped>
.path-picker {
    display: grid;
    gap: 10px;
}

.browser {
    border: 1px solid rgba(33, 50, 60, 0.12);
    border-radius: 14px;
    background: rgba(255, 255, 255, 0.9);
    overflow: hidden;
}

.browser-toolbar {
    display: flex;
    justify-content: flex-start;
    align-items: center;
    gap: 10px;
    padding: 9px 12px;
    background: rgba(47, 111, 109, 0.055);
    border-bottom: 1px solid var(--soft-line);
}

.browser-breadcrumbs {
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 0;
    overflow: hidden;
}

.browser-crumb {
    position: relative;
    max-width: 220px;
    min-width: 0;
    padding: 4px 18px 4px 8px;
    border: none;
    border-radius: 6px;
    background: transparent;
    box-shadow: none;
    color: var(--muted);
    font-family: var(--font-mono);
    font-size: 0.82rem;
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.browser-crumb::after {
    content: "/";
    position: absolute;
    right: 6px;
    color: rgba(74, 90, 99, 0.44);
}

.browser-crumb:last-child {
    padding-right: 8px;
}

.browser-crumb:last-child::after {
    content: "";
}

.browser-crumb:not(:disabled):hover {
    background: rgba(47, 111, 109, 0.1);
    transform: none;
}

.browser-crumb:disabled {
    opacity: 1;
    cursor: default;
}

.search-actions {
    display: flex;
    gap: 8px;
    align-items: center;
}

.browser-search {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    background: rgba(255, 255, 255, 0.52);
    border-bottom: 1px solid var(--soft-line);
}

.browser-search-box {
    position: relative;
    flex: 1;
    min-width: 0;
}

.browser-search-icon {
    position: absolute;
    left: 11px;
    top: 50%;
    width: 16px;
    height: 16px;
    color: rgba(74, 90, 99, 0.62);
    fill: currentColor;
    pointer-events: none;
    transform: translateY(-50%);
}

.browser-search input[type="text"] {
    width: 100%;
    border: 1px solid rgba(33, 50, 60, 0.16);
    border-radius: 9px;
    outline: none;
    background: rgba(255, 255, 255, 0.95);
    padding: 8px 10px 8px 34px;
}

.browser-error {
    padding: 8px 12px;
    font-size: 0.85rem;
    color: #8f2f2f;
    background: rgba(207, 80, 80, 0.08);
    border-bottom: 1px solid rgba(207, 80, 80, 0.2);
}

.browser-list {
    padding: 7px 8px;
    max-height: 340px;
    overflow: auto;
    display: grid;
    gap: 3px;
}

.browser-row {
    position: relative;
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    min-height: 38px;
    padding: 8px 10px 8px 12px;
    border-radius: 8px;
    cursor: pointer;
    user-select: none;
    -webkit-user-select: none;
    transition:
        background 0.14s ease,
        color 0.14s ease,
        transform 0.14s ease;
}

.browser-row:not(.empty):hover {
    background: rgba(47, 111, 109, 0.075);
}

.browser-row.selected {
    background: rgba(47, 111, 109, 0.15);
    box-shadow: inset 4px 0 0 rgba(47, 111, 109, 0.78);
}

.browser-row.locked {
    pointer-events: none;
    opacity: 0.68;
}

.browser-row.directory .browser-row-name {
    font-weight: 500;
}

.browser-row.selected .browser-row-name {
    font-weight: 700;
}

.browser-row.empty {
    color: var(--muted);
    justify-content: flex-start;
    cursor: default;
}

.browser-row-name {
    font-size: 0.88rem;
    font-weight: 400;
    word-break: break-all;
    line-height: 1.35;
}

.browser-row-main {
    min-width: 0;
    flex: 1;
    display: flex;
    align-items: center;
    gap: 10px;
}

.browser-row-side {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 10px;
}

.browser-row-icon {
    flex: 0 0 auto;
    width: 22px;
    height: 22px;
    opacity: 0.9;
}

.browser-row-icon svg {
    display: block;
    width: 22px;
    height: 22px;
    fill: currentColor;
}

.browser-row-icon-folder {
    color: #40918d;
}

.browser-row-icon-disc {
    color: #747f8c;
}

.browser-row-icon-video {
    color: #3f7fa6;
}

.browser-row-icon-file {
    color: #8b939b;
}

.browser-row-size {
    color: var(--muted);
    font-size: 0.78rem;
    line-height: 1.2;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
}

.browser-row-duration {
    color: var(--accent);
    font-size: 0.78rem;
    line-height: 1.2;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
}

.browser-enter-btn {
    flex: 0 0 auto;
    padding: 5px 10px;
    border-radius: 999px;
    font-size: 0.78rem;
    line-height: 1.2;
}

@media (max-width: 900px) {
    .browser-row {
        flex-direction: column;
        align-items: flex-start;
    }

    .browser-row-side {
        width: 100%;
        justify-content: space-between;
    }
}
</style>
