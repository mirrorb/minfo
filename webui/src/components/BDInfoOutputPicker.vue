<template>
    <div class="variant-picker">
        <button
            type="button"
            class="ghost variant-option"
            :class="{ active: modelValue === 'code' }"
            :disabled="busy"
            @click="$emit('update:modelValue', 'code')"
        >
            精简报告
        </button>
        <button
            type="button"
            class="ghost variant-option"
            :class="{ active: modelValue === 'full' }"
            :disabled="busy"
            @click="$emit('update:modelValue', 'full')"
        >
            完整报告
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
