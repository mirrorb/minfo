<template>
    <div class="grain"></div>
    <main class="shell">
        <NoticeToast :text="noticeText" />
        <AppHeader :version-label="versionLabel" :repo-url="repoUrl" />

        <section class="panel">
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
                @refresh="refreshBrowser"
                @enter-entry="handleEntryEnter"
                @open-entry="handleEntryDoubleClick"
            />

            <div class="panel-section">
                <div class="panel-section-header config-section-header">
                    <label>配置</label>
                    <button
                        class="config-toggle icon-btn ghost"
                        type="button"
                        :aria-expanded="configExpanded"
                        aria-controls="config-panel"
                        :aria-label="configExpanded ? '收起配置' : '展开配置'"
                        @click="configExpanded = !configExpanded"
                    >
                        <span aria-hidden="true">{{ configExpanded ? "⌃" : "⌄" }}</span>
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
                    @stop-active="stopActiveTask"
                />
            </div>
        </section>

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
            @clear="clearLinkItems"
            @remove-link="removeLink"
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
    appendShotLinks,
    stopActiveTask,
    clearOutputText,
    clearLinkItems,
    copyOutputText,
    copyLinks,
    copyBBCode,
    removeLink,
} = mediaActions;

watch(
    [path, browserDir, screenshotVariant, screenshotSubtitleMode, screenshotHDRProcessor, screenshotCount, uploadProxyURL, configExpanded, bdinfoMode],
    ([nextPath, nextBrowserDir, nextVariant, nextSubtitleMode, nextHDRProcessor, nextScreenshotCount, nextUploadProxyURL, nextConfigExpanded, nextBDInfoMode]) => {
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
        });
    },
    { deep: true, immediate: true },
);
</script>
