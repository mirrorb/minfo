<template>
    <div class="variant-picker">
        <button
            type="button"
            class="ghost variant-option"
            :class="{ active: modelValue === 'auto' }"
            :disabled="busy"
            @click="$emit('update:modelValue', 'auto')"
        >
            挂载字幕
        </button>
        <button
            type="button"
            class="ghost variant-option"
            :class="{ active: modelValue === 'off' }"
            :disabled="busy"
            @click="$emit('update:modelValue', 'off')"
        >
            纯净截图
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
