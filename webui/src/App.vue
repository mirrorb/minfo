<template>
    <div class="grain"></div>
    <main class="shell">
        <NoticeToast :text="noticeText" :type="noticeType" />
        <AppHeader :version-label="versionLabel" :repo-url="repoUrl" />

        <section class="main-panel">
            <PathBrowser
                v-model:path="path"
                v-model:search-keyword="searchKeyword"
                :busy="busy"
                :browser-dir="browserDir"
                :browser-error="browserError"
                :browser-loading="browserLoading"
                :can-navigate-up="canNavigateUp"
                :entries="filteredEntries"
                @navigate-up="navigateUp"
                @navigate-path="navigateToDirectory"
                @refresh="refreshBrowser"
                @enter-entry="handleEntryEnter"
                @open-entry="handleEntryDoubleClick"
            />

            <div class="panel-section">
                <div class="panel-section-header">
                    <button
                        class="config-section-header"
                        type="button"
                        :aria-expanded="configExpanded"
                        aria-controls="config-panel"
                        :aria-label="configExpanded ? '收起配置' : '展开配置'"
                        @click="configExpanded = !configExpanded"
                    >
                        <span class="config-section-title">配置</span>
                        <svg viewBox="0 0 24 24" aria-hidden="true" :class="{ expanded: configExpanded }">
                            <path d="M7.4 9.4 12 14l4.6-4.6L18 10.8l-6 6-6-6 1.4-1.4Z" />
                        </svg>
                    </button>
                </div>
                <Transition name="config-collapse">
                    <div id="config-panel" v-show="configExpanded" class="config-grid">
                        <div class="field">
                            <label class="field-label-muted">截图模式</label>
                            <ScreenshotVariantPicker v-model="screenshotVariant" :busy="busy" />
                        </div>

                        <div class="field">
                            <label class="field-label-muted">BDInfo 输出</label>
                            <BDInfoOutputPicker v-model="bdinfoMode" :busy="busy" />
                        </div>
                        <div class="field">
                            <label class="field-label-muted">字幕处理</label>
                            <ScreenshotSubtitleModePicker v-model="screenshotSubtitleMode" :busy="busy" />
                        </div>
                        <div class="field">
                            <div class="field-label-with-help">
                                <label class="field-label-muted">HDR / DV 色彩空间转换</label>
                                <span
                                    class="field-help"
                                    tabindex="0"
                                    role="note"
                                    aria-label="查看 HDR / DV 色彩空间转换说明"
                                >
                                    <span class="field-help-trigger" aria-hidden="true">?</span>
                                    <span class="field-help-bubble">
                                        <strong>libplacebo</strong>：HDR / DV 处理更完整，色调映射通常更好，也更适合杜比视界场景<br />
                                        <strong>zscale</strong>：兼容性更好，但 HDR 处理相对保守，且无法正确应用杜比视界元数据，可能出现偏色或映射不准
                                    </span>
                                </span>
                            </div>
                            <ScreenshotHDRProcessorPicker v-model="screenshotHDRProcessor" :busy="busy" />
                        </div>
                        <div class="field">
                            <label for="screenshot-count" class="field-label-muted">截图数量</label>
                            <input
                                id="screenshot-count"
                                class="config-number-input"
                                type="number"
                                inputmode="numeric"
                                min="1"
                                max="10"
                                step="1"
                                :disabled="busy"
                                :value="screenshotCount"
                                @input="handleScreenshotCountInput"
                                @blur="handleScreenshotCountBlur"
                            />
                        </div>
                        <div class="field config-field-wide">
                            <label for="upload-proxy-url" class="field-label-muted">图床代理</label>
                            <input
                                id="upload-proxy-url"
                                v-model="uploadProxyURL"
                                class="config-text-input"
                                type="text"
                                inputmode="url"
                                autocomplete="off"
                                spellcheck="false"
                                placeholder="http://宿主机网关IP:7890"
                                :disabled="busy"
                            />
                        </div>
                    </div>
                </Transition>
            </div>

            <div class="panel-section panel-section-actions">
                <div class="panel-section-header">
                    <label>操作</label>
                </div>
                <ActionButtons
                    :busy="busy"
                    :active-action="activeAction"
                    :stopping-action="stoppingAction"
                    :has-input="hasInput"
                    @mediainfo="runInfo('/api/mediainfo', 'MediaInfo', {}, 'mediainfo')"
                    @bdinfo="runInfo('/api/bdinfo', 'BDInfo', { bdinfo_mode: bdinfoMode }, 'bdinfo')"
                    @download-shots="downloadShots"
                    @output-links="outputShotLinks"
                    @make-torrent="torrentDialogOpen = true"
                    @stop-active="stopActiveTask"
                />
            </div>
        </section>

        <TorrentDialog
            :open="torrentDialogOpen"
            :busy="busy"
            :task-progress="taskProgress"
            :target-path="path"
            :initial-options="torrentOptions"
            @close="torrentDialogOpen = false"
            @stop-active="stopActiveTask"
            @submit="handleTorrentSubmit"
        />

        <OutputPanel
            v-if="showOutputPanel"
            :busy="busy"
            :copy-output-label="copyOutputLabel"
            :output-text="outputText"
            :status-message="statusMessage"
            :task-progress="taskProgress"
            @copy="copyOutputText"
            @clear="clearOutputText"
        />

        <ImageLinksPanel
            v-if="showImageLinksPanel"
            :busy="busy"
            :active-action="activeAction"
            :stopping-action="stoppingAction"
            :copy-links-label="copyLinksLabel"
            :copy-b-b-code-label="copyBBCodeLabel"
            :link-status-text="linkStatusText"
            :link-items="linkItems"
            :task-progress="taskProgress"
            @append-links="appendShotLinks"
            @stop-active="stopActiveTask"
            @copy-links="copyLinks"
            @copy-bbcode="copyBBCode"
            @copy-link="copyLinkItem"
            @clear="clearLinkItems"
            @remove-link="removeLink"
            @rerender-jpg="rerenderLossyLinkAsJPG"
        />

    </main>
