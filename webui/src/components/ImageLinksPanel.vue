<template>
    <section class="panel output image-links-panel">
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
                <button class="ghost output-copy-btn" @click="$emit('copy-links')">{{ copyLinksLabel }}</button>
                <button class="ghost output-copy-btn" @click="$emit('copy-bbcode')">{{ copyBBCodeLabel }}</button>
                <button class="ghost" :disabled="busy" @click="$emit('clear')">清空</button>
            </div>
        </div>
        <div class="output-body">
            <TaskProgressBar v-if="taskProgress" :progress="taskProgress" />
            <p v-if="linkStatusText !== '' && linkItems.length > 0" class="output-note">{{ linkStatusText }}</p>

            <div v-if="linkItems.length > 0" class="output-links">
                <div class="output-links-header">
                    <strong>图床链接</strong>
                    <span>{{ linkItems.length }} 条</span>
                </div>

                <div class="output-link-list">
                    <article v-for="item in linkItems" :key="item.id" class="output-link-item">
                        <a class="output-link-preview" :href="item.url" target="_blank" rel="noreferrer noopener">
                            <div v-if="item.isLossy" class="output-link-badge" :title="item.lossyTooltip || lossyTooltip" aria-label="该图片已被有损压缩">
                                有损
                            </div>
                            <div v-if="previewStateMap[item.id] !== 'loaded'" class="output-link-preview-state">
                                <div v-if="previewStateMap[item.id] === 'error'" class="output-link-preview-error">预览失败</div>
                                <div v-else class="output-link-preview-loading">
                                    <span class="output-link-spinner"></span>
                                    <span>加载中</span>
                                </div>
                            </div>
                            <img
                                :class="{ loaded: previewStateMap[item.id] === 'loaded' }"
                                :src="item.url"
                                alt="截图预览"
                                loading="lazy"
                                @load="markLoaded(item.id)"
                                @error="markError(item.id)"
                            />
                        </a>
                        <div class="output-link-details">
                            <a class="output-link-anchor" :href="item.url" target="_blank" rel="noreferrer noopener">
                                {{ item.url }}
                            </a>
                        </div>
                        <button class="ghost output-link-delete" type="button" :disabled="busy" @click.stop="$emit('remove-link', item.id)">
                            删除
                        </button>
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

const emit = defineEmits(["append-links", "stop-active", "copy-links", "copy-bbcode", "clear", "remove-link"]);

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
            nextStateMap[item.id] = previewStateMap.value[item.id] || "loading";
        }
        previewStateMap.value = nextStateMap;
    },
    { immediate: true, deep: true },
);

const markLoaded = (id) => {
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
