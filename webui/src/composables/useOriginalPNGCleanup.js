import { onScopeDispose, watch } from "vue";
import { deleteOriginalPNG } from "../api/media.js";

// 缓存跟随图床窗口和条目存活；移除入口时仅清理它对应的原图。
export function useOriginalPNGCleanup(items) {
    let retainedURLs = new Set();

    const release = (urls) => {
        for (const url of urls) {
            void deleteOriginalPNG(url).catch((err) => console.warn("清理无损原图失败：", err));
        }
    };
    const releaseAll = () => {
        release(retainedURLs);
        retainedURLs.clear();
    };

    watch(
        () => items.value.map((item) => item.originalDownloadURL).filter(Boolean),
        (urls) => {
            const nextURLs = new Set(urls);
            release([...retainedURLs].filter((url) => !nextURLs.has(url)));
            retainedURLs = nextURLs;
        },
        { immediate: true, flush: "sync" },
    );

    if (typeof window !== "undefined") {
        window.addEventListener("pagehide", releaseAll);
    }
    onScopeDispose(() => {
        releaseAll();
        if (typeof window !== "undefined") {
            window.removeEventListener("pagehide", releaseAll);
        }
    });
}