</template>

<script setup>
import { ref, watch } from "vue";
import ActionButtons from "./components/ActionButtons.vue";
import AppHeader from "./components/AppHeader.vue";
import BDInfoOutputPicker from "./components/BDInfoOutputPicker.vue";
import ImageLinksPanel from "./components/ImageLinksPanel.vue";
import NoticeToast from "./components/NoticeToast.vue";
import OutputPanel from "./components/OutputPanel.vue";
import PathBrowser from "./components/PathBrowser.vue";
import ScreenshotHDRProcessorPicker from "./components/ScreenshotHDRProcessorPicker.vue";
import ScreenshotSubtitleModePicker from "./components/ScreenshotSubtitleModePicker.vue";
import ScreenshotVariantPicker from "./components/ScreenshotVariantPicker.vue";
import TorrentDialog from "./components/TorrentDialog.vue";
import { useMediaActions } from "./composables/useMediaActions";
import { usePathBrowser } from "./composables/usePathBrowser";
import { loadAppState, saveAppState } from "./utils/storage";

const repoUrl = "https://github.com/mirrorb/minfo";
const appVersion = `${import.meta.env.VITE_APP_VERSION || "dev"}`.trim() || "dev";
const versionLabel = /^\d/.test(appVersion) ? `v${appVersion}` : appVersion;

const persistedState = loadAppState();
const screenshotVariant = ref(persistedState.screenshotVariant);
const screenshotSubtitleMode = ref(persistedState.screenshotSubtitleMode);
const screenshotHDRProcessor = ref(persistedState.screenshotHDRProcessor);
const screenshotCount = ref(persistedState.screenshotCount);
const uploadProxyURL = ref(persistedState.uploadProxyURL);
const bdinfoMode = ref(persistedState.bdinfoMode);
const configExpanded = ref(persistedState.configExpanded);
const torrentDialogOpen = ref(false);
const torrentOptions = ref(persistedState.torrentOptions);
const pathBrowser = usePathBrowser({
    initialPath: persistedState.path,
    initialBrowserDir: persistedState.browserDir,
});
const mediaActions = useMediaActions(
    pathBrowser.path,
    screenshotVariant,
    screenshotSubtitleMode,
    screenshotHDRProcessor,
    screenshotCount,
    uploadProxyURL,
    pathBrowser.hasInput,
);

