import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { fetchDirectory } from "../api/media";
import {
    buildEntries,
    buildVirtualISOPath,
    canNavigateUp as computeCanNavigateUp,
    cleanPath,
    filterEntries,
    getParentDirectory,
    normalizeComparePath,
    withTrailingSeparator,
} from "../utils/path-browser";

export function usePathBrowser(options = {}) {
    const initialPath = typeof options.initialPath === "string" ? options.initialPath : "";
    const initialBrowserDir = typeof options.initialBrowserDir === "string" ? options.initialBrowserDir : "";
    const path = ref(initialPath);
    const browserRoots = ref([]);
    const browserRoot = ref("");
    const browserDir = ref("");
    const browserEntries = ref([]);
    const browserLoading = ref(false);
    const browserError = ref("");
    const searchKeyword = ref("");
    let browserController = null;

    const hasInput = computed(() => path.value.trim() !== "");
    const canNavigateUp = computed(() => computeCanNavigateUp(browserDir.value, browserRoot.value, browserRoots.value));
    const filteredEntries = computed(() => filterEntries(browserEntries.value, searchKeyword.value));

    const loadDirectory = async (dir) => {
        browserLoading.value = true;
        browserError.value = "";
        try {
            if (browserController) {
                browserController.abort();
            }
            browserController = new AbortController();

            const prefix = dir ? withTrailingSeparator(dir) : "";
            const data = await fetchDirectory(prefix, browserController.signal);
            browserRoots.value = Array.isArray(data.roots) ? data.roots.map(cleanPath).filter(Boolean) : [];
            browserRoot.value = typeof data.root === "string" ? cleanPath(data.root) : "";
            browserEntries.value = buildEntries(data.items);
            browserDir.value = dir && dir !== "" ? cleanPath(dir) : browserRoot.value || "";
            searchKeyword.value = "";
        } catch (err) {
            if (err?.name === "AbortError") {
                return;
            }
            browserError.value = err?.message || "读取路径失败。";
            browserEntries.value = [];
        } finally {
            browserLoading.value = false;
        }
    };

    const navigateUp = async () => {
        if (!browserDir.value) {
            await loadDirectory("");
            return;
        }

        let parent = getParentDirectory(browserDir.value, browserRoot.value);
        const root = normalizeComparePath(browserRoot.value);
        const current = normalizeComparePath(browserDir.value);

        if (root !== "" && current === root && browserRoots.value.length > 1) {
            await loadDirectory("");
            return;
        }
        if (root !== "" && normalizeComparePath(parent).length < root.length) {
            parent = browserRoot.value;
        }
        if (parent === browserDir.value && browserRoot.value && browserDir.value !== browserRoot.value) {
            parent = browserRoot.value;
        }

        await loadDirectory(parent || "");
    };

    const refreshBrowser = async () => {
        await loadDirectory(browserDir.value || "");
    };

    const navigateToDirectory = async (dir) => {
        if (browserLoading.value) {
            return;
        }
        await loadDirectory(typeof dir === "string" ? dir : "");
    };

    const handleEntryEnter = async (entry) => {
        if (browserLoading.value) {
            return;
        }
        if (entry.isDir) {
            await loadDirectory(entry.path);
            return;
        }
        if (entry.isISO) {
            await loadDirectory(buildVirtualISOPath(entry.path));
            return;
        }
        path.value = entry.path;
    };

    const handleEntryDoubleClick = async (entry) => {
        if (entry.isDir) {
            await handleEntryEnter(entry);
            return;
        }
        path.value = entry.path;
    };

    onMounted(async () => {
        const preferredDirectory = initialBrowserDir.trim();
        await loadDirectory(preferredDirectory);

        if (preferredDirectory !== "" && browserError.value !== "") {
            await loadDirectory("");
        }
    });

    onBeforeUnmount(() => {
        if (browserController) {
            browserController.abort();
        }
    });

    return {
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
    };
}
