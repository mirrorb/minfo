<template>
    <header class="hero">
        <div class="hero-meta">
            <span class="hero-version">{{ versionLabel }}</span>
            <a
                class="hero-repo-link"
                :href="repoUrl"
                target="_blank"
                rel="noreferrer noopener"
                aria-label="GitHub 仓库"
                title="GitHub 仓库"
            >
                <svg viewBox="0 0 16 16" aria-hidden="true">
                    <path
                        d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38
                        0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52
                        -.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.5-1.07
                        -1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12
                        0 0 .67-.21 2.2.82a7.52 7.52 0 0 1 4 0c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12
                        .51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48
                        0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z"
                    />
                </svg>
            </a>
        </div>
        <div class="hero-copy">
            <h1>minfo</h1>
            <p class="lead">一键生成MediaInfo/BDInfo/图床</p>
        </div>
    </header>
</template>

<script setup>
defineProps({
    versionLabel: {
        type: String,
        required: true,
    },
    repoUrl: {
        type: String,
        required: true,
    },
});
</script>

<style scoped>
.hero {
    position: relative;
    display: grid;
    gap: 8px;
    grid-template-columns: 1fr;
    align-items: flex-start;
    padding-top: 2px;
}

.hero-meta {
    position: absolute;
    top: 0;
    right: 0;
    z-index: 3;
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 10px;
}

.hero-version {
    font-family: var(--font-mono);
    font-size: 0.88rem;
    color: var(--muted);
    letter-spacing: 0.02em;
}

.hero-repo-link {
    position: relative;
    z-index: 4;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    border: none;
    border-radius: 999px;
    background: transparent;
    color: var(--ink);
    text-decoration: none;
    cursor: pointer;
    pointer-events: auto;
    transition:
        transform 0.2s ease,
        opacity 0.2s ease;
}

.hero-repo-link:hover {
    opacity: 0.72;
    transform: translateY(-1px);
}

.hero-repo-link svg {
    width: 22px;
    height: 22px;
    fill: currentColor;
}

.hero-copy {
    max-width: 760px;
    padding-right: 120px;
}

h1 {
    margin: 0 0 4px;
    font-family: var(--font-body);
    font-size: clamp(2.4rem, 5vw, 3.8rem);
    font-weight: 800;
    color: #202829;
    letter-spacing: 0;
}

.lead {
    margin: 0;
    font-size: 0.96rem;
    color: rgba(74, 90, 99, 0.82);
}

@media (max-width: 900px) {
    .hero-meta {
        position: static;
        width: 100%;
        justify-content: space-between;
    }

    .hero-copy {
        padding-right: 0;
    }
}
</style>
