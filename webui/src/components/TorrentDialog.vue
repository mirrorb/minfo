<template>
    <Teleport to="body">
        <Transition name="modal-fade">
            <div v-if="open" class="modal-backdrop" @click.self="requestClose">
                <section class="torrent-dialog" role="dialog" aria-modal="true" aria-labelledby="torrent-dialog-title">
                    <div class="torrent-dialog-header">
                        <h2 id="torrent-dialog-title">制作种子</h2>
                        <button class="ghost icon-btn" type="button" aria-label="关闭" :disabled="busy" @click="requestClose">
                            <svg viewBox="0 0 24 24" aria-hidden="true">
                                <path d="m6.4 5 12.6 12.6-1.4 1.4L5 6.4 6.4 5Z" />
                                <path d="M17.6 5 19 6.4 6.4 19 5 17.6 17.6 5Z" />
                            </svg>
                        </button>
                    </div>
                    <div
                        v-if="busy"
                        class="torrent-dialog-progress"
                        role="progressbar"
                        aria-label="制作种子中"
                        :aria-valuenow="progressPercent"
                        aria-valuemin="0"
                        aria-valuemax="100"
                    >
                        <div
                            class="torrent-dialog-progress-bar"
                            :class="{ indeterminate: progressIndeterminate }"
                            :style="progressIndeterminate ? undefined : { width: `${progressPercent}%` }"
                        ></div>
                    </div>

                    <form class="torrent-dialog-body" @submit.prevent="submit">
                        <section class="torrent-section" aria-labelledby="torrent-settings-title">
                            <h3 id="torrent-settings-title">设置</h3>
                            <div class="torrent-settings">
                                <div class="torrent-form-field torrent-piece-field">
                                    <label for="torrent-piece-length">分块大小</label>
                                    <select id="torrent-piece-length" v-model.number="form.pieceLength" :disabled="busy">
                                        <option v-for="option in pieceLengthOptions" :key="option.value" :value="option.value">
                                            {{ option.label }}
                                        </option>
                                    </select>
                                </div>

                                <label class="torrent-check-row torrent-settings-wide">
                                    <input v-model="form.private" type="checkbox" :disabled="busy" />
                                    <span class="torrent-check-copy">
                                        <span class="torrent-check-title">私有 torrent</span>
                                        <span class="torrent-check-help">不会在 DHT 网络上分发</span>
                                    </span>
                                </label>
                            </div>
                        </section>

                        <section class="torrent-section" aria-labelledby="torrent-fields-title">
                            <h3 id="torrent-fields-title">字段</h3>
                            <div class="torrent-fields">
                                <div class="torrent-form-field">
                                    <label for="torrent-tracker-url">Tracker URL</label>
                                    <textarea
                                        id="torrent-tracker-url"
                                        v-model="form.trackerURL"
                                        rows="2"
                                        autocomplete="off"
                                        spellcheck="false"
                                        :disabled="busy"
                                    ></textarea>
                                </div>

                                <div class="torrent-form-field">
                                    <label for="torrent-web-seed-url">Web 种子 URL</label>
                                    <textarea
                                        id="torrent-web-seed-url"
                                        v-model="form.webSeedURL"
                                        rows="2"
                                        autocomplete="off"
                                        spellcheck="false"
                                        :disabled="busy"
                                    ></textarea>
                                </div>

                                <div class="torrent-form-field">
                                    <label for="torrent-comment">注释</label>
                                    <textarea
                                        id="torrent-comment"
                                        v-model="form.comment"
                                        rows="2"
                                        autocomplete="off"
                                        spellcheck="false"
                                        :disabled="busy"
                                    ></textarea>
                                </div>

                                <div class="torrent-form-field">
                                    <label for="torrent-source">源</label>
                                    <input
                                        id="torrent-source"
                                        v-model="form.source"
                                        type="text"
                                        autocomplete="off"
                                        spellcheck="false"
                                        :disabled="busy"
                                    />
                                </div>
                            </div>
                        </section>

                        <div v-if="busy" class="torrent-dialog-status">
                            <strong>{{ progressStage }}</strong>
                            <span>{{ progressDetail }}</span>
                            <span v-if="!progressIndeterminate" class="torrent-dialog-status-percent">{{ progressPercent }}%</span>
                        </div>

                        <div class="torrent-dialog-actions">
                            <button class="ghost" type="button" @click="handleSecondaryAction">{{ busy ? "停止任务" : "取消" }}</button>
                            <button type="submit" :disabled="busy || !hasTargetPath">制作并下载</button>
                        </div>
                    </form>
                </section>
            </div>
        </Transition>
    </Teleport>
