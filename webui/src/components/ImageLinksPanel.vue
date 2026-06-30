<template>
    <section class="output-panel image-links-panel">
        <div class="output-header">
            <h2>图床</h2>
            <div class="output-actions">
                <button
                    class="ghost"
                    :class="{ stoppable: isAppendActive }"
                    :disabled="appendDisabled"
                    @click="handleAppendClick"
                >
                    {{ appendLabel }}
                </button>
                <button class="ghost output-toolbar-btn" type="button" @click="$emit('copy-links')">{{ copyLinksLabel }}</button>
                <button class="ghost output-toolbar-btn" type="button" @click="$emit('copy-bbcode')">{{ copyBBCodeLabel }}</button>
                <button class="ghost output-toolbar-btn output-toolbar-btn-danger" type="button" :disabled="busy" @click="$emit('clear')">清空</button>
            </div>
        </div>
        <div class="output-body">
            <TaskProgressBar v-if="taskProgress" :progress="taskProgress" />
            <div v-if="linkStatusText !== '' && linkItems.length > 0" class="success-banner" role="status" aria-live="polite">
                <svg class="icon-success" viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M12 2.75a9.25 9.25 0 1 1 0 18.5a9.25 9.25 0 0 1 0-18.5Zm4.18 6.98a.75.75 0 0 0-1.06 0l-4.08 4.08-2.16-2.16a.75.75 0 1 0-1.06 1.06l2.69 2.69a.75.75 0 0 0 1.06 0l4.61-4.61a.75.75 0 0 0 0-1.06Z" />
                </svg>
                <span>{{ linkStatusText }}</span>
            </div>

            <div v-if="linkItems.length > 0" class="output-links">
                <div class="output-link-list">
                    <article v-for="item in linkItems" :key="item.id" class="output-link-item">
                        <a class="output-link-preview" :href="item.url" target="_blank" rel="noreferrer noopener">
                            <div
                                v-if="previewStateMap[item.id] !== 'loaded'"
                                class="output-link-preview-state"
                                :class="{ error: previewStateMap[item.id] === 'error' }"
                            >
                                <div v-if="previewStateMap[item.id] === 'error'" class="output-link-preview-error">预览失败</div>
                                <div v-else class="output-link-preview-loading">
                                    <span class="output-link-spinner"></span>
                                    <span>加载中</span>
                                </div>
                            </div>
                            <img
                                v-if="previewURL(item) !== ''"
                                :class="{ error: previewStateMap[item.id] === 'error' }"
                                :src="previewURL(item)"
                                alt="截图预览"
                                loading="lazy"
                                @load="handlePreviewLoad(item.id)"
                                @error="markError(item.id)"
                            />
                        </a>
                        <div class="output-link-details">
                            <p v-if="item.filename" class="output-link-filename">{{ item.filename }}</p>
                            <div class="output-link-meta">
                                <span
                                    v-if="item.isLossy"
                                    class="output-link-meta-item output-link-badge"
                                    :title="item.lossyTooltip || lossyTooltip"
                                    aria-label="该图片已被有损压缩"
                                >
                                    有损
                                </span>
                                <span v-if="item.size > 0" class="output-link-meta-item">{{ formatFileSize(item.size) }}</span>
                                <span v-if="originalDimensions(item)" class="output-link-meta-item">{{ originalDimensions(item) }}</span>
                            </div>
                            <div class="output-link-url-wrapper">
                                <a class="output-link-anchor" :href="item.url" target="_blank" rel="noreferrer noopener">
                                    {{ item.url }}
                                </a>
                                <button
                                    class="output-link-copy copy-single-btn"
                                    type="button"
                                    :disabled="busy"
                                    aria-label="复制该链接"
                                    title="复制该链接"
                                    @click.stop="$emit('copy-link', item.url)"
                                >
                                    <svg viewBox="0 0 24 24" aria-hidden="true">
                                        <path d="M8 3.5h9.5V16H8V3.5Zm2 2V14h5.5V5.5H10Z" />
                                        <path d="M5 8h2v10.5h7v2H5V8Z" />
                                    </svg>
                                </button>
                            </div>
                        </div>
                        <div class="output-link-actions">
                            <button
                                v-if="item.isLossy"
                                class="ghost output-link-rerender"
                                type="button"
                                :disabled="busy"
                                @click.stop="$emit('rerender-jpg', item)"
                            >
                                <svg viewBox="0 0 24 24" aria-hidden="true">
                                    <path d="M12 5a7 7 0 0 1 6.2 3.8l1.3-2.3 1.3.8-2.1 3.8a.75.75 0 0 1-1 .3l-3.9-1.8.6-1.4 2.3 1A5.5 5.5 0 1 0 17.1 14h1.6A7 7 0 1 1 12 5Z" />
                                </svg>
                                重拍 JPG
                            </button>
                            <button
                                class="ghost output-link-delete"
                                type="button"
                                :disabled="busy"
                                aria-label="删除该链接"
                                title="删除该链接"
                                @click.stop="$emit('remove-link', item.id)"
                            >
                                <svg viewBox="0 0 24 24" aria-hidden="true">
                                    <path d="M9 4.5h6l.6 1.5H19v2H5v-2h3.4L9 4.5Zm-1 5h2v7H8v-7Zm6 0h2v7h-2v-7ZM6.5 8h11l-.8 11.5a2 2 0 0 1-2 1.9H9.3a2 2 0 0 1-2-1.9L6.5 8Z" />
                                </svg>
                            </button>
                        </div>
                    </article>
                </div>
            </div>

            <div v-else class="output-empty">
                {{ linkStatusText !== "" ? linkStatusText : "暂无图床结果。" }}
            </div>
        </div>
    </section>
