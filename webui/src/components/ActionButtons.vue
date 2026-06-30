<template>
    <div class="actions">
        <button class="action-btn" :disabled="props.busy || !props.hasInput" @click="$emit('make-torrent')">
            <span class="action-icon" aria-hidden="true">
                <svg viewBox="0 0 24 24"><path d="M8 6.5a4 4 0 0 1 8 0v2.2h-2V6.5a2 2 0 0 0-4 0v2.2H8V6.5Zm0 6.2h2v4.8a2 2 0 0 0 4 0v-4.8h2v4.8a4 4 0 0 1-8 0v-4.8Z" /><path d="M5.8 9h12.4v3.2H5.8V9Z" /></svg>
            </span>
            <span>制作种子</span>
        </button>
        <button class="action-btn" :class="{ stoppable: isActive('mediainfo') }" :disabled="isDisabled('mediainfo')" @click="handleClick('mediainfo', 'mediainfo')">
            <span class="action-icon" aria-hidden="true">
                <svg viewBox="0 0 24 24"><path d="M6 3.5h8l4 4v13H6v-17Zm7 1.8v3.2h3.2L13 5.3Z" /><path d="M8.5 12h7v1.5h-7V12Zm0 3h7v1.5h-7V15Z" /></svg>
            </span>
            <span>{{ buildLabel("mediainfo", "生成 MediaInfo") }}</span>
        </button>
        <button class="action-btn" :class="{ stoppable: isActive('bdinfo') }" :disabled="isDisabled('bdinfo')" @click="handleClick('bdinfo', 'bdinfo')">
            <span class="action-icon" aria-hidden="true">
                <svg viewBox="0 0 24 24"><path d="M5 4h9l5 5v11H5V4Zm8 1.8V10h4.2L13 5.8Z" /><path d="M8 12h8v1.5H8V12Zm0 3h8v1.5H8V15Z" /></svg>
            </span>
            <span>{{ buildLabel("bdinfo", "生成 BDInfo") }}</span>
        </button>
        <button class="action-btn" :class="{ stoppable: isActive('download-shots') }" :disabled="isDisabled('download-shots')" @click="handleClick('download-shots', 'download-shots')">
            <span class="action-icon" aria-hidden="true">
                <svg viewBox="0 0 24 24"><path d="M5 5h14v9H5V5Zm2 2v5h10V7H7Z" /><path d="M11 15h2v3.2l2.2-2.2 1.3 1.3L12 21.8l-4.5-4.5L8.8 16l2.2 2.2V15Z" /></svg>
            </span>
            <span>{{ buildLabel("download-shots", "下载截图") }}</span>
        </button>
        <button class="action-btn" :class="{ stoppable: isActive('output-links') }" :disabled="isDisabled('output-links')" @click="handleClick('output-links', 'output-links')">
            <span class="action-icon" aria-hidden="true">
                <svg viewBox="0 0 24 24"><path d="M9.2 7.3 7.8 5.9l-4.1 4.1a4 4 0 0 0 5.7 5.7l2.1-2.1-1.4-1.4L8 14.3a2 2 0 0 1-2.8-2.8l4-4.2Z" /><path d="m14.8 16.7 1.4 1.4 4.1-4.1a4 4 0 0 0-5.7-5.7l-2.1 2.1 1.4 1.4 2.1-2.1a2 2 0 0 1 2.8 2.8l-4 4.2Z" /><path d="m8.8 13.8 5-5 1.4 1.4-5 5-1.4-1.4Z" /></svg>
            </span>
            <span>{{ buildLabel("output-links", "生成图床链接") }}</span>
        </button>
    </div>
</template>

<script setup>
const props = defineProps({
    busy: { type: Boolean, required: true },
    activeAction: { type: String, required: true },
    stoppingAction: { type: String, required: true },
    hasInput: { type: Boolean, required: true },
});

const emit = defineEmits(["mediainfo", "bdinfo", "download-shots", "output-links", "make-torrent", "stop-active"]);

const isActive = (action) => props.busy && props.activeAction === action;

const isDisabled = (action) => {
    if (isActive(action)) {
        return props.stoppingAction === action;
    }
    return props.busy || !props.hasInput;
};

const handleClick = (action, eventName) => {
    if (isActive(action)) {
        emit("stop-active");
        return;
    }
    emit(eventName);
};

const buildLabel = (action, idleLabel) => {
    if (!isActive(action)) {
        return idleLabel;
    }
    if (props.stoppingAction === action) {
        return "停止中...";
    }
    return "停止任务";
};
</script>

<style scoped>
.actions {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
}

.action-btn {
    min-width: 148px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    white-space: nowrap;
    border: 1px solid rgba(47, 111, 109, 0.2);
    border-radius: 8px;
    background: rgba(255, 255, 255, 0.92);
    color: var(--accent);
    box-shadow: 0 1px 2px rgba(14, 22, 30, 0.06);
    transition:
        transform 0.15s ease,
        color 0.15s ease,
        background 0.15s ease,
        border-color 0.15s ease,
        box-shadow 0.15s ease;
}

.action-btn:hover {
    border-color: rgba(47, 111, 109, 0.36);
    background: rgba(47, 111, 109, 0.1);
    color: #245f5d;
    box-shadow: 0 8px 18px rgba(47, 111, 109, 0.12);
}

.action-icon {
    display: inline-flex;
    width: 17px;
    height: 17px;
    color: currentColor;
}

.action-icon svg {
    display: block;
    width: 17px;
    height: 17px;
    fill: currentColor;
}

.action-btn.stoppable,
:deep(button.ghost.stoppable) {
    background: #9b3734;
    color: white;
    border-color: #9b3734;
    box-shadow: 0 8px 18px rgba(155, 55, 52, 0.18);
}
</style>
