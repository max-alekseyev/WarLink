// ui/js/store.js - Frontend SWR Cache & State Store for WarLink v2.2.4

class UIStoreClass {
    constructor() {
        this._cache = new Map();
        this._imageCache = new Map();
        this._inflight = new Map();
        this._storagePrefix = 'wl_cache_';
        this._loadPersistentCache();
    }

    _isPopulated(key, data) {
        if (!data) return false;
        if (key.includes('/api/notifications')) {
            return Array.isArray(data.notifications) && data.notifications.length > 0;
        }
        if (key.includes('/api/sponsors')) {
            return Array.isArray(data.sponsors) && data.sponsors.length > 0;
        }
        if (key.includes('/api/market/gold')) {
            return Array.isArray(data.points) && data.points.length > 0;
        }
        if (key.includes('/api/progression/database')) {
            return data && Array.isArray(data.unlocks) && data.unlocks.length >= 270;
        }
        if (key.includes('/api/user-profile')) {
            return !!(data.account_number && data.created_at && data.created_at !== '—');
        }
        if (Array.isArray(data)) {
            return data.length > 0;
        }
        return true;
    }

    _loadPersistentCache() {
        try {
            if (typeof localStorage === 'undefined') return;
            for (let i = 0; i < localStorage.length; i++) {
                const k = localStorage.key(i);
                if (k && k.startsWith(this._storagePrefix)) {
                    const endpointKey = k.substring(this._storagePrefix.length);
                    const raw = localStorage.getItem(k);
                    if (raw) {
                        const parsed = JSON.parse(raw);
                        if (parsed && parsed.data !== undefined) {
                            // If persisted cache was empty placeholder, purge it so fresh skeletons reflect real loading
                            if (this._isPopulated(endpointKey, parsed.data)) {
                                this._cache.set(endpointKey, parsed);
                            } else {
                                localStorage.removeItem(k);
                            }
                        }
                    }
                }
            }
        } catch (e) {
            console.warn('UIStore failed to hydrate from localStorage:', e);
        }
    }

    get(key) {
        if (!this._cache.has(key)) return null;
        return this._cache.get(key).data;
    }

    set(key, data, etag = null) {
        const hash = this.fastHash(data);
        const item = {
            data,
            hash,
            etag,
            timestamp: Date.now()
        };
        this._cache.set(key, item);
        try {
            if (typeof localStorage !== 'undefined') {
                localStorage.setItem(this._storagePrefix + key, JSON.stringify(item));
            }
        } catch (e) {}
    }

    has(key) {
        return this._cache.has(key);
    }

    invalidate(key) {
        this._cache.delete(key);
        try {
            if (typeof localStorage !== 'undefined') {
                localStorage.removeItem(this._storagePrefix + key);
            }
        } catch (e) {}
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
        const cached = this._cache.get(key);
        const hasPopulatedCache = cached && this._isPopulated(key, cached.data);

        if (hasPopulatedCache) {
            // Instant 0ms render from warm populated cache (zero skeletons, zero layout shift)
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
            // First run or empty cache: display true skeleton while network fetch is in flight
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
            '/api/user-profile',
            '/api/notifications',
            '/api/progression',
            '/api/community-goal',
            '/api/market/gold',
            '/api/support/active'
        ];

        for (const url of endpoints) {
            try {
                const resp = await fetch(url);
                if (resp.ok) {
                    const data = await resp.json();
                    if (this._isPopulated(url, data)) {
                        this.set(url, data);
                    }
                }
            } catch (e) {
                // Ignore silent prefetch failures
            }
            // Small pause between background fetches to keep UI loop silky smooth
            await new Promise(r => setTimeout(r, 60));
        }
    }
}

const UIStore = new UIStoreClass();
if (typeof window !== 'undefined') {
    window.UIStore = UIStore;
}