const clampScreenshotCount = (value) => {
    const parsed = Number.parseInt(`${value ?? ""}`.trim(), 10);
    if (!Number.isFinite(parsed)) {
        return 4;
    }
    return Math.min(10, Math.max(1, parsed));
};

const handleScreenshotCountInput = (event) => {
    const nextValue = clampScreenshotCount(event?.target?.value);
    screenshotCount.value = nextValue;
    if (event?.target) {
        event.target.value = `${nextValue}`;
    }
};

const handleScreenshotCountBlur = (event) => {
    const nextValue = clampScreenshotCount(event?.target?.value || screenshotCount.value);
    screenshotCount.value = nextValue;
    if (event?.target) {
        event.target.value = `${nextValue}`;
    }
};

const {
    path,
    searchKeyword,
    browserDir,
    browserError,
    browserLoading,
    canNavigateUp,
    filteredEntries,
    hasInput,
    navigateUp,
    navigateToDirectory,
    refreshBrowser,
    handleEntryEnter,
    handleEntryDoubleClick,
} = pathBrowser;

const {
    outputText,
    linkItems,
    busy,
    activeAction,
    stoppingAction,
    taskProgress,
    noticeText,
    noticeType,
    linkStatusText,
    copyOutputLabel,
    copyLinksLabel,
    copyBBCodeLabel,
    statusMessage,
    showOutputPanel,
    showImageLinksPanel,
    runInfo,
    downloadShots,
    outputShotLinks,
    makeTorrent,
    appendShotLinks,
    stopActiveTask,
    clearOutputText,
    clearLinkItems,
    copyOutputText,
    copyLinks,
    copyBBCode,
    copyLinkItem,
    removeLink,
    rerenderLossyLinkAsJPG,
} = mediaActions;

const handleTorrentSubmit = async (options) => {
    torrentOptions.value = options;
    const ok = await makeTorrent(options);
    if (ok) {
        torrentDialogOpen.value = false;
    }
};

watch(
    [path, browserDir, screenshotVariant, screenshotSubtitleMode, screenshotHDRProcessor, screenshotCount, uploadProxyURL, configExpanded, bdinfoMode, torrentOptions],
    ([nextPath, nextBrowserDir, nextVariant, nextSubtitleMode, nextHDRProcessor, nextScreenshotCount, nextUploadProxyURL, nextConfigExpanded, nextBDInfoMode, nextTorrentOptions]) => {
        saveAppState({
            path: nextPath,
            browserDir: nextBrowserDir,
            screenshotVariant: nextVariant,
            screenshotSubtitleMode: nextSubtitleMode,
            screenshotHDRProcessor: nextHDRProcessor,
            screenshotCount: nextScreenshotCount,
            uploadProxyURL: nextUploadProxyURL,
            configExpanded: nextConfigExpanded,
            bdinfoMode: nextBDInfoMode,
            torrentOptions: nextTorrentOptions,
        });
    },
    { deep: true, immediate: true },
);
</script>

<style scoped>
.grain {
    position: fixed;
    inset: 0;
    pointer-events: none;
    opacity: 0.4;
    background-image: url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='120' height='120' viewBox='0 0 120 120'><filter id='n'><feTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='2'/></filter><rect width='120' height='120' filter='url(%23n)' opacity='0.08'/></svg>");
}

.shell {
    max-width: 1100px;
    margin: 0 auto;
    padding: 14px 32px 56px;
    display: grid;
    gap: 20px;
}

.main-panel {
    position: relative;
    z-index: 1;
    padding: 24px;
    border: 1px solid rgba(33, 50, 60, 0.07);
    border-radius: 20px;
    background: var(--panel);
    box-shadow: var(--shadow);
    animation: main-panel-rise 0.6s ease both;
}

.panel-section {
    display: grid;
    gap: 16px;
}

.panel-section + .panel-section {
    margin-top: 28px;
    padding-top: 8px;
}

.panel-section-header {
    display: grid;
    gap: 0;
}

.config-section-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    width: 100%;
    padding: 8px 10px;
    margin: -8px -10px;
    border: none;
    border-radius: 12px;
    background: transparent;
    color: inherit;
    box-shadow: none;
    cursor: pointer;
    transition:
        background 0.16s ease,
        color 0.16s ease;
}

