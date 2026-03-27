import { computed, ref } from "vue";
import { requestInfo, requestScreenshotLinks, requestScreenshotZip } from "../api/media";
import { buildBBCodeText, buildCopyText, buildLinkText, copyText, extractDirectLinks, mergeOutputLinks, normalizeOutputLinks, saveBlob } from "../utils/output";

export function useMediaActions(path, screenshotVariant, hasInput, options = {}) {
    const outputText = ref(typeof options.initialOutputText === "string" ? options.initialOutputText : "");
    const linkItems = ref(normalizeOutputLinks(options.initialLinkItems));
    const busy = ref(false);
    const statusMessage = ref("");
    const linkStatusText = ref("");
    const copyOutputStatus = ref("");
    const copyLinksStatus = ref("");
    const copyBBCodeStatus = ref("");
    const copyOutputLabel = computed(() => copyOutputStatus.value || "复制输出");
    const copyLinksLabel = computed(() => copyLinksStatus.value || "复制链接");
    const copyBBCodeLabel = computed(() => copyBBCodeStatus.value || "复制 BBCode");

    const setBusy = (isBusy, label) => {
        busy.value = isBusy;
        statusMessage.value = isBusy ? label || "" : "";
    };

    const setOutputText = (text) => {
        outputText.value = typeof text === "string" ? text : "";
    };

    const setLinkStatusText = (text) => {
        linkStatusText.value = typeof text === "string" ? text : "";
    };

    const errorOutput = (message) => {
        setOutputText(`错误：${message}`);
    };

    const errorLinkOutput = (message) => {
        setLinkStatusText(`错误：${message}`);
    };

    const runInfo = async (url, label, fields = {}) => {
        if (!hasInput.value) {
            errorOutput("请先选择媒体路径。");
            return;
        }
        try {
            setBusy(true, `${label} 生成中...`);
            const data = await requestInfo(path.value.trim(), url, fields);
            setOutputText(data.output || "没有输出。");
        } catch (err) {
            errorOutput(err?.message || "请求失败。");
        } finally {
            setBusy(false);
        }
    };

    const downloadShots = async () => {
        if (!hasInput.value) {
            errorOutput("请先选择媒体路径。");
            return;
        }
        try {
            setBusy(true, "正在生成截图...");
            const blob = await requestScreenshotZip(path.value.trim(), screenshotVariant.value);
            saveBlob(blob, "screenshots.zip");
            setOutputText("截图已下载为 screenshots.zip。");
        } catch (err) {
            errorOutput(err?.message || "截图请求失败。");
        } finally {
            setBusy(false);
        }
    };

    const outputShotLinks = async () => {
        if (!hasInput.value) {
            errorLinkOutput("请先选择媒体路径。");
            return;
        }
        try {
            setBusy(true);
            linkItems.value = [];
            setLinkStatusText("正在生成截图并上传...");
            const data = await requestScreenshotLinks(path.value.trim(), screenshotVariant.value);
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
            errorLinkOutput(err?.message || "图床链接请求失败。");
        } finally {
            setBusy(false);
        }
    };

    const appendShotLinks = async () => {
        if (!hasInput.value) {
            errorLinkOutput("请先选择媒体路径。");
            return;
        }
        try {
            setBusy(true);
            setLinkStatusText("正在生成截图并上传...");
            const data = await requestScreenshotLinks(path.value.trim(), screenshotVariant.value);
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
            errorLinkOutput(err?.message || "图床链接请求失败。");
        } finally {
            setBusy(false);
        }
    };

    const clearOutputText = () => {
        if (busy.value) {
            return;
        }
        setOutputText("");
    };

    const clearLinkItems = () => {
        if (busy.value) {
            return;
        }
        linkItems.value = [];
        setLinkStatusText("");
    };

    const copyOutputText = async () => {
        const text = buildCopyText(outputText.value, []);
        if (text.trim() === "") {
            errorOutput("没有可复制的内容。");
            return;
        }

        try {
            await copyText(text);
            copyOutputStatus.value = "已复制";
            setTimeout(() => {
                copyOutputStatus.value = "";
            }, 1200);
        } catch (err) {
            errorOutput(err?.message || "复制失败。");
        }
    };

    const copyLinks = async () => {
        const text = buildLinkText(linkItems.value);
        if (text.trim() === "") {
            errorLinkOutput("没有可复制的链接。");
            return;
        }

        try {
            await copyText(text);
            copyLinksStatus.value = "已复制";
            setTimeout(() => {
                copyLinksStatus.value = "";
            }, 1200);
        } catch (err) {
            errorLinkOutput(err?.message || "复制链接失败。");
        }
    };

    const copyBBCode = async () => {
        const text = buildBBCodeText(linkItems.value);
        if (text.trim() === "") {
            errorLinkOutput("没有可复制的 BBCode。");
            return;
        }

        try {
            await copyText(text);
            copyBBCodeStatus.value = "已复制";
            setTimeout(() => {
                copyBBCodeStatus.value = "";
            }, 1200);
        } catch (err) {
            errorLinkOutput(err?.message || "复制 BBCode 失败。");
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
        setLinkStatusText(nextItems.length > 0 ? `已移除 1 条图床链接，当前共 ${nextItems.length} 条。` : "已移除最后 1 条图床链接。");
    };

    return {
        outputText,
        linkItems,
        busy,
        linkStatusText,
        copyOutputLabel,
        copyLinksLabel,
        copyBBCodeLabel,
        statusMessage,
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
