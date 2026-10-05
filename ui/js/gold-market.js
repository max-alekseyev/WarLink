/**
 * WarLink Gold Bar Market & Analytics Controller
 * Strictly Dark Cloudflare Utility design, Zero Emoji, high-performance Canvas rendering.
 * All text strictly in Russian (Zero English words).
 */

(function () {
    let goldMarketData = null;
    let selectedTimeframe = '90d';
    let hoverIndex = -1;
    let pinnedTimestamp = null;
    let canvasResizeObserver = null;

    function formatNumber(num) {
        if (num === null || num === undefined || isNaN(num)) return '0';
        return num.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ' ');
    }

    function formatPct(val) {
        if (val === null || val === undefined || isNaN(val)) return '0.00%';
        const pct = (val * 100).toFixed(2);
        return (val > 0 ? '+' : '') + pct + '%';
    }

    function formatDate(ts) {
        const d = new Date(ts);
        const months = ['янв', 'фев', 'мар', 'апр', 'май', 'июн', 'июл', 'авг', 'сен', 'окт', 'ноя', 'дек'];
        return d.getDate() + ' ' + months[d.getMonth()] + ' ' + d.getFullYear();
    }

    function formatFullDate(ts) {
        const d = new Date(ts);
        const months = ['янв.', 'февр.', 'мар.', 'апр.', 'мая', 'июн.', 'июл.', 'авг.', 'сент.', 'окт.', 'нояб.', 'дек.'];
        return `${d.getDate()} ${months[d.getMonth()]} ${d.getFullYear()} г.`;
    }

    function formatShortPrice(p) {
        if (p >= 1000000) return (p / 1000000).toFixed(2) + ' млн';
        if (p >= 1000) return (p / 1000).toFixed(0) + ' тыс.';
        return p.toString();
    }

    async function fetchGoldMarketData(forceRefresh = false) {
        try {
            const url = forceRefresh ? '/api/market/gold?t=' + Date.now() : '/api/market/gold';
            const res = await fetch(url);
            if (!res.ok) throw new Error('HTTP ' + res.status);
            const data = await res.json();
            const points = (data && (data.points || data.goldBars));
            if (data && data.stats && points) {
                goldMarketData = data;
                renderGoldMarketUI();
                return true;
            }
        } catch (err) {
            console.error('[GOLD-MARKET] Failed to fetch market data:', err);
        }
        return false;
    }

    function renderGoldMarketUI() {
        if (!goldMarketData) return;

        const stats = goldMarketData.stats;
        const current = stats.current;

        // 1. Current Price Card
        const elCurrent = document.getElementById('gold-stat-current');
        if (elCurrent) elCurrent.textContent = '$ ' + formatNumber(current);

        // 2. 7 Days Change
        const el7d = document.getElementById('gold-stat-7d');
        if (el7d && stats.changes) {
            const val = stats.changes['7d'] || 0;
            el7d.textContent = formatPct(val);
            el7d.className = 'gold-stat-val font-mono ' + (val >= 0 ? 'gain' : 'loss');
        }

        // 3. 30 Days Change
        const el30d = document.getElementById('gold-stat-30d');
        if (el30d && stats.changes) {
            const val = stats.changes['30d'] || 0;
            el30d.textContent = formatPct(val);
            el30d.className = 'gold-stat-val font-mono ' + (val >= 0 ? 'gain' : 'loss');
        }

        // 4. All Time Change
        const elAll = document.getElementById('gold-stat-all');
        if (elAll && stats.changes) {
            const val = stats.changes['all'] || 0;
            elAll.textContent = formatPct(val);
            elAll.className = 'gold-stat-val font-mono ' + (val >= 0 ? 'gain' : 'loss');
        }

        // 5. Buy Zone Check ($166k - $200k)
        const inBuyZone = (current >= 166000 && current <= 200000);
        const elSignal = document.getElementById('gold-stat-signal');
        const alertBanner = document.getElementById('gold-alert-banner');
        const alertBannerText = document.getElementById('gold-alert-banner-text');

        if (elSignal) {
            if (inBuyZone) {
                elSignal.textContent = 'ВЫГОДНО: Покупайте';
                elSignal.style.color = '#22c55e';
            } else if (current > 1500000) {
                elSignal.textContent = 'Пик: Фиксация прибыли';
                elSignal.style.color = '#FF5E1F';
            } else {
                elSignal.textContent = 'Ожидание спада';
                elSignal.style.color = 'var(--text-muted)';
            }
        }

        if (alertBanner) {
            if (inBuyZone) {
                alertBanner.style.display = 'flex';
                if (alertBannerText) {
                    alertBannerText.textContent = 'Слиток золота подешевел до $' + formatNumber(current) + '! Отличный момент для покупки.';
                }
            } else {
                alertBanner.style.display = 'none';
            }
        }

        // Min & Max (Min is anchored to $166 000 game minimum)
        const elMin = document.getElementById('gold-stat-min');
        if (elMin) elMin.textContent = '$' + formatNumber(166000);

        const elMax = document.getElementById('gold-stat-max');
        if (elMax) elMax.textContent = '$' + formatNumber(stats.max || 2798240);

        // Reset Date Subtitle
        const allBars = (goldMarketData.points || goldMarketData.goldBars || []);
        const elReset = document.getElementById('gold-stat-reset-date');
        if (elReset && allBars.length > 0) {
            const lastBar = allBars[allBars.length - 1];
            elReset.textContent = 'Обновлено ' + formatDate(lastBar.t);
        }

        // Converter Hint & Initial Values
        const elHint = document.getElementById('gold-calc-hint');
        if (elHint) {
            elHint.textContent = '1 слиток = $ ' + formatNumber(current) + ' по текущему курсу рынка WARDOGS';
        }
        calculateGoldToCash();

        // Update pinned badge if already active
        syncPinnedBadge();

        // Render Chart
        renderChart();
    }

    function getFilteredBars() {
        if (!goldMarketData) return [];
        const allBars = (goldMarketData.points || goldMarketData.goldBars || []);
        if (selectedTimeframe === '7d') return allBars.slice(-7);
        if (selectedTimeframe === '30d') return allBars.slice(-30);
        if (selectedTimeframe === '90d') return allBars.slice(-90);
        return allBars;
    }

    function syncPinnedBadge() {
        const badge = document.getElementById('gold-pinned-badge');
        if (!badge) return;

        if (!pinnedTimestamp || !goldMarketData || !goldMarketData.stats) {
            badge.style.display = 'none';
            return;
        }

        const bars = getFilteredBars();
        const pinnedBar = bars.find(b => b.t === pinnedTimestamp);
        if (!pinnedBar) {
            badge.style.display = 'none';
            return;
        }

        const current = goldMarketData.stats.current;
        const diffPct = pinnedBar.price > 0 ? ((current - pinnedBar.price) / pinnedBar.price) * 100 : 0;
        const sign = diffPct > 0 ? '+' : '';

        const elDate = document.getElementById('gold-pinned-date');
        const elPrice = document.getElementById('gold-pinned-price');
        const elPct = document.getElementById('gold-pinned-pct');

        if (elDate) elDate.textContent = 'С ' + formatFullDate(pinnedBar.t);
        if (elPrice) elPrice.textContent = formatNumber(pinnedBar.price);
        if (elPct) {
            elPct.textContent = sign + diffPct.toFixed(2) + '%';
            elPct.style.color = diffPct >= 0 ? '#22c55e' : '#ef4444';
        }

        badge.style.display = 'inline-flex';
    }

    function renderChart() {
        const canvas = document.getElementById('gold-market-canvas');
        if (!canvas) return;

        const wrap = document.getElementById('gold-canvas-wrap');
        const rect = wrap ? wrap.getBoundingClientRect() : { width: 620, height: 180 };
        const dpr = window.devicePixelRatio || 1;

        const w = rect.width || 620;
        const h = rect.height || 180;

        canvas.width = w * dpr;
        canvas.height = h * dpr;
        canvas.style.width = w + 'px';
        canvas.style.height = h + 'px';

        const ctx = canvas.getContext('2d');
        ctx.resetTransform && ctx.resetTransform();
        ctx.scale(dpr, dpr);

        ctx.clearRect(0, 0, w, h);

        const bars = getFilteredBars();
        if (bars.length < 2) return;

        // Margins configured for crisp labels without clipping:
        const padLeft = 58;  // Ample room for "2.31 млн"
        const padRight = 18;
        const padTop = 20;   // Safe room for peak label above the line
        const padBottom = 26; // Room for X-axis dates

        const plotW = w - padLeft - padRight;
        const plotH = h - padTop - padBottom;

        let minPrice = Infinity;
        let maxPrice = -Infinity;
        let minIdx = 0;
        let maxIdx = 0;

        for (let i = 0; i < bars.length; i++) {
            const p = bars[i].price;
            if (p < minPrice) {
                minPrice = p;
                minIdx = i;
            }
            if (p > maxPrice) {
                maxPrice = p;
                maxIdx = i;
            }
        }

        // Minimum floor is 166 000 (game minimum).
        let plotMin = 166000;
        if (minPrice > 300000) {
            plotMin = Math.max(166000, Math.floor((minPrice * 0.95) / 10000) * 10000);
        }

        // Headroom for peak label on top
        let plotMax = maxPrice * 1.12;
        const priceRange = Math.max(1, plotMax - plotMin);

        const getX = (idx) => padLeft + (idx / (bars.length - 1)) * plotW;
        const getY = (price) => padTop + plotH - ((price - plotMin) / priceRange) * plotH;

        // 1. Draw horizontal grid lines with Russian currency labels
        ctx.strokeStyle = '#222222';
        ctx.lineWidth = 1;
        ctx.setLineDash([]);
        const gridSteps = 4;
        ctx.fillStyle = '#666666';
        ctx.font = '9px monospace';
        ctx.textAlign = 'right';
        ctx.textBaseline = 'middle';

        for (let i = 0; i <= gridSteps; i++) {
            const p = plotMin + (i / gridSteps) * priceRange;
            const y = getY(p);
            ctx.beginPath();
            ctx.moveTo(padLeft, y);
            ctx.lineTo(w - padRight, y);
            ctx.stroke();

            let label = (p / 1000).toFixed(0) + ' тыс.';
            if (p >= 1000000) label = (p / 1000000).toFixed(2) + ' млн';
            ctx.fillText(label, padLeft - 6, y);
        }

        // 2. Draw SINGLE dashed line for period average price (Russian label)
        const avgPrice = Math.round(bars.reduce((sum, b) => sum + b.price, 0) / bars.length);
        const avgY = getY(avgPrice);

        ctx.strokeStyle = 'rgba(255, 255, 255, 0.28)';
        ctx.lineWidth = 1;
        ctx.setLineDash([4, 4]);
        ctx.beginPath();
        ctx.moveTo(padLeft, avgY);
        ctx.lineTo(w - padRight, avgY);
        ctx.stroke();
        ctx.setLineDash([]);

        // Label Ср. ... on the right side above the dashed line
        ctx.fillStyle = '#888888';
        ctx.font = '9px monospace';
        ctx.textAlign = 'right';
        ctx.textBaseline = 'bottom';
        const avgLabel = 'Ср. ' + formatShortPrice(avgPrice);
        ctx.fillText(avgLabel, w - padRight - 4, avgY - 3);

        // 3. Draw Date Ticks on X-axis
        ctx.fillStyle = '#666666';
        ctx.font = '9px monospace';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'top';
        const numTicks = Math.min(6, bars.length);
        for (let t = 0; t < numTicks; t++) {
            const idx = Math.floor((t / (numTicks - 1)) * (bars.length - 1));
            const x = getX(idx);
            const d = new Date(bars[idx].t);
            const months = ['янв', 'фев', 'мар', 'апр', 'май', 'июн', 'июл', 'авг', 'сен', 'окт', 'ноя', 'дек'];
            const dateStr = d.getDate() + ' ' + months[d.getMonth()];
            ctx.fillText(dateStr, x, h - padBottom + 8);
        }

        // 4. Fill Gradient Area Below Trend Line
        const grad = ctx.createLinearGradient(0, padTop, 0, h - padBottom);
        grad.addColorStop(0, 'rgba(255, 94, 31, 0.28)');
        grad.addColorStop(1, 'rgba(255, 94, 31, 0.00)');

        ctx.fillStyle = grad;
        ctx.beginPath();
        ctx.moveTo(getX(0), getY(bars[0].price));
        for (let i = 1; i < bars.length; i++) {
            ctx.lineTo(getX(i), getY(bars[i].price));
        }
        ctx.lineTo(getX(bars.length - 1), h - padBottom);
        ctx.lineTo(getX(0), h - padBottom);
        ctx.closePath();
        ctx.fill();

        // 5. Draw Trend Line
        ctx.strokeStyle = '#FF5E1F';
        ctx.lineWidth = 2;
        ctx.setLineDash([]);
        ctx.beginPath();
        ctx.moveTo(getX(0), getY(bars[0].price));
        for (let i = 1; i < bars.length; i++) {
            ctx.lineTo(getX(i), getY(bars[i].price));
        }
        ctx.stroke();

        // 6. Draw Peak & Valley markers (with collision avoidance)
        const peakX = getX(maxIdx);
        const peakY = getY(maxPrice);
        ctx.fillStyle = '#FF5E1F';
        ctx.beginPath();
        ctx.arc(peakX, peakY, 2.5, 0, Math.PI * 2);
        ctx.fill();

        ctx.fillStyle = '#ffffff';
        ctx.font = '10px monospace';
        ctx.textAlign = 'center';
        if (peakY <= padTop + 14) {
            ctx.textBaseline = 'top';
            ctx.fillText(formatShortPrice(maxPrice), peakX, peakY + 4);
        } else {
            ctx.textBaseline = 'bottom';
            ctx.fillText(formatShortPrice(maxPrice), peakX, peakY - 4);
        }

        if (minIdx !== maxIdx) {
            const valleyX = getX(minIdx);
            const valleyY = getY(minPrice);
            ctx.fillStyle = '#FF5E1F';
            ctx.beginPath();
            ctx.arc(valleyX, valleyY, 2.5, 0, Math.PI * 2);
            ctx.fill();

            ctx.fillStyle = '#ffffff';
            ctx.font = '10px monospace';
            ctx.textAlign = 'center';

            // If valley is near bottom floor, draw label ABOVE the dot so it NEVER overlaps date ticks!
            if (valleyY >= h - padBottom - 18) {
                ctx.textBaseline = 'bottom';
                ctx.fillText(formatShortPrice(minPrice), valleyX, valleyY - 4);
            } else {
                ctx.textBaseline = 'top';
                ctx.fillText(formatShortPrice(minPrice), valleyX, valleyY + 4);
            }
        }

        // 7. Draw PINNED vertical marker (with guaranteed 7px gap from text to line)
        const pinnedIdx = pinnedTimestamp ? bars.findIndex(b => b.t === pinnedTimestamp) : -1;
        if (pinnedIdx >= 0) {
            const pinnedBar = bars[pinnedIdx];
            const pinnedX = getX(pinnedIdx);
            const pinnedY = getY(pinnedBar.price);

            // Vertical dashed line (white)
            ctx.strokeStyle = 'rgba(255, 255, 255, 0.7)';
            ctx.lineWidth = 1;
            ctx.setLineDash([2, 2]);
            ctx.beginPath();
            ctx.moveTo(pinnedX, padTop);
            ctx.lineTo(pinnedX, h - padBottom);
            ctx.stroke();
            ctx.setLineDash([]);

            // Rotated date text along vertical line
            ctx.save();
            ctx.font = '9px monospace';
            const dateStr = formatFullDate(pinnedBar.t);
            const textLength = ctx.measureText(dateStr).width;
            const textY = Math.min(h - padBottom - 8, padTop + textLength + 8);

            // Place text to the left with 7px margin (or to right if near left edge)
            const isNearLeft = pinnedX < (padLeft + 35);
            const textOffset = isNearLeft ? 7 : -7;
            const textBaselineMode = isNearLeft ? 'top' : 'bottom';

            ctx.translate(pinnedX + textOffset, textY);
            ctx.rotate(-Math.PI / 2);
            ctx.fillStyle = '#ffffff';
            ctx.textAlign = 'left';
            ctx.textBaseline = textBaselineMode;
            ctx.fillText(dateStr, 0, 0);
            ctx.restore();

            // Dot on curve for pinned point
            ctx.fillStyle = '#ffffff';
            ctx.beginPath();
            ctx.arc(pinnedX, pinnedY, 3, 0, Math.PI * 2);
            ctx.fill();
            ctx.fillStyle = '#FF5E1F';
            ctx.beginPath();
            ctx.arc(pinnedX, pinnedY, 1.5, 0, Math.PI * 2);
            ctx.fill();
        }

        // 8. Draw HOVER highlight (if pointer active)
        if (hoverIndex >= 0 && hoverIndex < bars.length) {
            const activeX = getX(hoverIndex);
            const activeY = getY(bars[hoverIndex].price);

            // Vertical Guide Line
            ctx.strokeStyle = '#444444';
            ctx.lineWidth = 1;
            ctx.setLineDash([2, 2]);
            ctx.beginPath();
            ctx.moveTo(activeX, padTop);
            ctx.lineTo(activeX, h - padBottom);
            ctx.stroke();
            ctx.setLineDash([]);

            // Outer Glow Circle
            ctx.fillStyle = 'rgba(255, 94, 31, 0.25)';
            ctx.beginPath();
            ctx.arc(activeX, activeY, 6, 0, Math.PI * 2);
            ctx.fill();

            // Inner Solid Circle
            ctx.fillStyle = '#FF5E1F';
            ctx.beginPath();
            ctx.arc(activeX, activeY, 3.5, 0, Math.PI * 2);
            ctx.fill();

            // White Center
            ctx.fillStyle = '#ffffff';
            ctx.beginPath();
            ctx.arc(activeX, activeY, 1.5, 0, Math.PI * 2);
            ctx.fill();
        }
    }

    // Floating Tooltip Update (Strictly Russian text)
    function updateTooltip(e, idx, bars) {
        const tooltip = document.getElementById('gold-chart-tooltip');
        if (!tooltip || idx < 0 || idx >= bars.length) return;

        const b = bars[idx];
        const prevPrice = idx > 0 ? bars[idx - 1].price : b.price;
        const diffPct = prevPrice > 0 ? ((b.price - prevPrice) / prevPrice) * 100 : 0;

        const ttDate = document.getElementById('gold-tt-date');
        const ttPrice = document.getElementById('gold-tt-price');
        const ttChange = document.getElementById('gold-tt-change');

        if (ttDate) ttDate.textContent = formatFullDate(b.t);
        if (ttPrice) {
            ttPrice.innerHTML = `${formatNumber(b.price)} <span>$ за слиток</span>`;
        }
        if (ttChange) {
            const sign = diffPct > 0 ? '+' : '';
            ttChange.textContent = `${sign}${diffPct.toFixed(1)}% к прошлому дню`;
            ttChange.className = 'gold-tt-change font-mono ' + (diffPct >= 0 ? 'gain' : 'loss');
        }

        const wrap = document.getElementById('gold-canvas-wrap');
        const wrapRect = wrap ? wrap.getBoundingClientRect() : { left: 0, top: 0, width: 620, height: 180 };
        const clientX = e.touches ? e.touches[0].clientX : e.clientX;
        const clientY = e.touches ? e.touches[0].clientY : e.clientY;

        const mouseX = clientX - wrapRect.left;
        const mouseY = clientY - wrapRect.top;

        let ttLeft = mouseX + 14;
        let ttTop = mouseY - 20;

        // Flip horizontally if near right boundary
        if (ttLeft + 165 > wrapRect.width) {
            ttLeft = mouseX - 170;
        }
        if (ttLeft < 6) ttLeft = 6;

        // Clamp vertically
        if (ttTop + 75 > wrapRect.height) {
            ttTop = wrapRect.height - 78;
        }
        if (ttTop < 6) ttTop = 6;

        tooltip.style.left = ttLeft + 'px';
        tooltip.style.top = ttTop + 'px';
        tooltip.style.display = 'flex';
    }

    function hideTooltip() {
        const tooltip = document.getElementById('gold-chart-tooltip');
        if (tooltip) tooltip.style.display = 'none';
        hoverIndex = -1;
        renderChart();
    }

    // Canvas Events (Mouse & Touch & Click)
    function setupCanvasEvents() {
        const wrap = document.getElementById('gold-canvas-wrap');
        const canvas = document.getElementById('gold-market-canvas');
        if (!wrap || !canvas) return;

        function handlePointerMove(e) {
            const rect = canvas.getBoundingClientRect();
            const clientX = e.touches ? e.touches[0].clientX : e.clientX;
            const padLeft = 58;
            const padRight = 18;
            const plotW = rect.width - padLeft - padRight;

            const bars = getFilteredBars();
            if (bars.length < 2) return;

            const ratio = (clientX - rect.left - padLeft) / plotW;
            const clamped = Math.max(0, Math.min(1, ratio));
            hoverIndex = Math.round(clamped * (bars.length - 1));

            renderChart();
            updateTooltip(e, hoverIndex, bars);
        }

        function handleCanvasClick(e) {
            const bars = getFilteredBars();
            if (bars.length < 2 || hoverIndex < 0 || hoverIndex >= bars.length) return;

            const b = bars[hoverIndex];
            pinnedTimestamp = b.t;
            syncPinnedBadge();
            renderChart();
        }

        wrap.addEventListener('mousemove', handlePointerMove);
        wrap.addEventListener('touchmove', handlePointerMove, { passive: true });

        wrap.addEventListener('mouseleave', hideTooltip);
        wrap.addEventListener('touchend', hideTooltip);

        wrap.addEventListener('click', handleCanvasClick);

        if (window.ResizeObserver) {
            if (canvasResizeObserver) canvasResizeObserver.disconnect();
            canvasResizeObserver = new ResizeObserver(() => {
                renderChart();
            });
            canvasResizeObserver.observe(wrap);
        }
    }

    // Timeframe selector
    window.setGoldTimeframe = function (tf) {
        selectedTimeframe = tf;
        const btns = document.querySelectorAll('.gold-tf-btn');
        btns.forEach(btn => {
            btn.classList.toggle('active', btn.getAttribute('data-tf') === tf);
        });
        hoverIndex = -1;
        syncPinnedBadge();
        renderChart();
    };

    // Unpin action from header badge
    window.unpinGoldPoint = function (e) {
        if (e) {
            e.preventDefault();
            e.stopPropagation();
        }
        pinnedTimestamp = null;
        syncPinnedBadge();
        renderChart();
    };

    // Calculator: Bars -> Currency
    window.calculateGoldToCash = function () {
        if (!goldMarketData || !goldMarketData.stats) return;
        const rate = goldMarketData.stats.current;
        const inputBars = document.getElementById('gold-calc-bars');
        const inputCash = document.getElementById('gold-calc-cash');
        if (!inputBars || !inputCash) return;

        let bars = parseFloat(inputBars.value);
        if (isNaN(bars) || bars < 0) bars = 1;

        const totalCash = Math.round(bars * rate);
        inputCash.value = formatNumber(totalCash);
    };

    // Calculator: Currency -> Bars
    window.calculateCashToBars = function () {
        if (!goldMarketData || !goldMarketData.stats) return;
        const rate = goldMarketData.stats.current;
        const inputBars = document.getElementById('gold-calc-bars');
        const inputCash = document.getElementById('gold-calc-cash');
        if (!inputBars || !inputCash || rate <= 0) return;

        const cleanStr = inputCash.value.replace(/\s+/g, '').replace(/[^0-9]/g, '');
        const cash = parseInt(cleanStr, 10);
        if (isNaN(cash) || cash <= 0) {
            inputBars.value = '0';
            return;
        }

        const bars = (cash / rate).toFixed(2);
        inputBars.value = bars.endsWith('.00') ? parseInt(bars, 10) : bars;
    };

    // Refresh button
    window.refreshGoldMarket = async function (e) {
        if (e) e.stopPropagation();
        const btn = e ? e.currentTarget : null;
        if (btn) {
            btn.textContent = 'Обновление...';
            btn.disabled = true;
        }
        await fetchGoldMarketData(true);
        if (btn) {
            btn.textContent = 'Обновить';
            btn.disabled = false;
        }
    };

    // View open entry point
    window.openGoldMarket = function () {
        setupCanvasEvents();
        fetchGoldMarketData();
    };

    // Global toggle from titlebar or notification
    window.toggleGoldMarket = function (e) {
        if (e) e.stopPropagation();
        if (typeof switchView === 'function') {
            switchView('view-gold-market');
        }
    };

    // Auto-init listener
    window.addEventListener('DOMContentLoaded', () => {
        setupCanvasEvents();
        fetchGoldMarketData();
    });

})();