</template>

<script setup>
import { computed, ref, watch } from "vue";
import TaskProgressBar from "./TaskProgressBar.vue";
import { formatFileSize } from "../utils/path-browser";

const props = defineProps({
    busy: { type: Boolean, required: true },
    activeAction: { type: String, required: true },
    stoppingAction: { type: String, required: true },
    copyLinksLabel: { type: String, required: true },
    copyBBCodeLabel: { type: String, required: true },
    linkStatusText: { type: String, required: true },
    linkItems: { type: Array, required: true },
    taskProgress: { type: Object, default: null },
});

const emit = defineEmits(["append-links", "stop-active", "copy-links", "copy-bbcode", "copy-link", "clear", "remove-link", "rerender-jpg"]);

const previewStateMap = ref({});
const lossyTooltip = "为满足图床要求该图片已被有损压缩";

const isAppendActive = computed(() => props.busy && props.activeAction === "append-links");
const appendDisabled = computed(() => {
    if (isAppendActive.value) {
        return props.stoppingAction === "append-links";
    }
    return props.busy;
});
const appendLabel = computed(() => {
    if (!isAppendActive.value) {
        return "附加图床链接";
    }
    if (props.stoppingAction === "append-links") {
        return "停止中...";
    }
    return "停止任务";
});

const handleAppendClick = () => {
    if (isAppendActive.value) {
        emit("stop-active");
        return;
    }
    emit("append-links");
};

watch(
    () => props.linkItems,
    (items) => {
        const nextStateMap = {};
        for (const item of items) {
            nextStateMap[item.id] = previewURL(item) === "" ? "error" : previewStateMap.value[item.id] || "loading";
        }
        previewStateMap.value = nextStateMap;
    },
    { immediate: true, deep: true },
);

const previewURL = (item) => {
    if (typeof item?.thumbnailURL === "string" && item.thumbnailURL.trim() !== "") {
        return item.thumbnailURL;
    }
    return "";
};

const originalDimensions = (item) => {
    const width = Number.parseInt(`${item?.width ?? ""}`.trim(), 10);
    const height = Number.parseInt(`${item?.height ?? ""}`.trim(), 10);
    if (!Number.isFinite(width) || width <= 0 || !Number.isFinite(height) || height <= 0) {
        return "";
    }
    return `${width} × ${height}`;
};

const handlePreviewLoad = (id) => {
    previewStateMap.value = {
        ...previewStateMap.value,
        [id]: "loaded",
    };
};

