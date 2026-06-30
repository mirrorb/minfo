<template>
    <Teleport to="body">
        <Transition name="modal-fade">
            <div v-if="open" class="modal-backdrop" @click.self="requestClose">
                <section class="torrent-dialog" role="dialog" aria-modal="true" aria-labelledby="torrent-dialog-title">
                    <div class="torrent-dialog-header">
                        <h2 id="torrent-dialog-title">制作种子</h2>
                        <button class="ghost icon-btn" type="button" aria-label="关闭" :disabled="busy" @click="requestClose">
                            ×
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
                        <fieldset class="torrent-fieldset">
                            <legend>设置</legend>
                            <div class="torrent-settings">
                                <label for="torrent-piece-length">分块大小:</label>
                                <select id="torrent-piece-length" v-model.number="form.pieceLength" :disabled="busy">
                                    <option v-for="option in pieceLengthOptions" :key="option.value" :value="option.value">
                                        {{ option.label }}
                                    </option>
                                </select>

                                <label class="torrent-check-row torrent-settings-wide">
                                    <input v-model="form.private" type="checkbox" :disabled="busy" />
                                    <span>私有 torrent（不会在 DHT 网络上分发）</span>
                                </label>
                            </div>
                        </fieldset>

                        <fieldset class="torrent-fieldset">
                            <legend>字段</legend>
                            <div class="torrent-fields">
                                <label for="torrent-tracker-url">Tracker URL:</label>
                                <textarea
                                    id="torrent-tracker-url"
                                    v-model="form.trackerURL"
                                    rows="2"
                                    autocomplete="off"
                                    spellcheck="false"
                                    :disabled="busy"
                                ></textarea>

                                <label for="torrent-web-seed-url">Web 种子 URL:</label>
                                <textarea
                                    id="torrent-web-seed-url"
                                    v-model="form.webSeedURL"
                                    rows="2"
                                    autocomplete="off"
                                    spellcheck="false"
                                    :disabled="busy"
                                ></textarea>

                                <label for="torrent-comment">注释:</label>
                                <textarea
                                    id="torrent-comment"
                                    v-model="form.comment"
                                    rows="2"
                                    autocomplete="off"
                                    spellcheck="false"
                                    :disabled="busy"
                                ></textarea>

                                <label for="torrent-source">源:</label>
                                <input
                                    id="torrent-source"
                                    v-model="form.source"
                                    type="text"
                                    autocomplete="off"
                                    spellcheck="false"
                                    :disabled="busy"
                                />
                            </div>
                        </fieldset>

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
