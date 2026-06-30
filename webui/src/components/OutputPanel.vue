<template>
    <section class="output-panel">
        <div class="output-header">
            <h2>输出</h2>
            <div class="output-actions">
                <button class="output-clear-btn" :disabled="busy" @click="$emit('clear')">清空</button>
            </div>
        </div>
        <div class="output-body">
            <TaskProgressBar v-if="taskProgress" :progress="taskProgress" />
            <div v-if="outputText !== ''" class="output-text">
                <button class="output-copy-btn" type="button" @click="$emit('copy')" :aria-label="copyOutputLabel" :title="copyOutputLabel">
                    <svg viewBox="0 0 24 24" aria-hidden="true">
                        <path d="M8 3.5h9.5V16H8V3.5Zm2 2V14h5.5V5.5H10Z" />
                        <path d="M5 8h2v10.5h7v2H5V8Z" />
                    </svg>
                    <span>{{ copyOutputLabel }}</span>
                </button>
                <pre>{{ outputText }}</pre>
            </div>
            <div v-if="outputText === ''" class="output-empty">
                {{ busy && statusMessage ? statusMessage : "就绪。" }}
            </div>
        </div>
    </section>
</template>

<script setup>
import TaskProgressBar from "./TaskProgressBar.vue";

defineProps({
    busy: { type: Boolean, required: true },
    copyOutputLabel: { type: String, required: true },
    outputText: { type: String, required: true },
    statusMessage: { type: String, required: true },
    taskProgress: { type: Object, default: null },
});

defineEmits(["copy", "clear"]);
</script>

<style scoped>
.output-panel {
    position: relative;
    z-index: 0;
    padding: 24px;
    border: 1px solid rgba(33, 50, 60, 0.07);
    border-radius: 20px;
    background: var(--panel);
    box-shadow: var(--shadow);
    animation: output-panel-rise 0.6s ease 0.08s both;
}

.output-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 12px;
    gap: 16px;
}

.output-actions {
    display: flex;
    gap: 8px;
    align-items: center;
}

.output-header h2 {
    margin: 0;
    font-family: var(--font-display);
}

.output-clear-btn {
    border: none;
    border-radius: 8px;
    background: transparent;
    color: rgba(74, 90, 99, 0.74);
    box-shadow: none;
    padding: 8px 10px;
}

.output-clear-btn:hover {
    background: rgba(155, 55, 52, 0.08);
    color: #9b3734;
    transform: none;
}

.output-body {
    display: grid;
    gap: 16px;
}

.output-text {
    position: relative;
}

.output-copy-btn {
    position: absolute;
    top: 10px;
    right: 10px;
    z-index: 2;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    min-width: auto;
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 8px;
    padding: 7px 10px;
    background: rgba(255, 255, 255, 0.09);
    color: rgba(255, 255, 255, 0.82);
    box-shadow: none;
    backdrop-filter: blur(8px);
}

.output-copy-btn svg {
    width: 15px;
    height: 15px;
    fill: currentColor;
}

.output-copy-btn span {
    font-size: 0.78rem;
}

.output-copy-btn:hover {
    background: rgba(255, 255, 255, 0.16);
    color: #ffffff;
    transform: none;
}

.output-text pre {
    margin: 0;
    white-space: pre-wrap;
    font-family: var(--font-mono);
    font-size: 0.9rem;
    background: rgba(24, 30, 34, 0.92);
    color: #f0efe9;
    padding: 44px 18px 18px;
    border-radius: 8px;
    min-height: 220px;
}

.output-empty {
    min-height: 220px;
    border-radius: 16px;
    display: grid;
    place-items: center;
    padding: 24px;
    color: var(--muted);
    background: linear-gradient(180deg, rgba(246, 248, 249, 0.96), rgba(240, 243, 244, 0.92));
}

@keyframes output-panel-rise {
    from {
        opacity: 0;
        transform: translateY(16px);
    }

    to {
        opacity: 1;
        transform: translateY(0);
    }
}

@media (max-width: 900px) {
    .output-header {
        align-items: flex-start;
        flex-direction: column;
    }

    .output-actions {
        width: 100%;
        justify-content: flex-start;
        flex-wrap: wrap;
    }
}
</style>
