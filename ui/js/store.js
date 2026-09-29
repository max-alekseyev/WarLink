// ui/js/store.js - Frontend SWR Cache & State Store for WarLink v2.1.9

class UIStoreClass {
    constructor() {
        this._cache = new Map();
        this._imageCache = new Map();
        this._inflight = new Map();
    }

    get(key) {
        if (!this._cache.has(key)) return null;
        return this._cache.get(key).data;
    }

    set(key, data, etag = null) {
        const hash = this.fastHash(data);
        this._cache.set(key, {
            data,
            hash,
            etag,
            timestamp: Date.now()
        });
    }

    has(key) {
        return this._cache.has(key);
    }

    invalidate(key) {
        this._cache.delete(key);
    }

    // 32-bit FNV-1a fast hashing algorithm
    fastHash(str) {
        if (typeof str !== 'string') {
            str = (str === null || str === undefined) ? '' : JSON.stringify(str);
        }
        let hash = 0x811c9dc5;
        for (let i = 0; i < str.length; i++) {
            hash ^= str.charCodeAt(i);
            hash = Math.imul(hash, 0x01000193);
        }
        return (hash >>> 0).toString(16);
    }

    async loadDecodedImage(url) {
        if (!url) return null;
        if (this._imageCache.has(url)) {
            return this._imageCache.get(url);
        }
        try {
            const img = new Image();
            img.src = url;
            if (typeof img.decode === 'function') {
                await img.decode();
            }
            this._imageCache.set(url, img);
            return img;
        } catch (e) {
            return null;
        }
    }

    async requestSWR(key, fetcher, renderFn, showSkeletonFn) {
        const hasCached = this.has(key);

        if (hasCached) {
            const cached = this._cache.get(key);
            try {
                if (typeof renderFn === 'function') {
                    renderFn(cached.data, false);
                }
            } catch (err) {
                console.error(`UIStore SWR cached render error (${key}):`, err);
            }

            // In background: asynchronous fetcher, compare hash to prevent redundant DOM mutations
            (async () => {
                try {
                    let promise = this._inflight.get(key);
                    if (!promise) {
                        promise = Promise.resolve().then(() => fetcher()).finally(() => {
                            this._inflight.delete(key);
                        });
                        this._inflight.set(key, promise);
                    }
                    const freshData = await promise;
                    if (freshData === null || freshData === undefined) return;
                    const freshHash = this.fastHash(freshData);
                    if (freshHash !== cached.hash) {
                        this.set(key, freshData);
                        if (typeof renderFn === 'function') {
                            renderFn(freshData, true);
                        }
                    }
                } catch (err) {
                    console.error(`UIStore SWR background revalidate error (${key}):`, err);
                }
            })();
        } else {
            // First run without cache: show skeleton, then await fetcher
            try {
                if (typeof showSkeletonFn === 'function') {
                    showSkeletonFn();
                }
            } catch (err) {
                console.error(`UIStore SWR skeleton error (${key}):`, err);
            }

            try {
                let promise = this._inflight.get(key);
                if (!promise) {
                    promise = Promise.resolve().then(() => fetcher()).finally(() => {
                        this._inflight.delete(key);
                    });
                    this._inflight.set(key, promise);
                }
                const freshData = await promise;
                if (freshData !== null && freshData !== undefined) {
                    this.set(key, freshData);
                    if (typeof renderFn === 'function') {
                        renderFn(freshData, true);
                    }
                }
            } catch (err) {
                console.error(`UIStore SWR initial fetch error (${key}):`, err);
            }
        }
    }

    async prefetchAll() {
        const endpoints = [
            '/api/sponsors',
            '/api/votes',
            '/api/profile',
            '/api/user-profile',
            '/api/notifications',
            '/api/progression',
            '/api/progression/database'
        ];

        await Promise.allSettled(endpoints.map(async (url) => {
            try {
                const resp = await fetch(url);
                if (resp.ok) {
                    const data = await resp.json();
                    this.set(url, data);
                }
            } catch (e) {
                // Ignore silent prefetch failures
            }
        }));
    }
}

const UIStore = new UIStoreClass();
if (typeof window !== 'undefined') {
    window.UIStore = UIStore;
}