.config-section-header:hover {
    background: rgba(47, 111, 109, 0.06);
    transform: none;
}

.config-section-header:focus-visible {
    outline: 2px solid rgba(47, 111, 109, 0.26);
    outline-offset: 2px;
}

.config-section-title {
    font-size: 1.02rem;
    font-weight: 700;
    text-align: left;
}

.config-section-header svg {
    width: 18px;
    height: 18px;
    fill: currentColor;
    transition: transform 0.16s ease;
}

.config-section-header svg.expanded {
    transform: rotate(180deg);
}

.config-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 18px 20px;
    padding: 18px;
    border-radius: 16px;
    background: #f9fafb;
    border: 1px solid rgba(33, 50, 60, 0.08);
}

.config-grid .field {
    margin-bottom: 0;
}

.config-collapse-enter-active,
.config-collapse-leave-active {
    max-height: 760px;
    overflow: hidden;
    transform-origin: top;
    transition:
        max-height 0.22s ease,
        opacity 0.18s ease,
        transform 0.22s ease,
        padding-top 0.22s ease,
        padding-bottom 0.22s ease,
        border-top-width 0.22s ease,
        border-bottom-width 0.22s ease;
}

.config-collapse-enter-from,
.config-collapse-leave-to {
    max-height: 0;
    padding-top: 0;
    padding-bottom: 0;
    border-top-width: 0;
    border-bottom-width: 0;
    opacity: 0;
    transform: translateY(-4px);
}

.config-field-wide {
    grid-column: 1 / -1;
}

.config-text-input {
    width: 100%;
    min-width: 0;
    min-height: 38px;
    padding: 9px 12px;
    font-family: var(--font-mono);
}

.config-number-input {
    width: 96px;
}

.panel-section-actions :deep(.actions) {
    padding: 16px 18px;
    border-radius: 16px;
    background: rgba(47, 111, 109, 0.04);
    border: 1px solid rgba(47, 111, 109, 0.12);
}

.field-label-muted {
    font-weight: 600;
    font-size: 0.9rem;
    color: var(--muted);
}

.field-label-with-help {
    display: inline-flex;
    align-items: center;
    gap: 8px;
}

.field-help {
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    outline: none;
}

.field-help-trigger {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    border-radius: 999px;
    border: 1px solid rgba(47, 111, 109, 0.35);
    background: rgba(47, 111, 109, 0.08);
    color: var(--accent);
    font-size: 0.75rem;
    font-weight: 700;
    line-height: 1;
    cursor: help;
    transition: background 0.15s ease, transform 0.15s ease;
}

.field-help:hover .field-help-trigger,
.field-help:focus .field-help-trigger,
.field-help:focus-within .field-help-trigger {
    background: rgba(47, 111, 109, 0.16);
    transform: translateY(-1px);
}

.field-help-bubble {
    position: absolute;
    top: calc(100% + 10px);
    left: 0;
    z-index: 20;
    width: min(320px, calc(100vw - 88px));
    padding: 10px 12px;
    border-radius: 12px;
    background: rgba(27, 31, 34, 0.96);
    color: rgba(255, 255, 255, 0.92);
    font-size: 0.8rem;
    font-weight: 400;
    line-height: 1.55;
    box-shadow: 0 12px 28px rgba(14, 22, 30, 0.24);
    opacity: 0;
    pointer-events: none;
    transform: translateY(-4px);
    transition: opacity 0.16s ease, transform 0.16s ease;
}

.field-help-bubble::before {
    content: "";
    position: absolute;
    top: -6px;
    left: 10px;
    width: 12px;
    height: 12px;
    background: rgba(27, 31, 34, 0.96);
    transform: rotate(45deg);
}

.field-help:hover .field-help-bubble,
.field-help:focus .field-help-bubble,
.field-help:focus-within .field-help-bubble {
    opacity: 1;
    transform: translateY(0);
}

.field-help-bubble strong {
    color: #ffffff;
}

@keyframes main-panel-rise {
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
    .shell {
        padding: 12px 20px 44px;
    }

    .config-grid {
        grid-template-columns: 1fr;
    }
}
</style>
