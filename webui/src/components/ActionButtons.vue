<template>
    <div class="actions">
        <button class="action-btn" :disabled="props.busy || !props.hasInput" @click="$emit('make-torrent')">
            <span>制作种子</span>
        </button>
        <button class="action-btn" :class="{ stoppable: isActive('mediainfo') }" :disabled="isDisabled('mediainfo')" @click="handleClick('mediainfo', 'mediainfo')">
            <span>{{ buildLabel("mediainfo", "生成 MediaInfo") }}</span>
        </button>
        <button class="action-btn" :class="{ stoppable: isActive('bdinfo') }" :disabled="isDisabled('bdinfo')" @click="handleClick('bdinfo', 'bdinfo')">
            <span>{{ buildLabel("bdinfo", "生成 BDInfo") }}</span>
        </button>
        <button class="action-btn" :class="{ stoppable: isActive('download-shots') }" :disabled="isDisabled('download-shots')" @click="handleClick('download-shots', 'download-shots')">
            <span>{{ buildLabel("download-shots", "下载截图") }}</span>
        </button>
        <button class="action-btn" :class="{ stoppable: isActive('output-links') }" :disabled="isDisabled('output-links')" @click="handleClick('output-links', 'output-links')">
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
