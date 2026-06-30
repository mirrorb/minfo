<template>
    <section class="task-progress" :class="{ indeterminate: progress.indeterminate }" aria-live="polite">
        <div class="task-progress-header">
            <strong>{{ progress.stage || "处理中" }}</strong>
            <span>{{ displayPercentLabel }}%</span>
        </div>

        <div class="task-progress-track" role="progressbar" :aria-valuenow="displayPercent" aria-valuemin="0" aria-valuemax="100">
            <div class="task-progress-fill" :style="fillStyle"></div>
        </div>

        <p v-if="detailText !== ''" class="task-progress-detail">{{ detailText }}</p>
    </section>
</template>

<script setup>
import { computed } from "vue";

const props = defineProps({
    progress: { type: Object, required: true },
});

const rawPercent = computed(() => Math.min(100, Math.max(0, Number(props.progress?.percent) || 0)));
const displayPercent = computed(() => Math.round(rawPercent.value));
const displayPercentLabel = computed(() => {
    const value = rawPercent.value;
    const rounded = Math.round(value);
    if (Math.abs(value - rounded) < 0.05) {
        return `${rounded}`;
    }
    return value.toFixed(1);
});

const fillStyle = computed(() => ({
    width: `${rawPercent.value}%`,
}));

const detailText = computed(() => {
    const detail = typeof props.progress?.detail === "string" ? props.progress.detail.trim() : "";
    if (detail !== "") {
        return detail;
    }
    const hasCounter = Number.isFinite(props.progress?.current) && props.progress.current > 0 && Number.isFinite(props.progress?.total) && props.progress.total > 0;
    if (hasCounter) {
        return `${props.progress.current}/${props.progress.total}`;
    }
    return "";
});
</script>

<style scoped>
.task-progress {
    display: grid;
    gap: 10px;
    padding: 14px 16px;
    border-radius: 16px;
    background: linear-gradient(180deg, rgba(245, 248, 248, 0.96), rgba(238, 242, 242, 0.92));
    border: 1px solid rgba(47, 111, 109, 0.12);
}

.task-progress-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
}

.task-progress-header strong {
    font-size: 0.95rem;
}

.task-progress-header span {
    font-family: var(--font-mono);
    font-size: 0.86rem;
    color: var(--muted);
}

.task-progress-track {
    height: 12px;
    border-radius: 999px;
    overflow: hidden;
    background: rgba(47, 111, 109, 0.12);
}

.task-progress-fill {
    position: relative;
    height: 100%;
    min-width: 14px;
    border-radius: inherit;
    background: linear-gradient(90deg, #2f6f6d, #3a8885);
    box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.1);
    transition: width 0.35s ease;
}

.task-progress.indeterminate .task-progress-fill::after {
    content: "";
    position: absolute;
    inset: 0;
    background: linear-gradient(110deg, transparent 0%, rgba(255, 255, 255, 0.42) 50%, transparent 100%);
    transform: translateX(-100%);
    animation: task-progress-shimmer 1.15s linear infinite;
}

.task-progress-detail {
    margin: 0;
    color: var(--muted);
    font-size: 0.9rem;
}

@keyframes task-progress-shimmer {
    from {
        transform: translateX(-100%);
    }

    to {
        transform: translateX(100%);
    }
}
</style>