const markError = (id) => {
    previewStateMap.value = {
        ...previewStateMap.value,
        [id]: "error",
    };
};
</script>

<style scoped>
.output-panel {
    position: relative;
    z-index: 0;
    padding: 24px;
    border: 1px solid rgba(33, 50, 60, 0.07);
    border-radius: 20px;
    background: var(--panel);
    box-shadow: var(--shadow);
    animation: output-panel-rise 0.6s ease 0.08s both;
}

.output-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 12px;
    gap: 16px;
}

.output-actions {
    display: flex;
    gap: 8px;
    align-items: center;
}

.output-header h2 {
    margin: 0;
    font-family: var(--font-display);
}

.output-body {
    display: grid;
    gap: 16px;
}

.success-banner {
    display: flex;
    align-items: center;
    gap: 10px;
    margin: 0;
    padding: 12px 14px;
    border-radius: 14px;
    color: #166534;
    background: rgba(236, 253, 245, 0.92);
    border: 1px solid rgba(74, 222, 128, 0.22);
}

.icon-success {
    display: block;
    width: 18px;
    height: 18px;
    flex: 0 0 18px;
    color: #16a34a;
    fill: currentColor;
}

.output-toolbar-btn {
    border-radius: 8px;
    padding: 10px 14px;
    background: rgba(255, 255, 255, 0.78);
    border-color: rgba(47, 111, 109, 0.24);
    color: var(--accent);
}

.output-toolbar-btn:hover {
    background: rgba(47, 111, 109, 0.08);
    border-color: rgba(47, 111, 109, 0.36);
    color: #245f5d;
    box-shadow: 0 8px 18px rgba(47, 111, 109, 0.08);
}

.output-toolbar-btn.output-toolbar-btn-danger:hover:not(:disabled),
.output-toolbar-btn.output-toolbar-btn-danger:focus-visible:not(:disabled) {
    background: #fef2f2;
    border-color: #f87171;
    color: #dc2626;
    box-shadow: none;
    transform: none;
}

.output-empty {
    min-height: 220px;
    border-radius: 16px;
    display: grid;
    place-items: center;
    padding: 24px;
    color: var(--muted);
    background: linear-gradient(180deg, rgba(246, 248, 249, 0.96), rgba(240, 243, 244, 0.92));
}

.output-links {
    display: grid;
    gap: 12px;
}

.output-link-list {
    display: grid;
    gap: 12px;
}

.output-link-item {
    display: grid;
    grid-template-columns: minmax(220px, 280px) minmax(0, 1fr) auto;
    gap: 16px;
    align-items: start;
    padding: 14px 16px;
    border-radius: 16px;
    border: 1px solid rgba(33, 50, 60, 0.12);
    background: linear-gradient(180deg, rgba(255, 255, 255, 0.96), rgba(250, 251, 251, 0.92));
    box-shadow: 0 10px 28px rgba(14, 22, 30, 0.05);
}

.output-link-preview {
    display: block;
    position: relative;
    overflow: hidden;
    border-radius: 14px;
    background: rgba(24, 30, 34, 0.96);
    aspect-ratio: 16 / 9;
}

.output-link-preview-state {
    position: absolute;
    left: 12px;
    right: auto;
    bottom: 12px;
    z-index: 2;
    pointer-events: none;
}

.output-link-preview-state.error {
    inset: 0;
    display: grid;
    place-items: center;
    padding: 16px;
    background: linear-gradient(135deg, rgba(24, 30, 34, 0.86), rgba(47, 111, 109, 0.24));
}

.output-link-preview-loading,
.output-link-preview-error {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    color: rgba(255, 255, 255, 0.92);
    font-size: 0.9rem;
    border-radius: 999px;
    padding: 8px 12px;
    background: rgba(24, 30, 34, 0.74);
    backdrop-filter: blur(6px);
}

.output-link-preview-state.error .output-link-preview-error {
    display: grid;
    gap: 10px;
    justify-items: center;
    padding: 14px 18px;
}

.output-link-spinner {
    width: 20px;
    height: 20px;
    border-radius: 999px;
    border: 2px solid rgba(255, 255, 255, 0.24);
    border-top-color: rgba(255, 255, 255, 0.96);
    animation: spin 0.8s linear infinite;
}

