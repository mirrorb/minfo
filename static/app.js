const fileInput = document.getElementById("file");
const pathInput = document.getElementById("path");
const pathList = document.getElementById("path-list");
const output = document.getElementById("output");
const btnMediaInfo = document.getElementById("btn-mediainfo");
const btnBdInfo = document.getElementById("btn-bdinfo");
const btnShots = document.getElementById("btn-shots");
const btnClear = document.getElementById("btn-clear");

const buttons = [btnMediaInfo, btnBdInfo, btnShots];
let suggestTimer = null;
let suggestController = null;
let lastSuggest = null;

function hasInput() {
  return (fileInput.files && fileInput.files.length > 0) || pathInput.value.trim() !== "";
}

function setBusy(isBusy, label) {
  buttons.forEach((btn) => {
    btn.disabled = isBusy;
  });
  if (label) {
    output.textContent = label;
  }
}

function appendOutput(text) {
  output.textContent = text;
}

function errorOutput(message) {
  output.textContent = `Error: ${message}`;
}

function scheduleSuggest() {
  if (!pathInput || !pathList) {
    return;
  }
  clearTimeout(suggestTimer);
  suggestTimer = setTimeout(() => {
    suggestPaths(pathInput.value.trim());
  }, 200);
}

async function suggestPaths(prefix) {
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
    pathList.innerHTML = "";
    data.items.forEach((item) => {
      const option = document.createElement("option");
      option.value = item;
      pathList.appendChild(option);
    });
  } catch (err) {
    if (err.name !== "AbortError") {
      return;
    }
  }
}

async function postForm(url) {
  const form = new FormData();
  if (fileInput.files && fileInput.files.length > 0) {
    const file = fileInput.files[0];
    form.append("file", file, file.name);
  }
  const path = pathInput.value.trim();
  if (path !== "") {
    form.append("path", path);
  }
  return fetch(url, { method: "POST", body: form });
}

async function runInfo(url, label) {
  if (!hasInput()) {
    errorOutput("Select a file or enter a server path first.");
    return;
  }
  try {
    setBusy(true, `${label} running...`);
    const res = await postForm(url);
    const data = await res.json();
    if (!res.ok || !data.ok) {
      throw new Error(data.error || "Request failed.");
    }
    appendOutput(data.output || "No output.");
  } catch (err) {
    errorOutput(err.message || "Request failed.");
  } finally {
    setBusy(false);
  }
}

async function downloadShots() {
  if (!hasInput()) {
    errorOutput("Select a file or enter a server path first.");
    return;
  }
  try {
    setBusy(true, "Generating screenshots...");
    const res = await postForm("/api/screenshots");
    const contentType = res.headers.get("content-type") || "";
    if (!res.ok || !contentType.includes("application/zip")) {
      const data = await res.json();
      throw new Error(data.error || "Screenshot request failed.");
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
    appendOutput("Screenshots downloaded as screenshots.zip.");
  } catch (err) {
    errorOutput(err.message || "Screenshot request failed.");
  } finally {
    setBusy(false);
  }
}

btnMediaInfo.addEventListener("click", () => runInfo("/api/mediainfo", "MediaInfo"));
btnBdInfo.addEventListener("click", () => runInfo("/api/bdinfo", "BDInfo"));
btnShots.addEventListener("click", downloadShots);
btnClear.addEventListener("click", () => appendOutput("Ready."));
pathInput.addEventListener("input", scheduleSuggest);
pathInput.addEventListener("focus", scheduleSuggest);
