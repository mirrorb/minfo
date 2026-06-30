<template>
    <transition name="notice-fade">
        <div v-if="text !== ''" class="notice-toast" :class="`notice-toast-${type}`" role="status" aria-live="polite">
            {{ text }}
        </div>
    </transition>
</template>

<script setup>
defineProps({
    text: { type: String, required: true },
    type: { type: String, default: "error" },
});
</script>

<style scoped>
.notice-toast {
    position: fixed;
    top: max(18px, env(safe-area-inset-top, 0px) + 12px);
    right: max(18px, env(safe-area-inset-right, 0px) + 12px);
    z-index: 40;
    width: min(520px, calc(100vw - 36px));
    padding: 12px 16px;
    border-radius: 14px;
    background: rgba(141, 43, 43, 0.94);
    color: #fff7f2;
    box-shadow: var(--shadow);
    border: 1px solid rgba(110, 33, 33, 0.18);
    pointer-events: none;
    backdrop-filter: blur(14px);
}

.notice-toast-success {
    background: rgba(238, 252, 245, 0.96);
    color: #166534;
    border-color: rgba(52, 168, 83, 0.18);
    box-shadow: 0 16px 40px rgba(22, 101, 52, 0.14);
}

.notice-fade-enter-active,
.notice-fade-leave-active {
    transition: opacity 0.18s ease, transform 0.18s ease;
}

.notice-fade-enter-from,
.notice-fade-leave-to {
    opacity: 0;
    transform: translateY(-6px);
}
</style>
