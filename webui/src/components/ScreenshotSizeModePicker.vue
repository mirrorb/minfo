<template>
    <div class="size-picker" role="group" aria-label="像素宽高比 (SAR)">
        <button
            v-for="option in options"
            :key="option.value"
            type="button"
            class="ghost size-option"
            :class="{ active: modelValue === option.value }"
            :aria-pressed="modelValue === option.value"
            :disabled="busy"
            :title="option.description"
            @click="$emit('update:modelValue', option.value)"
        >
            {{ option.label }}
        </button>
    </div>
    <p class="size-description">{{ selectedDescription }}</p>
</template>

<script setup>
import { computed } from "vue";

const props = defineProps({
    modelValue: { type: String, required: true },
    busy: { type: Boolean, required: true },
});

defineEmits(["update:modelValue"]);

const options = [
    { value: "auto", label: "智能校正", description: "自动按正确的播放比例拉正 DVD、老电视录像等变形片源" },
    { value: "original", label: "原始像素", description: "保留视频原始像素尺寸，不进行宽高比缩放。" },
];
const selectedDescription = computed(() => options.find((option) => option.value === props.modelValue)?.description || options[0].description);
</script>

<style scoped>
.size-picker {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
}

.size-option {
    border-color: rgba(33, 50, 60, 0.16);
    background: rgba(255, 255, 255, 0.88);
    color: var(--muted);
    font-weight: 500;
    box-shadow: none;
}

.size-option.active {
    background: rgba(47, 111, 109, 0.1);
    color: var(--accent);
    border-color: rgba(47, 111, 109, 0.42);
    font-weight: 700;
}

.size-option:not(.active):hover {
    background: rgba(47, 111, 109, 0.06);
    border-color: rgba(47, 111, 109, 0.28);
}

.size-description {
    margin: 6px 0 0;
    color: var(--muted);
    font-size: 0.8rem;
    line-height: 1.5;
}
</style>
