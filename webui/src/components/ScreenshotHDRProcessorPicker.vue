<template>
    <div class="variant-picker" role="group" aria-label="HDR / DV 色彩空间转换">
        <button
            type="button"
            class="ghost variant-option"
            :class="{ active: modelValue === 'libplacebo' }"
            :aria-pressed="modelValue === 'libplacebo'"
            :disabled="busy"
            @click="$emit('update:modelValue', 'libplacebo')"
        >
            libplacebo
        </button>
        <button
            type="button"
            class="ghost variant-option"
            :class="{ active: modelValue === 'zscale' }"
            :aria-pressed="modelValue === 'zscale'"
            :disabled="busy"
            @click="$emit('update:modelValue', 'zscale')"
        >
            zscale
        </button>
        <button
            type="button"
            class="ghost variant-option"
            :class="{ active: modelValue === 'off' }"
            :aria-pressed="modelValue === 'off'"
            :disabled="busy"
            @click="$emit('update:modelValue', 'off')"
        >
            禁用
        </button>
    </div>
</template>

<script setup>
defineProps({
    modelValue: { type: String, required: true },
    busy: { type: Boolean, required: true },
});

defineEmits(["update:modelValue"]);
</script>

<style scoped>
.variant-picker {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
}

.variant-option {
    min-width: 84px;
    border-color: rgba(33, 50, 60, 0.16);
    background: rgba(255, 255, 255, 0.88);
    color: var(--muted);
    font-weight: 500;
    box-shadow: none;
}

.variant-option.active {
    background: rgba(47, 111, 109, 0.1);
    color: var(--accent);
    border-color: rgba(47, 111, 109, 0.42);
    font-weight: 700;
    box-shadow: 0 1px 2px rgba(14, 22, 30, 0.04);
}

.variant-option:not(.active):hover {
    background: rgba(47, 111, 109, 0.06);
    border-color: rgba(47, 111, 109, 0.28);
}
</style>
