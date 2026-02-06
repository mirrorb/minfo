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

<script>
export default {
    data() {
        return {
            path: "",
            output: "就绪。",
            busy: false,
            suggestions: [],
            copyLabel: "复制",
            suggestTimer: null,
            suggestController: null,
            lastSuggest: null,
            showSuggestions: false,
            activeIndex: -1,
        };
    },
    computed: {
        filteredSuggestions() {
            const query = this.path.trim();
            if (query === "") {
                return this.suggestions.slice(0, 12).map((value) => ({
                    value,
                    score: 0,
                    segments: [{ text: value, match: false }],
                }));
            }

            const items = [];
            for (const value of this.suggestions) {
                const positions = this.matchPositions(value, query);
                if (!positions) {
                    continue;
                }
                items.push({
                    value,
                    score: this.scoreMatch(positions, value),
                    segments: this.buildSegments(value, positions),
                });
            }

            items.sort((a, b) => {
                if (b.score !== a.score) {
                    return b.score - a.score;
                }
                return a.value.length - b.value.length;
            });
            return items.slice(0, 12);
        },
    },
    methods: {
        hasInput() {
            return this.path.trim() !== "";
        },
        handleInput() {
            this.activeIndex = -1;
            this.showSuggestions = true;
            this.scheduleSuggest();
        },
        handleFocus() {
            this.showSuggestions = true;
            this.scheduleSuggest();
        },
        handleBlur() {
            setTimeout(() => {
                this.showSuggestions = false;
                this.activeIndex = -1;
            }, 120);
        },
        handleKeydown(event) {
            if (!this.showSuggestions) {
                return;
            }
            const list = this.filteredSuggestions;
            if (event.key === "ArrowDown") {
                event.preventDefault();
                if (list.length === 0) {
                    return;
                }
                this.activeIndex = (this.activeIndex + 1) % list.length;
            } else if (event.key === "ArrowUp") {
                event.preventDefault();
                if (list.length === 0) {
                    return;
                }
                this.activeIndex = (this.activeIndex - 1 + list.length) % list.length;
            } else if (event.key === "Enter") {
                if (this.activeIndex >= 0 && this.activeIndex < list.length) {
                    event.preventDefault();
                    this.selectSuggestion(list[this.activeIndex].value);
                }
            } else if (event.key === "Escape") {
                this.showSuggestions = false;
                this.activeIndex = -1;
            }
        },
        clearPath() {
            this.path = "";
            this.showSuggestions = false;
            this.activeIndex = -1;
            this.suggestions = [];
        },
        selectSuggestion(value) {
            this.path = value;
            this.showSuggestions = false;
            this.activeIndex = -1;
        },
        matchPositions(value, query) {
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
        },
        scoreMatch(positions, value) {
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
                    if (prev >= 0 && prev < length && this.isBoundary(value[prev])) {
                        score += 2;
                    }
                }
            }
            score += Math.max(0, 30 - positions[0]);
            return score;
        },
        isBoundary(ch) {
            return ch === "/" || ch === "\\" || ch === "_" || ch === "-" || ch === " ";
        },
        buildSegments(value, positions) {
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
        },
        setBusy(isBusy, label) {
            this.busy = isBusy;
            if (label) {
                this.output = label;
            }
        },
        appendOutput(text) {
            this.output = text;
        },
        errorOutput(message) {
            this.output = `错误：${message}`;
        },
        scheduleSuggest() {
            if (this.suggestTimer) {
                clearTimeout(this.suggestTimer);
            }
            this.suggestTimer = setTimeout(() => {
                this.suggestPaths(this.path.trim());
            }, 200);
        },
        async suggestPaths(prefix) {
            if (prefix === this.lastSuggest) {
                return;
            }
            this.lastSuggest = prefix;
            if (this.suggestController) {
                this.suggestController.abort();
            }
            this.suggestController = new AbortController();

            try {
                const url = new URL("/api/path", window.location.origin);
                if (prefix !== "") {
                    url.searchParams.set("prefix", prefix);
                }
                const res = await fetch(url.toString(), { signal: this.suggestController.signal });
                if (!res.ok) {
                    return;
                }
                const data = await res.json();
                if (!data.ok || !Array.isArray(data.items)) {
                    return;
                }
                this.suggestions = data.items;
            } catch (err) {
                if (err && err.name === "AbortError") {
                    return;
                }
            }
        },
        async postForm(url) {
            const form = new FormData();
            const path = this.path.trim();
            if (path !== "") {
                form.append("path", path);
            }
            return fetch(url, { method: "POST", body: form });
        },
        async runInfo(url, label) {
            if (!this.hasInput()) {
                this.errorOutput("请先填写媒体路径。");
                return;
            }
            try {
                this.setBusy(true, `${label} 生成中...`);
                const res = await this.postForm(url);
                let data = {};
                try {
                    data = await res.json();
                } catch (err) {
                    data = {};
                }
                if (!res.ok || !data.ok) {
                    throw new Error(data.error || "请求失败。");
                }
                this.appendOutput(data.output || "没有输出。");
            } catch (err) {
                this.errorOutput(err && err.message ? err.message : "请求失败。");
            } finally {
                this.setBusy(false);
            }
        },
        async downloadShots() {
            if (!this.hasInput()) {
                this.errorOutput("请先填写媒体路径。");
                return;
            }
            try {
                this.setBusy(true, "正在生成截图...");
                const res = await this.postForm("/api/screenshots");
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
                this.appendOutput("截图已下载为 screenshots.zip。");
            } catch (err) {
                this.errorOutput(err && err.message ? err.message : "截图请求失败。");
            } finally {
                this.setBusy(false);
            }
        },
        clearOutput() {
            if (this.busy) {
                return;
            }
            this.appendOutput("就绪。");
        },
        async copyOutput() {
            const text = this.output || "";
            if (text.trim() === "") {
                this.errorOutput("没有可复制的内容。");
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

            const original = this.copyLabel;
            this.copyLabel = "已复制";
            setTimeout(() => {
                this.copyLabel = original;
            }, 1200);
        },
    },
};
</script>