.output-link-preview img {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: contain;
    background: rgba(255, 255, 255, 0.06);
    opacity: 1;
    transition: filter 0.2s ease;
}

.output-link-preview img.error {
    filter: blur(1px);
}

.output-link-details {
    min-width: 0;
    padding-top: 2px;
}

.output-link-filename {
    margin: 0 0 8px;
    font-size: 0.94rem;
    font-weight: 600;
    color: var(--ink);
    word-break: break-all;
}

.output-link-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-bottom: 10px;
}

.output-link-meta-item {
    display: inline-flex;
    align-items: center;
    min-height: 26px;
    padding: 4px 10px;
    border-radius: 999px;
    background: rgba(47, 111, 109, 0.08);
    color: var(--muted);
    font-size: 0.8rem;
    font-variant-numeric: tabular-nums;
}

.output-link-meta-item.output-link-badge {
    background: #fff7ed;
    color: #c2410c;
    border: 1px solid #ffedd5;
    font-size: 0.78rem;
    font-weight: 500;
    letter-spacing: 0;
    padding: 2px 6px;
    border-radius: 4px;
    box-shadow: none;
    cursor: help;
}

.output-link-anchor {
    font-family: var(--font-mono);
    font-size: 0.88rem;
    color: var(--accent);
    word-break: break-all;
}

.output-link-url-wrapper {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    width: fit-content;
    max-width: 100%;
    vertical-align: top;
}

.output-link-copy,
.output-link-delete {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 38px;
    height: 38px;
    padding: 0;
    border-radius: 10px;
    flex-shrink: 0;
}

.copy-single-btn {
    width: 26px !important;
    height: 26px !important;
    padding: 0 !important;
    margin: 0 !important;
    border: none !important;
    border-radius: 8px;
    background: transparent !important;
    box-shadow: none !important;
    color: #94a3b8;
}

.copy-single-btn svg,
.output-link-delete svg {
    width: 16px;
    height: 16px;
    fill: currentColor;
}

.copy-single-btn:hover {
    background: #f1f5f9 !important;
    color: #475569 !important;
    box-shadow: none;
    transform: none;
}

.output-link-actions {
    display: flex;
    flex-direction: row;
    align-items: center;
    justify-self: end;
    gap: 10px;
}

.output-link-rerender,
.output-link-delete {
    padding: 10px 14px;
    white-space: nowrap;
}

.output-link-rerender {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 4px 10px;
    border-radius: 6px;
    font-size: 0.85rem;
    color: #8a3a00;
    border-color: rgba(249, 115, 22, 0.42);
    background: rgba(245, 158, 11, 0.16);
}

.output-link-rerender svg {
    width: 14px;
    height: 14px;
    fill: currentColor;
}

.output-link-delete {
    padding: 0;
    background: rgba(33, 50, 60, 0.04);
    border-color: rgba(33, 50, 60, 0.1);
    color: rgba(74, 90, 99, 0.7);
}

.output-link-delete:hover {
    background: rgba(254, 242, 242, 0.96);
    border-color: rgba(248, 113, 113, 0.22);
    color: #dc2626;
    box-shadow: none;
}

@keyframes output-panel-rise {
    from {
        opacity: 0;
        transform: translateY(16px);
    }

    to {
        opacity: 1;
        transform: translateY(0);
    }
}

@keyframes spin {
    from {
        transform: rotate(0deg);
    }

    to {
        transform: rotate(360deg);
    }
}

@media (max-width: 900px) {
    .output-header {
        align-items: flex-start;
        flex-direction: column;
    }

    .output-actions {
        width: 100%;
        justify-content: flex-start;
        flex-wrap: wrap;
    }

    .output-toolbar-btn {
        flex: 0 1 auto;
    }

    .output-link-item {
        grid-template-columns: 1fr;
    }

    .output-link-actions {
        justify-self: flex-start;
        flex-wrap: wrap;
        align-items: center;
    }

    .output-link-url-wrapper {
        width: 100%;
    }

    .output-link-preview {
        width: 100%;
    }
}
</style>
