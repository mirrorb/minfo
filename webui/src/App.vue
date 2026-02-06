<template>
  <div class="grain"></div>
  <main class="shell">
    <header class="hero">
      <div>
        <p class="kicker">本地媒体检测</p>
        <h1>minfo</h1>
        <p class="lead">一键生成 MediaInfo / BDInfo，并下载 4 张截图。</p>
      </div>
    </header>

    <section class="panel">
      <div class="field">
        <label for="file">媒体文件或光盘镜像</label>
        <input id="file" type="file" @change="onFileChange" />
        <p class="hint">大文件建议使用服务器路径。</p>
      </div>
      <div class="field">
        <label for="path">服务器路径（可选）</label>
        <input
          id="path"
          type="text"
          list="path-list"
          placeholder="/media/movie.mkv"
          v-model="path"
          @input="scheduleSuggest"
          @focus="scheduleSuggest"
        />
        <datalist id="path-list">
          <option v-for="item in suggestions" :key="item" :value="item"></option>
        </datalist>
        <p class="hint">输入自动补全（MEDIA_ROOT，默认 /media）。</p>
      </div>
      <div class="actions">
        <button :disabled="busy" @click="runInfo('/api/mediainfo', 'MediaInfo')">生成 MediaInfo</button>
        <button :disabled="busy" @click="runInfo('/api/bdinfo', 'BDInfo')">生成 BDInfo</button>
        <button :disabled="busy" @click="downloadShots">下载 4 张截图</button>
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
      <p>上传用于小文件；目录或超大文件请使用服务器路径。</p>
    </footer>
  </main>
</template>

<script>
export default {
  data() {
    return {
      file: null,
      path: "",
      output: "就绪。",
      busy: false,
      suggestions: [],
      copyLabel: "复制",
      suggestTimer: null,
      suggestController: null,
      lastSuggest: null,
    };
  },
  methods: {
    hasInput() {
      return !!this.file || this.path.trim() !== "";
    },
    onFileChange(event) {
      const files = event.target.files;
      this.file = files && files.length > 0 ? files[0] : null;
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
      if (this.file) {
        form.append("file", this.file, this.file.name);
      }
      const path = this.path.trim();
      if (path !== "") {
        form.append("path", path);
      }
      return fetch(url, { method: "POST", body: form });
    },
    async runInfo(url, label) {
      if (!this.hasInput()) {
        this.errorOutput("请先选择文件或填写服务器路径。");
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
        this.errorOutput("请先选择文件或填写服务器路径。");
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
