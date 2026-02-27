<template>
    <div class="grain"></div>
    <main class="shell">
        <header class="hero">
            <div>
                <p class="kicker">本地媒体检测</p>
                <h1>minfo</h1>
                <p class="lead">一键生成 MediaInfo / BDInfo，并下载 8 张截图。</p>
            </div>
        </header>

        <section class="panel">
            <div class="field">
                <label for="path">媒体路径</label>
                <div class="path-picker" @keydown="handleKeydown">
                    <div class="path-input">
                        <span class="path-icon">🔎</span>
                        <input
                            id="path"
                            type="text"
                            placeholder="/media/movie.mkv"
                            v-model="path"
                            @input="handleInput"
                            @focus="handleFocus"
                            @blur="handleBlur"
                            autocomplete="off"
                        />
                        <button
                            v-if="path.trim() !== ''"
                            type="button"
                            class="path-clear"
                            @click="clearPath"
                            :disabled="busy"
                            aria-label="清空路径"
                        >
                            ×
                        </button>
                    </div>
                    <div class="path-hint">
                        <span>支持模糊搜索与路径补全（MEDIA_ROOT，默认 /media）。</span>
                        <span class="path-meta" v-if="filteredSuggestions.length > 0">
                            匹配 {{ filteredSuggestions.length }} 条
                        </span>
                    </div>
                    <div class="path-suggestions" v-if="showSuggestions">
                        <div
                            v-for="(item, index) in filteredSuggestions"
                            :key="item.value"
                            class="suggestion"
                            :class="{ active: index === activeIndex }"
                            @mousedown.prevent="selectSuggestion(item.value)"
                        >
                            <span v-for="(segment, segIndex) in item.segments" :key="segIndex" :class="{ match: segment.match }">
                                {{ segment.text }}
                            </span>
                        </div>
                        <div v-if="filteredSuggestions.length === 0" class="suggestion empty">
                            没有匹配的路径
                        </div>
                    </div>
                </div>
            </div>
            <div class="actions">
                <button :disabled="busy" @click="runInfo('/api/mediainfo', 'MediaInfo')">生成 MediaInfo</button>
                <button :disabled="busy" @click="runInfo('/api/bdinfo', 'BDInfo')">生成 BDInfo</button>
                <button :disabled="busy" @click="downloadShots">下载 8 张截图</button>
            </div>
        </section>

        <section class="panel output">
            <div class="output-header">
                <h2>输出</h2>
                <div class="output-actions">
                    <button class="ghost" @click="copyOutput">{{ copyLabel }}</button>
                    <button class="ghost" :disabled="busy" @click="clearOutput">清空</button>
                </div>
            </div>
            <pre>{{ output }}</pre>
        </section>

        <footer class="footer">
            <p>请输入服务器路径，支持自动补全。</p>
        </footer>
    </main>
</template>

<script setup>
import { computed, onBeforeUnmount, ref } from "vue";

const path = ref("");
const output = ref("就绪。");
const busy = ref(false);
const suggestions = ref([]);
const copyLabel = ref("复制");
const showSuggestions = ref(false);
const activeIndex = ref(-1);

let suggestTimer = null;
let suggestController = null;
let lastSuggest = null;

const filteredSuggestions = computed(() => {
    const query = path.value.trim();
    if (query === "") {
        return suggestions.value.slice(0, 12).map((value) => ({
            value,
            score: 0,
            segments: [{ text: value, match: false }],
        }));
    }

    const items = [];
    for (const value of suggestions.value) {
        const positions = matchPositions(value, query);
        if (!positions) {
            continue;
        }
        items.push({
            value,
            score: scoreMatch(positions, value),
            segments: buildSegments(value, positions),
        });
    }

    items.sort((a, b) => {
        if (b.score !== a.score) {
            return b.score - a.score;
        }
        return a.value.length - b.value.length;
    });
    return items.slice(0, 12);
});

const hasInput = () => path.value.trim() !== "";

const handleInput = () => {
    activeIndex.value = -1;
    showSuggestions.value = true;
    scheduleSuggest();
};

const handleFocus = () => {
    showSuggestions.value = true;
    scheduleSuggest();
};

const handleBlur = () => {
    setTimeout(() => {
        showSuggestions.value = false;
        activeIndex.value = -1;
    }, 120);
};

const handleKeydown = (event) => {
    if (!showSuggestions.value) {
        return;
    }
    const list = filteredSuggestions.value;
    if (event.key === "ArrowDown") {
        event.preventDefault();
        if (list.length === 0) {
            return;
        }
        activeIndex.value = (activeIndex.value + 1) % list.length;
    } else if (event.key === "ArrowUp") {
        event.preventDefault();
        if (list.length === 0) {
            return;
        }
        activeIndex.value = (activeIndex.value - 1 + list.length) % list.length;
    } else if (event.key === "Enter") {
        if (activeIndex.value >= 0 && activeIndex.value < list.length) {
            event.preventDefault();
            selectSuggestion(list[activeIndex.value].value);
        }
    } else if (event.key === "Escape") {
        showSuggestions.value = false;
        activeIndex.value = -1;
    }
};

const clearPath = () => {
    path.value = "";
    showSuggestions.value = false;
    activeIndex.value = -1;
    suggestions.value = [];
};

const selectSuggestion = (value) => {
    path.value = value;
    showSuggestions.value = false;
    activeIndex.value = -1;
};

