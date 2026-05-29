<template>
    <div class="variant-picker">
        <button
            type="button"
            class="ghost variant-option"
            :class="{ active: modelValue === 'libplacebo' }"
            :disabled="busy"
            @click="$emit('update:modelValue', 'libplacebo')"
        >
            libplacebo
        </button>
        <button
            type="button"
            class="ghost variant-option"
            :class="{ active: modelValue === 'zscale' }"
            :disabled="busy"
            @click="$emit('update:modelValue', 'zscale')"
        >
            zscale
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