</template>

<script setup>
import { computed, reactive, watch } from "vue";

const defaultOptions = {
    format: "v1",
    pieceLength: 4 << 20,
    private: true,
    trackerURL: "",
    webSeedURL: "",
    comment: "",
    source: "",
};

const pieceLengthOptions = [
    { label: "16 KiB", value: 16 << 10 },
    { label: "32 KiB", value: 32 << 10 },
    { label: "64 KiB", value: 64 << 10 },
    { label: "128 KiB", value: 128 << 10 },
    { label: "256 KiB", value: 256 << 10 },
    { label: "512 KiB", value: 512 << 10 },
    { label: "1 MiB", value: 1 << 20 },
    { label: "2 MiB", value: 2 << 20 },
    { label: "4 MiB", value: 4 << 20 },
    { label: "8 MiB", value: 8 << 20 },
    { label: "16 MiB", value: 16 << 20 },
    { label: "32 MiB", value: 32 << 20 },
    { label: "64 MiB", value: 64 << 20 },
];

const props = defineProps({
    open: { type: Boolean, required: true },
    busy: { type: Boolean, required: true },
    taskProgress: { type: Object, default: null },
    targetPath: { type: String, required: true },
    initialOptions: { type: Object, default: () => ({}) },
});

const emit = defineEmits(["close", "submit", "stop-active"]);

const form = reactive({ ...defaultOptions });

const hasTargetPath = computed(() => props.targetPath.trim() !== "");
const progressPercent = computed(() => {
    const percent = Number(props.taskProgress?.percent);
    if (!Number.isFinite(percent)) {
        return 0;
    }
    return Math.round(Math.min(100, Math.max(0, percent)));
});
const progressIndeterminate = computed(() => props.taskProgress?.indeterminate === true || progressPercent.value <= 0);
const progressStage = computed(() => props.taskProgress?.stage || "制作种子");
const progressDetail = computed(() => props.taskProgress?.detail || "正在制作种子。");

watch(
    () => props.open,
    (isOpen) => {
        if (!isOpen) {
            return;
        }
        resetForm();
    },
    { immediate: true },
);

function resetForm() {
    Object.assign(form, normalizeOptions(props.initialOptions));
}

const requestClose = () => {
    if (props.busy) {
        return;
    }
    emit("close");
};

const handleSecondaryAction = () => {
    if (props.busy) {
        emit("stop-active");
        return;
    }
    requestClose();
};

const submit = () => {
    if (!hasTargetPath.value || props.busy) {
        return;
    }
    emit("submit", normalizeOptions(form));
};

function normalizeOptions(value = {}) {
    const source = value && typeof value === "object" ? value : {};
    const pieceLength = pieceLengthOptions.some((option) => option.value === Number(source.pieceLength))
        ? Number(source.pieceLength)
        : defaultOptions.pieceLength;

    return {
        format: source.format === "v1" ? "v1" : defaultOptions.format,
        pieceLength,
        private: source.private !== false,
        trackerURL: typeof source.trackerURL === "string" ? source.trackerURL : defaultOptions.trackerURL,
        webSeedURL: typeof source.webSeedURL === "string" ? source.webSeedURL : defaultOptions.webSeedURL,
        comment: typeof source.comment === "string" ? source.comment : defaultOptions.comment,
        source: typeof source.source === "string" ? source.source : defaultOptions.source,
    };
}
</script>

<style scoped>
.modal-backdrop {
    position: fixed;
    inset: 0;
    z-index: 30;
    display: grid;
    place-items: center;
    padding: 24px;
    background: rgba(14, 22, 30, 0.38);
    backdrop-filter: blur(8px);
}

.modal-fade-enter-active,
.modal-fade-leave-active {
    transition: opacity 0.16s ease;
}

.modal-fade-enter-from,
.modal-fade-leave-to {
    opacity: 0;
}

.torrent-dialog {
    width: min(680px, calc(100vw - 32px));
    max-height: calc(100vh - 48px);
    display: flex;
    flex-direction: column;
    overflow: hidden;
    border-radius: 18px;
    background: rgba(247, 245, 239, 0.98);
    border: 1px solid rgba(33, 50, 60, 0.16);
    box-shadow: 0 24px 64px rgba(14, 22, 30, 0.26);
}

.torrent-dialog-header {
    z-index: 2;
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 14px;
    padding: 16px 18px;
    background: rgba(247, 245, 239, 0.96);
    border-bottom: 1px solid rgba(33, 50, 60, 0.12);
}