const matchPositions = (value, query) => {
    const v = value.toLowerCase();
    const q = query.toLowerCase();
    const positions = [];
    let index = 0;
    for (const ch of q) {
        const found = v.indexOf(ch, index);
        if (found === -1) {
            return null;
        }
        positions.push(found);
        index = found + 1;
    }
    return positions;
};

const scoreMatch = (positions, value) => {
    if (!positions || positions.length === 0) {
        return 0;
    }
    const length = value.length;
    let score = positions.length;
    for (let i = 1; i < positions.length; i++) {
        if (positions[i] === positions[i - 1] + 1) {
            score += 3;
        } else {
            score += 1;
        }
    }
    for (const pos of positions) {
        if (pos === 0) {
            score += 3;
        } else {
            const prev = pos - 1;
            if (prev >= 0 && prev < length && isBoundary(value[prev])) {
                score += 2;
            }
        }
    }
    score += Math.max(0, 30 - positions[0]);
    return score;
};

const isBoundary = (ch) => ch === "/" || ch === "\\" || ch === "_" || ch === "-" || ch === " ";

const buildSegments = (value, positions) => {
    const segments = [];
    const posSet = new Set(positions);
    let current = "";
    let currentMatch = posSet.has(0);
    for (let i = 0; i < value.length; i++) {
        const isMatch = posSet.has(i);
        if (isMatch !== currentMatch && current !== "") {
            segments.push({ text: current, match: currentMatch });
            current = "";
        }
        currentMatch = isMatch;
        current += value[i];
    }
    if (current !== "") {
        segments.push({ text: current, match: currentMatch });
    }
    return segments;
};

const setBusy = (isBusy, label) => {
    busy.value = isBusy;
    if (label) {
        output.value = label;
    }
};

const appendOutput = (text) => {
    output.value = text;
};

const errorOutput = (message) => {
    output.value = `错误：${message}`;
};

const scheduleSuggest = () => {
    if (suggestTimer) {
        clearTimeout(suggestTimer);
    }
    suggestTimer = setTimeout(() => {
        suggestPaths(path.value.trim());
    }, 200);
};

const suggestPaths = async (prefix) => {
    if (prefix === lastSuggest) {
        return;
    }
    lastSuggest = prefix;
    if (suggestController) {
        suggestController.abort();
    }
    suggestController = new AbortController();

    try {
        const url = new URL("/api/path", window.location.origin);
        if (prefix !== "") {
            url.searchParams.set("prefix", prefix);
        }
        const res = await fetch(url.toString(), { signal: suggestController.signal });
        if (!res.ok) {
            return;
        }
        const data = await res.json();
        if (!data.ok || !Array.isArray(data.items)) {
            return;
        }
        suggestions.value = data.items;
    } catch (err) {
        if (err && err.name === "AbortError") {
            return;
        }
    }
};

const postForm = async (url) => {
    const form = new FormData();
    const value = path.value.trim();
    if (value !== "") {
        form.append("path", value);
    }
    return fetch(url, { method: "POST", body: form });
};

const runInfo = async (url, label) => {
    if (!hasInput()) {
        errorOutput("请先填写媒体路径。");
        return;
    }
    try {
        setBusy(true, `${label} 生成中...`);
        const res = await postForm(url);
        let data = {};
        try {
            data = await res.json();
        } catch (err) {
            data = {};
        }
        if (!res.ok || !data.ok) {
            throw new Error(data.error || "请求失败。");
        }
        appendOutput(data.output || "没有输出。");
    } catch (err) {
        errorOutput(err && err.message ? err.message : "请求失败。");
    } finally {
        setBusy(false);
    }
};

const downloadShots = async () => {
    if (!hasInput()) {
        errorOutput("请先填写媒体路径。");
        return;
    }
    try {
        setBusy(true, "正在生成截图...");
        const res = await postForm("/api/screenshots");
        const contentType = res.headers.get("content-type") || "";
        if (!res.ok || !contentType.includes("application/zip")) {
            let data = {};
            try {
                data = await res.json();
            } catch (err) {
                data = {};
            }
            throw new Error(data.error || "截图请求失败。");
        }
        const blob = await res.blob();
        const url = window.URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = "screenshots.zip";
        document.body.appendChild(a);
        a.click();
        a.remove();
        window.URL.revokeObjectURL(url);
        appendOutput("截图已下载为 screenshots.zip。");
    } catch (err) {
        errorOutput(err && err.message ? err.message : "截图请求失败。");
    } finally {
        setBusy(false);
    }
};

const clearOutput = () => {
    if (busy.value) {
        return;
    }
    appendOutput("就绪。");
};

const copyOutput = async () => {
    const text = output.value || "";
    if (text.trim() === "") {
        errorOutput("没有可复制的内容。");
        return;
    }

    try {
        await navigator.clipboard.writeText(text);
    } catch (err) {
        const textarea = document.createElement("textarea");
        textarea.value = text;
        textarea.setAttribute("readonly", "");
        textarea.style.position = "absolute";
        textarea.style.left = "-9999px";
        document.body.appendChild(textarea);
        textarea.select();
        try {
            document.execCommand("copy");
        } finally {
            textarea.remove();
        }
    }

    const original = copyLabel.value;
    copyLabel.value = "已复制";
    setTimeout(() => {
        copyLabel.value = original;
    }, 1200);
};

onBeforeUnmount(() => {
    if (suggestTimer) {
        clearTimeout(suggestTimer);
    }
    if (suggestController) {
        suggestController.abort();
    }
});
</script>
