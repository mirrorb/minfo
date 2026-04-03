import { computed, ref, watch } from "vue";
import { createLiveLogStream, prepareScreenshotZipDownload, requestInfo, requestScreenshotLinks, startPreparedDownload } from "../api/media";
import { buildBBCodeText, buildCopyText, buildLinkText, copyText, extractDirectLinks, mergeOutputLinks } from "../utils/output";

export function useMediaActions(path, screenshotVariant, screenshotSubtitleMode, screenshotCount, hasInput) {
    const outputText = ref("");
    const linkItems = ref([]);
    const busy = ref(false);
    const activeAction = ref("");
    const activePanel = ref("");
    const statusMessage = ref("");
    const linkStatusText = ref("");
    const copyOutputStatus = ref("");
    const copyLinksStatus = ref("");
    const copyBBCodeStatus = ref("");
    const noticeText = ref("");
    let noticeTimer = null;
    const copyOutputLabel = computed(() => copyOutputStatus.value || "复制输出");
    const copyLinksLabel = computed(() => copyLinksStatus.value || "复制链接");
    const copyBBCodeLabel = computed(() => copyBBCodeStatus.value || "复制 BBCode");
    const showOutputPanel = computed(() => activePanel.value === "output" && (busy.value || statusMessage.value !== "" || outputText.value !== ""));
    const showImageLinksPanel = computed(() => activePanel.value === "links" && (busy.value || linkStatusText.value !== "" || linkItems.value.length > 0));

    const setBusy = (isBusy, label, action = "") => {
        busy.value = isBusy;
        activeAction.value = isBusy ? action : "";
        statusMessage.value = isBusy ? label || "" : "";
    };

    const setOutputText = (text) => {
        outputText.value = typeof text === "string" ? text : "";
    };

    const setLinkStatusText = (text) => {
        linkStatusText.value = typeof text === "string" ? text : "";
    };

    const clearOutputState = () => {
        setOutputText("");
    };

    const clearLinkState = () => {
        linkItems.value = [];
        setLinkStatusText("");
    };

    const hidePanels = () => {
        activePanel.value = "";
    };

    const activateOutputPanel = () => {
        activePanel.value = "output";
        clearLinkState();
        clearOutputState();
    };

    const activateImageLinksPanel = (clearLinks = true) => {
        activePanel.value = "links";
        clearOutputState();
        if (clearLinks) {
            clearLinkState();
        }
    };

    const showNotice = (message) => {
        noticeText.value = typeof message === "string" ? message : "";
        if (noticeTimer) {
            clearTimeout(noticeTimer);
        }
        noticeTimer = setTimeout(() => {
            noticeText.value = "";
            noticeTimer = null;
        }, 2400);
    };

    const logConsoleLogs = (label, logs, isError = false) => {
        if (typeof logs !== "string" || logs.trim() === "") {
            return;
        }
        const write = isError ? console.error : console.log;
        const lines = formatConsoleLogLines(logs);
        for (const line of lines) {
            write(`[${label}] ${line}`);
        }
    };

    const logConsoleLogsWithFallback = (label, logs, liveStream, isError = false) => {
        if (liveStream?.hasLiveMessages()) {
            return;
        }
        logConsoleLogs(label, logs, isError);
    };

    watch(path, (nextValue, previousValue) => {
        if (normalizeTargetPath(nextValue) === normalizeTargetPath(previousValue)) {
            return;
        }
        clearOutputState();
        clearLinkState();
        hidePanels();
    });

    const runInfo = async (url, label, fields = {}, action = "") => {
        if (!hasInput.value) {
            showNotice("请先选择媒体路径。");
            return;
        }
        const logLabel = action || label.toLowerCase();
        const liveStream = createLiveLogStream(logLabel);
        try {
            activateOutputPanel();
            setBusy(true, `${label} 生成中...`, action);
            const data = await requestInfo(path.value.trim(), url, fields, { logSession: liveStream.sessionId });
            logConsoleLogsWithFallback(logLabel, data.logs, liveStream);
            setOutputText(data.output || "没有输出。");
        } catch (err) {
            logConsoleLogsWithFallback(`${logLabel} failed`, err?.logs, liveStream, true);
            clearOutputState();
            hidePanels();
            showNotice(err?.message || "请求失败。");
        } finally {
            liveStream.close();
            setBusy(false);
        }
    };

    const downloadShots = async () => {
        if (!hasInput.value) {
            showNotice("请先选择媒体路径。");
            return;
        }
        const liveStream = createLiveLogStream("screenshots");
        try {
            activateOutputPanel();
            setBusy(true, "正在生成截图...", "download-shots");
            const { downloadURL, logs } = await prepareScreenshotZipDownload(path.value.trim(), screenshotVariant.value, screenshotSubtitleMode.value, screenshotCount.value, {
                logSession: liveStream.sessionId,
            });
            logConsoleLogsWithFallback("screenshots download", logs, liveStream);
            startPreparedDownload(downloadURL);
            setOutputText("截图已生成。");
        } catch (err) {
            logConsoleLogsWithFallback("screenshots download failed", err?.logs, liveStream, true);
            clearOutputState();
            hidePanels();
            showNotice(err?.message || "截图请求失败。");
        } finally {
            liveStream.close();
            setBusy(false);
        }
    };

    const outputShotLinks = async () => {
        if (!hasInput.value) {
            showNotice("请先选择媒体路径。");
            return;
        }
        const liveStream = createLiveLogStream("screenshots");
        try {
            activateImageLinksPanel(true);
            setBusy(true, "", "output-links");
            setLinkStatusText("正在生成截图并上传...");
            const data = await requestScreenshotLinks(path.value.trim(), screenshotVariant.value, screenshotSubtitleMode.value, screenshotCount.value, {
                logSession: liveStream.sessionId,
            });
            logConsoleLogsWithFallback("screenshots upload", data.logs, liveStream);
            const output = data.output || "";
            const links = extractDirectLinks(output);

            if (links.length > 0) {
                const { items, addedCount, duplicateCount } = mergeOutputLinks([], links);
                linkItems.value = items;

                if (addedCount === 0) {
                    setLinkStatusText("本次没有生成可用图床链接。");
                } else if (duplicateCount > 0) {
                    setLinkStatusText(`已生成 ${addedCount} 条图床链接，忽略 ${duplicateCount} 条重复链接。`);
                } else {
                    setLinkStatusText(`已生成 ${addedCount} 条图床链接。`);
                }
                return;
            }

            setLinkStatusText(output || "没有返回图床链接。");
        } catch (err) {
            logConsoleLogsWithFallback("screenshots upload failed", err?.logs, liveStream, true);
            clearLinkState();
            hidePanels();
            showNotice(err?.message || "图床链接请求失败。");
        } finally {
            liveStream.close();
            setBusy(false);
        }
    };

    const appendShotLinks = async () => {
        if (!hasInput.value) {
            showNotice("请先选择媒体路径。");
            return;
        }
        const previousStatusText = linkStatusText.value;
        const liveStream = createLiveLogStream("screenshots");
        try {
            activateImageLinksPanel(false);
            setBusy(true);
            setLinkStatusText("正在生成截图并上传...");
            const data = await requestScreenshotLinks(path.value.trim(), screenshotVariant.value, screenshotSubtitleMode.value, screenshotCount.value, {
                logSession: liveStream.sessionId,
            });
            logConsoleLogsWithFallback("screenshots upload", data.logs, liveStream);
            const output = data.output || "";
            const links = extractDirectLinks(output);

            if (links.length > 0) {
                const { items, addedCount, duplicateCount } = mergeOutputLinks(linkItems.value, links);
                linkItems.value = items;

                if (addedCount === 0) {
                    setLinkStatusText(`本次没有新增图床链接，当前共 ${linkItems.value.length} 条。`);
                } else if (duplicateCount > 0) {
                    setLinkStatusText(`新增 ${addedCount} 条图床链接，忽略 ${duplicateCount} 条重复链接，当前共 ${linkItems.value.length} 条。`);
                } else {
                    setLinkStatusText(`新增 ${addedCount} 条图床链接，当前共 ${linkItems.value.length} 条。`);
                }
                return;
            }

            setLinkStatusText(output || "没有返回图床链接。");
        } catch (err) {
            logConsoleLogsWithFallback("screenshots upload failed", err?.logs, liveStream, true);
            setLinkStatusText(previousStatusText);
            showNotice(err?.message || "图床链接请求失败。");
        } finally {
            liveStream.close();
            setBusy(false);
        }
    };

    const clearOutputText = () => {
        if (busy.value) {
            return;
        }
        clearOutputState();
        if (activePanel.value === "output") {
            hidePanels();
        }
    };

    const clearLinkItems = () => {
        if (busy.value) {
            return;
        }
        clearLinkState();
        if (activePanel.value === "links") {
            hidePanels();
        }
    };

    const copyOutputText = async () => {
        const text = buildCopyText(outputText.value, []);
        if (text.trim() === "") {
            showNotice("没有可复制的内容。");
            return;
        }

        try {
            await copyText(text);
            copyOutputStatus.value = "已复制";
            setTimeout(() => {
                copyOutputStatus.value = "";
            }, 1200);
        } catch (err) {
            showNotice(err?.message || "复制失败。");
        }
    };

    const copyLinks = async () => {
        const text = buildLinkText(linkItems.value);
        if (text.trim() === "") {
            showNotice("没有可复制的链接。");
            return;
        }

        try {
            await copyText(text);
            copyLinksStatus.value = "已复制";
            setTimeout(() => {
                copyLinksStatus.value = "";
            }, 1200);
        } catch (err) {
            showNotice(err?.message || "复制链接失败。");
        }
    };

    const copyBBCode = async () => {
        const text = buildBBCodeText(linkItems.value);
        if (text.trim() === "") {
            showNotice("没有可复制的 BBCode。");
            return;
        }

        try {
            await copyText(text);
            copyBBCodeStatus.value = "已复制";
            setTimeout(() => {
                copyBBCodeStatus.value = "";
            }, 1200);
        } catch (err) {
            showNotice(err?.message || "复制 BBCode 失败。");
        }
    };

    const removeLink = (id) => {
        if (busy.value) {
            return;
        }

        const nextItems = linkItems.value.filter((item) => item.id !== id);
        if (nextItems.length === linkItems.value.length) {
            return;
        }

        linkItems.value = nextItems;
        if (nextItems.length === 0) {
            clearLinkState();
            if (activePanel.value === "links") {
                hidePanels();
            }
            return;
        }
        setLinkStatusText(`已移除 1 条图床链接，当前共 ${nextItems.length} 条。`);
    };

    return {
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
    };
}

function normalizeTargetPath(value) {
    return typeof value === "string" ? value.trim() : "";
}

function formatConsoleLogLines(logs) {
    const normalized = `${logs}`.replaceAll("\r\n", "\n").replaceAll("\r", "\n");
    return normalized
        .split("\n")
        .filter((line) => line.trim() !== "")
        .map((line) => (hasTimePrefix(line) ? line : `[${formatConsoleTime(new Date())}] ${line}`));
}

function hasTimePrefix(line) {
    return /^\[\d{2}:\d{2}:\d{2}\]\s/.test(line);
}

function formatConsoleTime(value) {
    const hours = `${value.getHours()}`.padStart(2, "0");
    const minutes = `${value.getMinutes()}`.padStart(2, "0");
    const seconds = `${value.getSeconds()}`.padStart(2, "0");
    return `${hours}:${minutes}:${seconds}`;
}
