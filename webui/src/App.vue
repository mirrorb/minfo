<template>
    <div class="grain"></div>
    <main class="shell">
        <NoticeToast :text="noticeText" />
        <AppHeader />

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
                @open-entry="handleEntryDoubleClick"
            />

            <div class="panel-section">
                <div class="panel-section-header">
                    <label>配置</label>
                </div>
                <div class="config-grid">
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
                </div>
            </div>

            <div class="panel-section panel-section-actions">
                <div class="panel-section-header">
                    <label>操作</label>
                </div>
                <ActionButtons
                    :busy="busy"
                    :active-action="activeAction"
                    :has-input="hasInput"
                    @mediainfo="runInfo('/api/mediainfo', 'MediaInfo', {}, 'mediainfo')"
                    @bdinfo="runInfo('/api/bdinfo', 'BDInfo', { bdinfo_mode: bdinfoMode }, 'bdinfo')"
                    @download-shots="downloadShots"
                    @output-links="outputShotLinks"
                />
            </div>
        </section>

        <OutputPanel
            v-if="showOutputPanel"
            :busy="busy"
            :copy-output-label="copyOutputLabel"
            :output-text="outputText"
            :status-message="statusMessage"
            @copy="copyOutputText"
            @clear="clearOutputText"
        />

        <ImageLinksPanel
            v-if="showImageLinksPanel"
            :busy="busy"
            :copy-links-label="copyLinksLabel"
            :copy-b-b-code-label="copyBBCodeLabel"
            :link-status-text="linkStatusText"
            :link-items="linkItems"
            @append-links="appendShotLinks"
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
import ScreenshotSubtitleModePicker from "./components/ScreenshotSubtitleModePicker.vue";
import ScreenshotVariantPicker from "./components/ScreenshotVariantPicker.vue";
import { useMediaActions } from "./composables/useMediaActions";
import { usePathBrowser } from "./composables/usePathBrowser";
import { loadAppState, saveAppState } from "./utils/storage";

const persistedState = loadAppState();
const screenshotVariant = ref(persistedState.screenshotVariant);
const screenshotSubtitleMode = ref(persistedState.screenshotSubtitleMode);
const bdinfoMode = ref(persistedState.bdinfoMode);
const pathBrowser = usePathBrowser({
    initialPath: persistedState.path,
    initialBrowserDir: persistedState.browserDir,
});
const mediaActions = useMediaActions(pathBrowser.path, screenshotVariant, screenshotSubtitleMode, pathBrowser.hasInput);

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
    handleEntryDoubleClick,
} = pathBrowser;

const {
    outputText,
    linkItems,
    busy,
    activeAction,
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
    clearOutputText,
    clearLinkItems,
    copyOutputText,
    copyLinks,
    copyBBCode,
    removeLink,
} = mediaActions;

watch(
    [path, browserDir, screenshotVariant, screenshotSubtitleMode, bdinfoMode],
    ([nextPath, nextBrowserDir, nextVariant, nextSubtitleMode, nextBDInfoMode]) => {
        saveAppState({
            path: nextPath,
            browserDir: nextBrowserDir,
            screenshotVariant: nextVariant,
            screenshotSubtitleMode: nextSubtitleMode,
            bdinfoMode: nextBDInfoMode,
        });
    },
    { deep: true, immediate: true },
);
</script>