.torrent-dialog-header h2 {
    margin: 0;
    font-family: var(--font-display);
    font-size: 1.35rem;
}

.torrent-dialog-header .icon-btn {
    border: none;
    background: transparent;
    color: rgba(74, 90, 99, 0.78);
    box-shadow: none;
}

.torrent-dialog-header .icon-btn:hover {
    background: rgba(33, 50, 60, 0.08);
    color: var(--ink);
    transform: none;
}

.torrent-dialog-body {
    min-height: 0;
    overflow-x: hidden;
    overflow-y: auto;
    display: grid;
    flex: 0 1 auto;
    gap: 14px;
    padding: 16px 18px 18px;
}

.torrent-dialog-progress {
    flex: 0 0 auto;
    height: 4px;
    overflow: hidden;
    background: rgba(47, 111, 109, 0.12);
}

.torrent-dialog-progress-bar {
    height: 100%;
    border-radius: 999px;
    background: linear-gradient(90deg, rgba(47, 111, 109, 0.2), var(--accent), var(--accent-2));
    transition: width 0.18s ease;
}

.torrent-dialog-progress-bar.indeterminate {
    width: 42%;
    animation: torrent-progress-slide 1.05s ease-in-out infinite;
}

.torrent-dialog-status {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 4px 12px;
    align-items: center;
    padding: 10px 12px;
    border-radius: 8px;
    border: 1px solid rgba(47, 111, 109, 0.14);
    background: rgba(47, 111, 109, 0.08);
    color: var(--muted);
    font-weight: 600;
}

.torrent-dialog-status strong {
    color: var(--ink);
}

.torrent-dialog-status span:not(.torrent-dialog-status-percent) {
    grid-column: 1 / -1;
    font-size: 0.9rem;
    font-weight: 500;
}

.torrent-dialog-status-percent {
    grid-row: 1;
    grid-column: 2;
    font-family: var(--font-mono);
    color: var(--accent);
}

.torrent-section {
    display: grid;
    gap: 12px;
}

.torrent-section + .torrent-section {
    padding-top: 4px;
}

.torrent-section h3 {
    margin: 0;
    color: var(--ink);
    font-family: var(--font-body);
    font-size: 0.98rem;
    font-weight: 800;
}

.torrent-settings,
.torrent-fields {
    display: grid;
    gap: 12px;
}

.torrent-piece-field {
    max-width: 180px;
}

.torrent-form-field {
    display: grid;
    gap: 6px;
}

.torrent-form-field label {
    color: var(--muted);
    font-size: 0.86rem;
    font-weight: 700;
}

.torrent-form-field select,
.torrent-form-field textarea,
.torrent-form-field input[type="text"] {
    width: 100%;
    border-color: #e2e8f0;
    border-radius: 8px;
    background: rgba(255, 255, 255, 0.92);
    font-family: var(--font-mono);
    font-size: 0.88rem;
    font-weight: 400;
    transition:
        border-color 0.15s ease,
        box-shadow 0.15s ease,
        background 0.15s ease;
}

.torrent-form-field select:focus,
.torrent-form-field textarea:focus,
.torrent-form-field input[type="text"]:focus {
    outline: none;
    border-color: rgba(47, 111, 109, 0.82);
    background: #ffffff;
    box-shadow: 0 0 0 3px rgba(47, 111, 109, 0.12);
}

.torrent-form-field textarea {
    min-height: 56px;
    resize: vertical;
}

.torrent-check-row {
    display: inline-flex;
    align-items: flex-start;
    gap: 9px;
    min-height: 24px;
    font-weight: 500;
}

.torrent-check-row input {
    width: 16px;
    height: 16px;
    margin-top: 2px;
    accent-color: var(--accent);
}

.torrent-check-copy {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 8px;
    align-items: baseline;
}

.torrent-check-title {
    color: var(--ink);
    font-size: 0.93rem;
    font-weight: 600;
}

.torrent-check-help {
    color: rgba(74, 90, 99, 0.72);
    font-size: 0.82rem;
    font-weight: 500;
}

.torrent-dialog-actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
    padding-top: 2px;
}

@keyframes torrent-progress-slide {
    from {
        transform: translateX(-110%);
    }

    to {
        transform: translateX(245%);
    }
}

@media (max-width: 900px) {
    .modal-backdrop {
        align-items: start;
        padding: 14px;
    }

    .torrent-dialog {
        width: 100%;
        max-height: calc(100vh - 28px);
    }

    .torrent-dialog-actions {
        justify-content: stretch;
    }

    .torrent-dialog-actions button {
        flex: 1;
    }
}
</style>
