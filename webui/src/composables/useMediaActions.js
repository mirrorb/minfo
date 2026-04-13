import { computed, ref, watch } from "vue";
import {
    cancelInfoJob,
    cancelScreenshotJob,
    createInfoJob,
    createScreenshotJob,
    fetchInfoJob,
    fetchScreenshotJob,
    startPreparedDownload,
} from "../api/media";
import { clearActiveTask, loadActiveTask, saveActiveTask } from "../utils/storage";
import { buildBBCodeText, buildCopyText, buildLinkText, copyText, extractDirectLinks, mergeOutputLinks } from "../utils/output";

export function useMediaActions(path, screenshotVariant, screenshotSubtitleMode, screenshotCount, hasInput) {
    const outputText = ref("");
    const linkItems = ref([]);
    const busy = ref(false);
    const activeAction = ref("");
    const activePanel = ref("");
    const activeTask = ref(null);
    const statusMessage = ref("");
    const taskProgress = ref(null);
    const stoppingAction = ref("");
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

    const setBusy = (isBusy, label = "", action = "") => {
        busy.value = isBusy;
        activeAction.value = isBusy ? action : "";
        statusMessage.value = isBusy ? label || "" : "";
        if (!isBusy) {
            stoppingAction.value = "";
        }
    };

    const setOutputText = (text) => {
        outputText.value = typeof text === "string" ? text : "";
    };

    const setLinkStatusText = (text) => {
        linkStatusText.value = typeof text === "string" ? text : "";
    };

    const setTaskProgress = (progress) => {
        taskProgress.value = normalizeTaskProgressPayload(progress);
    };

    const clearOutputState = () => {
        setOutputText("");
    };

    const clearLinkState = () => {
        linkItems.value = [];
        setLinkStatusText("");
    };

    const clearTaskProgress = () => {
        taskProgress.value = null;
    };

    const hidePanels = () => {
        activePanel.value = "";
        clearTaskProgress();
    };

    const activateOutputPanel = () => {
        activePanel.value = "output";
        clearTaskProgress();
        clearLinkState();
        clearOutputState();
    };

    const activateImageLinksPanel = (clearLinks = true) => {
        activePanel.value = "links";
        clearTaskProgress();
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

    const persistActiveTask = (task) => {
        if (!task || typeof task !== "object") {
            activeTask.value = null;
            clearActiveTask();
            return;
        }
        activeTask.value = task;
        saveActiveTask(task);
    };

    const clearPersistedActiveTask = () => {
        activeTask.value = null;
        clearActiveTask();
    };

    const logConsoleLogs = (label, logs, logEntries = [], isError = false) => {
        const write = isError ? console.error : console.log;
        if (Array.isArray(logEntries) && logEntries.length > 0) {
            for (const entry of logEntries) {
                if (isInternalProgressLog(entry?.message)) {
                    continue;
                }
                write(`[${label}] ${formatStructuredConsoleLogLine(entry)}`);
            }
            return;
        }
        if (typeof logs !== "string" || logs.trim() === "") {
            return;
        }
        const lines = formatConsoleLogLines(logs);
        for (const line of lines) {
            write(`[${label}] ${line}`);
        }
    };

    const logTaskError = (label, err) => {
        if (err?.logEntriesPrinted) {
            return;
        }
        if (err?.canceled) {
            logConsoleLogs(`${label} canceled`, err?.logs, err?.logEntries, false);
            return;
        }
        logConsoleLogs(`${label} failed`, err?.logs, err?.logEntries, true);
    };

    const waitForAsyncJob = async (fetchJob, jobId, label, onProgress) => {
        let printedLogCount = 0;

        for (;;) {
            const job = await fetchJob(jobId);
            const currentLogEntries = Array.isArray(job.logEntries) ? job.logEntries : [];
            if (currentLogEntries.length > printedLogCount) {
                logConsoleLogs(label, "", currentLogEntries.slice(printedLogCount));
                printedLogCount = currentLogEntries.length;
            }

            if (typeof onProgress === "function") {
                onProgress(job);
            }

            switch (job.status) {
                case "pending":
                case "running":
                    await sleep(1000);
                    continue;
                case "canceling":
                    await sleep(500);
                    continue;
                case "succeeded":
                    return job;
                case "canceled": {
                    const error = buildAsyncJobError(job);
                    error.logEntriesPrinted = currentLogEntries.length > 0 && printedLogCount >= currentLogEntries.length;
                    throw error;
                }
                case "failed": {
                    const error = buildAsyncJobError(job);
                    error.logEntriesPrinted = currentLogEntries.length > 0 && printedLogCount >= currentLogEntries.length;
                    throw error;
                }
                default: {
                    const error = buildAsyncJobError({
                        error: `未知任务状态：${job.status || "unknown"}`,
                        logs: job.logs,
                        logEntries: job.logEntries,
                    });
                    error.logEntriesPrinted = currentLogEntries.length > 0 && printedLogCount >= currentLogEntries.length;
                    throw error;
                }
            }
        }
    };

    watch(path, (nextValue, previousValue) => {
        if (normalizeTargetPath(nextValue) === normalizeTargetPath(previousValue)) {
            return;
        }
        clearOutputState();
        clearLinkState();
        hidePanels();
    });

    const applyInfoProgress = (label, action, status, progress = null) => {
        const message = buildInfoProgressMessage(label, status);
        setBusy(true, message, action);
        if (action === "mediainfo") {
            setTaskProgress(null);
            return;
        }
        setTaskProgress(resolveTaskProgress(progress, buildInfoFallbackProgress(label, status)));
    };

    const applyDownloadProgress = (status, progress = null) => {
        const message = buildDownloadProgressMessage(status);
        setBusy(true, message, "download-shots");
        setTaskProgress(resolveTaskProgress(progress, buildDownloadFallbackProgress(status)));
    };

    const applyLinkProgress = (action, status, progress = null) => {
        const message = buildLinkProgressMessage(action, status);
        setBusy(true, message, action);
        setLinkStatusText(message);
        setTaskProgress(resolveTaskProgress(progress, buildLinkFallbackProgress(status)));
    };

    const stopActiveTask = async () => {
        const task = activeTask.value || loadActiveTask();
        if (!task || busy.value !== true) {
            return;
        }
        if (stoppingAction.value === task.action) {
            return;
        }

        stoppingAction.value = task.action;
        try {
            const job = task.jobType === "info" ? await cancelInfoJob(task.jobId) : await cancelScreenshotJob(task.jobId);
            applyPersistedTaskProgress(task, job.status, job.progress);
        } catch (err) {
            stoppingAction.value = "";
            showNotice(err?.message || "停止任务失败。");
        }
    };

    const runInfoTask = async ({ label, fields = {}, action, jobId = "" }) => {
        const baseTask = {
            jobType: "info",
            action,
            panel: "output",
            jobId,
            logLabel: action || label.toLowerCase(),
        };

        try {
            activateOutputPanel();
            applyInfoProgress(label, action, "pending");

            let trackedTask = baseTask;
            if (jobId === "") {
                const job = await createInfoJob(path.value.trim(), action === "bdinfo" ? "bdinfo" : "mediainfo", fields);
                applyInfoProgress(label, action, job.status, job.progress);
                trackedTask = {
                    ...baseTask,
                    jobId: job.jobId,
                };
            }

            persistActiveTask(trackedTask);
            const result = await waitForAsyncJob(fetchInfoJob, trackedTask.jobId, trackedTask.logLabel, (job) => {
                applyInfoProgress(label, action, job.status, job.progress);
            });

            clearPersistedActiveTask();
            setOutputText(result.output || "没有输出。");
        } catch (err) {
            clearPersistedActiveTask();
            logTaskError(baseTask.logLabel, err);

            if (err?.canceled) {
                activateOutputPanel();
                setOutputText(`${label} 任务已取消。`);
                showNotice(`${label} 任务已取消。`);
                return;
            }

            clearOutputState();
            hidePanels();
            showNotice(resolveTaskErrorMessage(err, `${label} 任务已失效，请重新发起。`));
        } finally {
            clearTaskProgress();
            setBusy(false);
        }
    };

    const runDownloadTask = async ({ jobId = "" } = {}) => {
        const baseTask = {
            jobType: "screenshot",
            action: "download-shots",
            panel: "output",
            jobId,
            logLabel: "screenshots download",
        };

        try {
            activateOutputPanel();
            applyDownloadProgress("pending");

            let trackedTask = baseTask;
            if (jobId === "") {
                const job = await createScreenshotJob(path.value.trim(), screenshotVariant.value, screenshotSubtitleMode.value, screenshotCount.value, "zip");
                applyDownloadProgress(job.status, job.progress);
                trackedTask = {
                    ...baseTask,
                    jobId: job.jobId,
                };
            }

            persistActiveTask(trackedTask);
            const result = await waitForAsyncJob(fetchScreenshotJob, trackedTask.jobId, trackedTask.logLabel, (job) => {
                applyDownloadProgress(job.status, job.progress);
            });

            if (typeof result.downloadURL !== "string" || result.downloadURL.trim() === "") {
                throw buildAsyncJobError({
                    error: "截图任务已完成，但未返回下载地址。",
                    logs: result.logs,
                    logEntries: result.logEntries,
                });
            }

            clearPersistedActiveTask();
            startPreparedDownload(new URL(result.downloadURL, window.location.origin).toString());
            setOutputText(jobId === "" ? "截图已生成。" : "截图已生成，正在恢复下载。");
        } catch (err) {
            clearPersistedActiveTask();
            logTaskError(baseTask.logLabel, err);

            if (err?.canceled) {
                activateOutputPanel();
                setOutputText("截图任务已取消。");
                showNotice("截图任务已取消。");
                return;
            }

            clearOutputState();
            hidePanels();
            showNotice(resolveTaskErrorMessage(err, "截图任务已失效，请重新发起。"));
        } finally {
            clearTaskProgress();
            setBusy(false);
        }
    };

    const runLinkTask = async ({ action, jobId = "" } = {}) => {
        const previousStatusText = linkStatusText.value;
        const isAppend = action === "append-links";
        const baseTask = {
            jobType: "screenshot",
            action,
            panel: "links",
            jobId,
            logLabel: "screenshots upload",
        };

        try {
            activateImageLinksPanel(!isAppend);
            applyLinkProgress(action, "pending");

            let trackedTask = baseTask;
            if (jobId === "") {
                const job = await createScreenshotJob(path.value.trim(), screenshotVariant.value, screenshotSubtitleMode.value, screenshotCount.value, "links");
                applyLinkProgress(action, job.status, job.progress);
                trackedTask = {
                    ...baseTask,
                    jobId: job.jobId,
                };
            }

            persistActiveTask(trackedTask);
            const result = await waitForAsyncJob(fetchScreenshotJob, trackedTask.jobId, trackedTask.logLabel, (job) => {
                applyLinkProgress(action, job.status, job.progress);
            });

            clearPersistedActiveTask();
            applyLinkResult(action, result.output || "");
        } catch (err) {
            clearPersistedActiveTask();
            logTaskError(baseTask.logLabel, err);

            if (err?.canceled) {
                handleCanceledLinkTask(action, previousStatusText);
                showNotice(action === "append-links" ? "附加图床任务已取消。" : "图床任务已取消。");
                return;
            }

            if (action === "append-links") {
                setLinkStatusText(previousStatusText);
                showNotice(resolveTaskErrorMessage(err, "附加图床任务已失效，请重新发起。"));
                return;
            }

            clearLinkState();
            hidePanels();
            showNotice(resolveTaskErrorMessage(err, "图床任务已失效，请重新发起。"));
        } finally {
            clearTaskProgress();
            setBusy(false);
        }
    };

    const runInfo = async (url, label, fields = {}, action = "") => {
        if (!hasInput.value) {
            showNotice("请先选择媒体路径。");
            return;
        }
        void url;
        await runInfoTask({ label, fields, action });
    };

    const downloadShots = async () => {
        if (!hasInput.value) {
            showNotice("请先选择媒体路径。");
            return;
        }
        await runDownloadTask();
    };

    const outputShotLinks = async () => {
        if (!hasInput.value) {
            showNotice("请先选择媒体路径。");
            return;
        }
        await runLinkTask({ action: "output-links" });
    };

    const appendShotLinks = async () => {
        if (!hasInput.value) {
            showNotice("请先选择媒体路径。");
            return;
        }
        await runLinkTask({ action: "append-links" });
    };

    const resumePersistedTask = async () => {
        const persistedTask = loadActiveTask();
        if (!persistedTask) {
            return;
        }

        switch (persistedTask.action) {
            case "mediainfo":
                await runInfoTask({ label: "MediaInfo", action: "mediainfo", jobId: persistedTask.jobId });
                return;
            case "bdinfo":
                await runInfoTask({ label: "BDInfo", action: "bdinfo", fields: {}, jobId: persistedTask.jobId });
                return;
            case "download-shots":
                await runDownloadTask({ jobId: persistedTask.jobId });
                return;
            case "output-links":
                await runLinkTask({ action: "output-links", jobId: persistedTask.jobId });
                return;
            case "append-links":
                await runLinkTask({ action: "append-links", jobId: persistedTask.jobId });
                return;
            default:
                clearPersistedActiveTask();
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

    void resumePersistedTask();

    return {
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
    };

    function applyPersistedTaskProgress(task, status, progress) {
        switch (task.action) {
            case "mediainfo":
                applyInfoProgress("MediaInfo", "mediainfo", status, progress);
                return;
            case "bdinfo":
                applyInfoProgress("BDInfo", "bdinfo", status, progress);
                return;
            case "download-shots":
                applyDownloadProgress(status, progress);
                return;
            case "output-links":
            case "append-links":
                applyLinkProgress(task.action, status, progress);
                return;
            default:
        }
    }

    function applyLinkResult(action, output) {
        const links = extractDirectLinks(output);
        if (action === "append-links") {
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
            return;
        }

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
    }

    function handleCanceledLinkTask(action, previousStatusText) {
        if (action === "append-links") {
            if (linkItems.value.length > 0) {
                setLinkStatusText(`已取消追加图床任务，当前共 ${linkItems.value.length} 条。`);
                return;
            }
            activateImageLinksPanel(false);
            setLinkStatusText("已取消追加图床任务。");
            return;
        }

        activateImageLinksPanel(true);
        setLinkStatusText("已取消图床任务。");
    }
}

function normalizeTargetPath(value) {
    return typeof value === "string" ? value.trim() : "";
}

function buildAsyncJobError(job = {}) {
    const canceled = job?.status === "canceled";
    const error = new Error(job?.error || (canceled ? "任务已取消。" : "任务失败。"));
    error.canceled = canceled;
    if (typeof job?.logs === "string" && job.logs.trim() !== "") {
        error.logs = job.logs;
    }
    if (Array.isArray(job?.logEntries) && job.logEntries.length > 0) {
        error.logEntries = job.logEntries;
    }
    return error;
}

function sleep(ms) {
    return new Promise((resolve) => {
        setTimeout(resolve, ms);
    });
}

function buildInfoProgressMessage(label, status) {
    switch (status) {
        case "canceled":
            return `${label} 任务已取消。`;
        case "succeeded":
            return `${label} 任务已完成。`;
        case "canceling":
            return `${label} 任务取消中...`;
        case "running":
            return `${label} 任务已提交，正在后台生成...`;
        case "pending":
        default:
            return `${label} 任务已提交，等待执行...`;
    }
}

function buildDownloadProgressMessage(status) {
    switch (status) {
        case "canceled":
            return "截图任务已取消。";
        case "succeeded":
            return "截图已生成。";
        case "canceling":
            return "截图任务取消中...";
        case "running":
            return "正在生成截图...";
        case "pending":
        default:
            return "截图任务已提交，等待执行...";
    }
}

function buildLinkProgressMessage(action, status) {
    switch (status) {
        case "canceled":
            return action === "append-links" ? "附加图床任务已取消。" : "图床任务已取消。";
        case "succeeded":
            return "图床任务已完成。";
        case "canceling":
            return action === "append-links" ? "附加图床任务取消中..." : "图床任务取消中...";
        case "running":
            return "正在生成截图并上传...";
        case "pending":
        default:
            return "截图任务已提交，等待执行...";
    }
}

function resolveTaskProgress(progress, fallback) {
    return normalizeTaskProgressPayload(progress) || normalizeTaskProgressPayload(fallback);
}

function buildInfoFallbackProgress(label, status) {
    if (label === "BDInfo") {
        return buildFallbackTaskProgress(status, "生成 BDInfo", "正在生成 BDInfo 报告。");
    }
    return buildFallbackTaskProgress(status, "分析媒体信息", "正在分析媒体信息。");
}

function buildDownloadFallbackProgress(status) {
    return buildScreenshotFallbackTaskProgress(status, "生成截图", "正在生成截图文件。");
}

function buildLinkFallbackProgress(status) {
    return buildScreenshotFallbackTaskProgress(status, "上传图床", "正在生成截图并上传图床。");
}

function buildFallbackTaskProgress(status, runningStage, runningDetail) {
    switch (status) {
        case "succeeded":
            return { percent: 100, stage: "已完成", detail: "任务执行完成。", indeterminate: false };
        case "failed":
            return { percent: 100, stage: "已失败", detail: "任务执行失败。", indeterminate: false };
        case "canceled":
            return { percent: 100, stage: "已取消", detail: "任务已取消。", indeterminate: false };
        case "canceling":
            return { percent: 94, stage: "正在停止", detail: "任务取消中...", indeterminate: true };
        case "running":
            return { percent: 12, stage: runningStage, detail: runningDetail, indeterminate: true };
        case "pending":
        default:
            return { percent: 6, stage: "等待开始", detail: "任务已提交，等待执行。", indeterminate: true };
    }
}

function buildScreenshotFallbackTaskProgress(status, runningStage, runningDetail) {
    switch (status) {
        case "succeeded":
            return { percent: 100, stage: "已完成", detail: "任务执行完成。", indeterminate: false };
        case "failed":
            return { percent: 100, stage: "已失败", detail: "任务执行失败。", indeterminate: false };
        case "canceled":
            return { percent: 100, stage: "已取消", detail: "任务已取消。", indeterminate: false };
        case "canceling":
            return { percent: 94, stage: "正在停止", detail: "任务取消中...", indeterminate: true };
        case "running":
            return { percent: 0, stage: runningStage, detail: runningDetail, indeterminate: true };
        case "pending":
        default:
            return { percent: 0, stage: "等待开始", detail: "任务已提交，等待执行。", indeterminate: true };
    }
}

function normalizeTaskProgressPayload(progress) {
    if (!progress || typeof progress !== "object") {
        return null;
    }

    const percent = Number.isFinite(progress.percent) ? Math.min(100, Math.max(0, Number(progress.percent))) : 0;
    const stage = typeof progress.stage === "string" ? progress.stage : "";
    const detail = typeof progress.detail === "string" ? progress.detail : "";
    const current = Number.isFinite(progress.current) && progress.current > 0 ? Math.round(progress.current) : 0;
    const total = Number.isFinite(progress.total) && progress.total > 0 ? Math.round(progress.total) : 0;
    const indeterminate = progress.indeterminate === true;

    if (percent === 0 && stage === "" && detail === "" && current === 0 && total === 0 && !indeterminate) {
        return null;
    }

    return {
        percent,
        stage,
        detail,
        current,
        total,
        indeterminate,
    };
}

function resolveTaskErrorMessage(err, notFoundMessage) {
    if (isMissingTaskError(err)) {
        return notFoundMessage;
    }
    return err?.message || "请求失败。";
}

function isMissingTaskError(err) {
    const message = typeof err?.message === "string" ? err.message.trim().toLowerCase() : "";
    return message === "job not found" || message.includes("not found");
}

function formatConsoleLogLines(logs) {
    const normalized = `${logs}`.replaceAll("\r\n", "\n").replaceAll("\r", "\n");
    return normalized
        .split("\n")
        .filter((line) => line.trim() !== "" && !isInternalProgressLog(line))
        .map((line) => (hasTimePrefix(line) ? line : `[${formatConsoleTime(new Date())}] ${line}`));
}

function formatStructuredConsoleLogLine(entry) {
    const message = typeof entry?.message === "string" ? entry.message : "";
    const timestamp = formatStructuredConsoleTimestamp(entry?.timestamp);
    if (timestamp === "") {
        return message;
    }
    if (message === "") {
        return `[${timestamp}]`;
    }
    return `[${timestamp}] ${message}`;
}

function hasTimePrefix(line) {
    return /^\[\d{2}:\d{2}:\d{2}\]\s/.test(line);
}

function isInternalProgressLog(line) {
    return typeof line === "string" && line.trim().startsWith("[进度]");
}

function formatConsoleTime(value) {
    return new Intl.DateTimeFormat("zh-CN", {
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        hour12: false,
    }).format(value);
}

function formatStructuredConsoleTimestamp(value) {
    if (typeof value !== "string") {
        return "";
    }
    const trimmed = value.trim();
    if (trimmed === "") {
        return "";
    }
    if (/^\d{2}:\d{2}:\d{2}$/.test(trimmed)) {
        return trimmed;
    }
    const parsed = new Date(trimmed);
    if (Number.isNaN(parsed.getTime())) {
        return trimmed;
    }
    return formatConsoleTime(parsed);
}
