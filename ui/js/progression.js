// Progression & Unlock Roadmap Controller (Dark Cloudflare Utility)
(function() {
    'use strict';

    let currentProgression = {
        career_level: 0,
        roles: {
            assault: 0,
            medic: 0,
            recon: 0,
            support: 0,
            driver: 0,
            pilot: 0
        },
        xp_progress: {
            assault: 0,
            medic: 0,
            recon: 0,
            support: 0,
            driver: 0,
            pilot: 0
        },
        wishlist_id: '',
        unlocked_items: []
    };

    let nextUnlocks = {};
    let allUnlocks = [];
    let currentFilterRole = 'career';
    let currentSearchTerm = '';
    let hideUnlocked = false;
    let isInitialized = false;

    const ROLES = ['assault', 'medic', 'recon', 'support', 'driver', 'pilot'];

    const ROLE_LABELS = {
        career: 'КАРЬЕРА',
        assault: 'ШТУРМОВИК',
        medic: 'МЕДИК',
        recon: 'РАЗВЕДЧИК',
        support: 'ПОДДЕРЖКА',
        driver: 'ВОДИТЕЛЬ',
        pilot: 'ПИЛОТ'
    };

    const ROLE_ICONS = {
        assault: 'assets/roles/assault.webp',
        medic: 'assets/roles/medic.webp',
        recon: 'assets/roles/recon.webp',
        support: 'assets/roles/support.webp',
        driver: 'assets/roles/driver.webp',
        pilot: 'assets/roles/pilot.webp',
        career: 'assets/career/1.webp'
    };

    function escapeHtml(text) {
        if (!text) return '';
        return String(text)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;');
    }

    function getCareerBadgePath(level) {
        if (!level || level <= 0) return 'assets/career/1.webp';
        if (level <= 100) return 'assets/career/' + level + '.webp';
        if (level < 125) return 'assets/career/100.webp';
        if (level < 150) return 'assets/career/125.webp';
        if (level < 175) return 'assets/career/150.webp';
        if (level < 200) return 'assets/career/175.webp';
        if (level < 225) return 'assets/career/200.webp';
        if (level < 250) return 'assets/career/225.webp';
        if (level < 275) return 'assets/career/250.webp';
        if (level < 300) return 'assets/career/275.webp';
        return 'assets/career/300.webp';
    }

    function normalizeItemIcon(icon) {
        if (!icon) return 'wardogs_icon.png';
        if (icon.startsWith('http://') || icon.startsWith('https://')) return icon;
        let clean = icon;
        if (clean.startsWith('guides/icons/')) {
            clean = 'static/wardogs/items/' + clean.slice(13);
        } else if (clean.startsWith('/guides/icons/')) {
            clean = 'static/wardogs/items/' + clean.slice(14);
        } else if (clean.startsWith('guides/images/')) {
            clean = 'static/wardogs/renders/' + clean.slice(14);
        } else if (clean.startsWith('/guides/images/')) {
            clean = 'static/wardogs/renders/' + clean.slice(15);
        }
        if (!clean.startsWith('/')) {
            clean = '/' + clean;
        }
        return clean;
    }

    function checkFirstTimeGuide() {
        if (currentProgression && currentProgression.guide_dismissed) {
            return;
        }
        const seen = localStorage.getItem('wl_prog_guide_seen');
        if (seen) return;

        const modal = document.getElementById('prog-guide-modal');
        if (modal) {
            modal.style.display = 'flex';
        }
    }

    function dismissGuide() {
        if (currentProgression) {
            currentProgression.guide_dismissed = true;
        }
        try {
            localStorage.setItem('wl_prog_guide_seen', '1');
        } catch(e) {}
        const modal = document.getElementById('prog-guide-modal');
        if (modal) {
            modal.style.display = 'none';
        }

        // Persist to backend config.json so it never shows again across restarts
        fetch('/api/progression', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ guide_dismissed: true })
        }).catch(function(err) {
            console.error('Failed to persist guide_dismissed', err);
        });
    }

    function toggleGuide() {
        const modal = document.getElementById('prog-guide-modal');
        if (!modal) return;
        if (modal.style.display === 'none' || !modal.style.display) {
            modal.style.display = 'flex';
        } else {
            modal.style.display = 'none';
        }
    }

    async function init() {
        if (isInitialized) return;
        isInitialized = true;

        // Setup global paste listener for Ctrl+V screenshot import
        window.addEventListener('paste', handleGlobalPaste);

        // Fetch initial data
        await loadProgressionData();
        await loadCatalogDatabase();
    }

    async function loadProgressionData() {
        const fetcher = async () => {
            const resp = await fetch('/api/progression');
            if (!resp.ok) throw new Error('Failed to load progression: ' + resp.status);
            return await resp.json();
        };

        const render = (data) => {
            if (!data) return;
            if (data.progression) {
                currentProgression = data.progression;
                if (!currentProgression.roles) {
                    currentProgression.roles = {};
                }
                if (!currentProgression.xp_progress) {
                    currentProgression.xp_progress = {};
                }
                if (!Array.isArray(currentProgression.unlocked_items)) {
                    currentProgression.unlocked_items = [];
                }
            }
            if (data.next_unlocks) {
                nextUnlocks = data.next_unlocks;
            }
            renderAll();
        };

        if (window.UIStore && typeof UIStore.requestSWR === 'function') {
            await UIStore.requestSWR('/api/progression', fetcher, render, () => {});
        } else {
            try {
                const data = await fetcher();
                render(data);
            } catch (e) {
                console.error('Failed loading progression data:', e);
            }
        }
    }

    async function loadCatalogDatabase() {
        const fetcher = async () => {
            const resp = await fetch('/api/progression/database');
            if (!resp.ok) throw new Error('Failed to load catalog: ' + resp.status);
            return await resp.json();
        };

        const render = (data) => {
            if (data && data.unlocks && Array.isArray(data.unlocks)) {
                allUnlocks = data.unlocks;
                updateCareerNextReward();
                updateTargetBanner();
                renderCatalog();
            }
        };

        if (window.UIStore && typeof UIStore.requestSWR === 'function') {
            await UIStore.requestSWR('/api/progression/database', fetcher, render, () => {});
        } else {
            try {
                const data = await fetcher();
                render(data);
            } catch (e) {
                console.error('Failed loading catalog database:', e);
            }
        }
    }

    function renderAll() {
        renderCareerAndRoles();
        updateTargetBanner();
        renderCatalog();
    }

    function renderCareerAndRoles() {
        // Career level is strictly the arithmetic sum of all 6 roles
        let sum = 0;
        for (const r of ROLES) {
            const lvl = (currentProgression.roles && currentProgression.roles[r]) || 0;
            sum += lvl;

            // Update level number in dial
            const lvlEl = document.getElementById('prog-lvl-' + r);
            if (lvlEl) lvlEl.textContent = String(lvl);

            // Update XP progress arc and dot
            const xp = (currentProgression.xp_progress && currentProgression.xp_progress[r]) || 0;
            const clampedXp = Math.min(100, Math.max(0, xp));

            // Arc stroke fill: 280deg arc on radius 37 has length 180.8
            const arcEl = document.getElementById('prog-dial-arc-' + r);
            if (arcEl) {
                const filled = 180.8 * (clampedXp / 100);
                arcEl.style.strokeDasharray = filled.toFixed(1) + ' 232.5';
            }

            // Dot on perimeter: rotated around center with radius 23.8px (58px dial wrap)
            const dotEl = document.getElementById('prog-dial-dot-' + r);
            if (dotEl) {
                const angle = 130 + (clampedXp / 100) * 280;
                dotEl.style.transform = 'rotate(' + angle.toFixed(1) + 'deg) translate(23.8px) rotate(-' + angle.toFixed(1) + 'deg)';
            }

            // Update role next unlock item name, level requirement and XP/price
            const roleInfo = nextUnlocks && nextUnlocks[r];
            const nameEl = document.getElementById('prog-dial-name-' + r);
            const lvlReqEl = document.getElementById('prog-dial-lvl-' + r);
            const xpReqEl = document.getElementById('prog-dial-xp-' + r);

            if (roleInfo && roleInfo.next_item) {
                const it = roleInfo.next_item;
                const displayName = it.name_ru || it.name;
                if (nameEl) {
                    nameEl.textContent = displayName;
                    nameEl.title = displayName;
                }
                if (lvlReqEl) lvlReqEl.textContent = 'Ур. ' + it.level;
                if (xpReqEl) {
                    if (it.total_xp) {
                        xpReqEl.textContent = it.total_xp.toLocaleString('en-US') + ' XP';
                    } else if (it.price) {
                        xpReqEl.textContent = '$' + it.price.toLocaleString('en-US');
                    } else {
                        xpReqEl.textContent = '';
                    }
                }
            } else {
                if (nameEl) {
                    nameEl.textContent = 'Все открыто';
                    nameEl.title = 'Все открыто';
                }
                if (lvlReqEl) lvlReqEl.textContent = 'MAX';
                if (xpReqEl) xpReqEl.textContent = '-';
            }
        }

        currentProgression.career_level = sum;

        // Update Career Level in main header row
        const careerValEl = document.getElementById('prog-career-val');
        if (careerValEl) careerValEl.textContent = String(sum);

        // Update Career Badge Icon
        const badgeImg = document.getElementById('prog-career-badge-img');
        if (badgeImg) {
            badgeImg.src = getCareerBadgePath(sum);
        }

        updateCareerNextReward();
    }

    function updateCareerNextReward() {
        const nextNameEl = document.getElementById('prog-career-next-name');
        const nextLvlEl = document.getElementById('prog-career-next-lvl');
        const nextPriceEl = document.getElementById('prog-career-next-price');

        const careerInfo = nextUnlocks && nextUnlocks['career'];
        if (careerInfo && careerInfo.next_item) {
            const item = careerInfo.next_item;
            const displayName = item.name_ru || item.name;
            if (nextNameEl) nextNameEl.textContent = displayName;
            if (nextLvlEl) nextLvlEl.textContent = 'Ур. ' + item.level;
            if (nextPriceEl) nextPriceEl.textContent = item.price ? '$' + item.price.toLocaleString('en-US') : '$0';
        } else {
            // Find closest career reward from allUnlocks if nextUnlocks['career'] not ready
            const careerItems = allUnlocks.filter(it => it.role === 'career' && it.level > currentProgression.career_level);
            if (careerItems.length > 0) {
                const item = careerItems[0];
                const displayName = item.name_ru || item.name;
                if (nextNameEl) nextNameEl.textContent = displayName;
                if (nextLvlEl) nextLvlEl.textContent = 'Ур. ' + item.level;
                if (nextPriceEl) nextPriceEl.textContent = item.price ? '$' + item.price.toLocaleString('en-US') : '$0';
            } else {
                if (nextNameEl) nextNameEl.textContent = 'Все награды получены';
                if (nextLvlEl) nextLvlEl.textContent = 'MAX';
                if (nextPriceEl) nextPriceEl.textContent = '-';
            }
        }
    }

    function updateTargetBanner() {
        const banner = document.getElementById('prog-target-banner');
        if (!banner) return;

        const wishlistId = currentProgression.wishlist_id;
        if (!wishlistId) {
            banner.style.display = 'none';
            return;
        }

        const targetItem = allUnlocks.find(it => it.unlock_id === wishlistId);
        if (!targetItem) {
            banner.style.display = 'none';
            return;
        }

        const targetRole = targetItem.role;
        let curLvl = 0;
        let roleNameRu = 'КАРЬЕРА';
        if (targetRole === 'career') {
            curLvl = currentProgression.career_level || 0;
            roleNameRu = 'КАРЬЕРА';
        } else {
            curLvl = (currentProgression.roles && currentProgression.roles[targetRole]) || 0;
            const roleLabels = {
                assault: 'ШТУРМОВИК',
                medic: 'МЕДИК',
                recon: 'РАЗВЕДЧИК',
                support: 'ПОДДЕРЖКА',
                driver: 'ВОДИТЕЛЬ',
                pilot: 'ПИЛОТ'
            };
            roleNameRu = roleLabels[targetRole] || (targetRole ? targetRole.toUpperCase() : 'ОПЕРАТОР');
        }

        const reqLvl = targetItem.level || 1;
        const remLvl = Math.max(0, reqLvl - curLvl);
        const pct = Math.min(100, Math.round((curLvl / reqLvl) * 100));

        // Calculate financial goals: target price & total path equipment budget
        const targetPrice = targetItem.price || 0;
        let pathBudget = 0;
        for (const it of allUnlocks) {
            if (it.role === targetRole && it.level > curLvl && it.level <= reqLvl) {
                pathBudget += (it.price || 0);
            }
        }
        if (pathBudget === 0) pathBudget = targetPrice;

        const imgEl = document.getElementById('prog-target-img');
        const nameEl = document.getElementById('prog-target-name');
        const catEl = document.getElementById('prog-target-cat');
        const roleLblEl = document.getElementById('prog-target-role-lbl');
        const reqLvlEl = document.getElementById('prog-target-req-lvl');
        const remEl = document.getElementById('prog-target-remaining');
        const priceEl = document.getElementById('prog-target-price');
        const budgetEl = document.getElementById('prog-target-path-budget');
        const statusEl = document.getElementById('prog-target-cur-status');
        const pctEl = document.getElementById('prog-target-pct');
        const fillEl = document.getElementById('prog-target-bar-fill');

        if (imgEl) imgEl.src = normalizeItemIcon(targetItem.icon);
        if (nameEl) nameEl.textContent = targetItem.name_ru || targetItem.name;
        if (catEl) catEl.textContent = targetItem.category_ru || SUBCAT_RU[targetItem.subcategory] || targetItem.subcategory || targetItem.tab || 'Предмет';
        if (roleLblEl) roleLblEl.textContent = roleNameRu;
        if (reqLvlEl) reqLvlEl.textContent = 'Ур. ' + reqLvl;
        if (remEl) {
            remEl.textContent = remLvl === 0 ? 'Уровень достигнут' : ('Осталось: ' + remLvl + ' ур.');
        }
        if (priceEl) priceEl.textContent = targetPrice ? '$' + targetPrice.toLocaleString('en-US') : '$0';
        if (budgetEl) budgetEl.textContent = pathBudget ? '$' + pathBudget.toLocaleString('en-US') : '$0';
        if (statusEl) statusEl.textContent = curLvl + ' / ' + reqLvl;
        if (pctEl) pctEl.textContent = pct + '%';
        if (fillEl) fillEl.style.width = pct + '%';

        banner.style.display = 'flex';
    }

    async function stepRole(role, delta) {
        if (!currentProgression.roles) currentProgression.roles = {};
        const cur = currentProgression.roles[role] || 0;
        const next = Math.max(0, cur + delta);
        currentProgression.roles[role] = next;

        // Strictly re-calculate sum of all 6 roles
        let sum = 0;
        for (const r of ROLES) {
            sum += currentProgression.roles[r] || 0;
        }
        currentProgression.career_level = sum;

        renderAll();

        try {
            const resp = await fetch('/api/progression', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    roles: currentProgression.roles,
                    career_level: sum,
                    wishlist_id: currentProgression.wishlist_id || '',
                    unlocked_items: currentProgression.unlocked_items || []
                })
            });
            if (resp.ok) {
                const data = await resp.json();
                if (data.next_unlocks) {
                    nextUnlocks = data.next_unlocks;
                    renderCareerAndRoles();
                    updateTargetBanner();
                    renderCatalog();
                }
            }
        } catch (e) {
            console.error('Failed saving role level:', e);
        }
    }

    async function resetAll() {
        // Reset all 6 roles and career to 0, clear unlocked items
        currentProgression.roles = {
            assault: 0,
            medic: 0,
            recon: 0,
            support: 0,
            driver: 0,
            pilot: 0
        };
        currentProgression.career_level = 0;
        currentProgression.xp_progress = {
            assault: 0,
            medic: 0,
            recon: 0,
            support: 0,
            driver: 0,
            pilot: 0
        };
        currentProgression.unlocked_items = [];
        currentProgression.wishlist_id = '';

        renderAll();

        if (window.showToast) {
            showToast('Весь прогресс сброшен до 0');
        }

        try {
            const resp = await fetch('/api/progression', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    roles: currentProgression.roles,
                    career_level: 0,
                    xp_progress: currentProgression.xp_progress,
                    wishlist_id: '',
                    unlocked_items: []
                })
            });
            if (resp.ok) {
                const data = await resp.json();
                if (data.next_unlocks) {
                    nextUnlocks = data.next_unlocks;
                    renderCareerAndRoles();
                    updateTargetBanner();
                    renderCatalog();
                }
            }
        } catch (e) {
            console.error('Failed resetting progression:', e);
        }
    }

    const SUBCAT_RU = {
        'Pistol': 'Пистолет',
        'Magazines': 'Магазин',
        'Body Armour': 'Броня',
        'Helmets': 'Шлем',
        'Optics': 'Прицел',
        'Muzzles': 'ДТК / Глушитель',
        'Assault Rifle': 'Штурм. винтовка',
        'SMG': 'Пистолет-пулемет',
        'Submachine Gun': 'Пистолет-пулемет',
        'Shotgun': 'Дробовик',
        'Sniper Rifle': 'Снайп. винтовка',
        'DMR': 'Марксман. винтовка',
        'Marksman Rifle': 'Марксман. винтовка',
        'LMG': 'Пулемет',
        'Light Machine Gun': 'Ручной пулемет',
        'Machine Gun': 'Пулемет',
        'Backpack': 'Рюкзак',
        'Vest': 'Разгрузка',
        'Grip': 'Рукоятка',
        'Foregrips': 'Рукоятка',
        'Ammunition': 'Боеприпасы',
        'Vehicle': 'Техника',
        'Ground': 'Наземная техника',
        'Air': 'Авиация',
        'Patch': 'Патч',
        'Medical': 'Медицина',
        'Supplies': 'Припасы',
        'Traversal': 'Снаряжение',
        'Building': 'Строительство',
        'Tactical': 'Тактическое',
        'Charge': 'Взрывчатка',
        'Launcher': 'Гранатомет',
        'Bow': 'Лук',
        'Arrows': 'Стрелы',
        'Crate': 'Ящик',
        'Recon': 'Разведка',
        'Other': 'Прочее'
    };

    function toggleHideUnlocked(checked) {
        if (typeof checked === 'boolean') {
            hideUnlocked = checked;
        } else {
            hideUnlocked = !hideUnlocked;
        }
        const checkInput = document.getElementById('check-prog-hide-unlocked');
        if (checkInput && checkInput.checked !== hideUnlocked) {
            checkInput.checked = hideUnlocked;
        }
        renderCatalog();
    }

    async function toggleItemUnlocked(unlockId, e) {
        if (e) e.stopPropagation();
        if (!unlockId) return;

        if (!Array.isArray(currentProgression.unlocked_items)) {
            currentProgression.unlocked_items = [];
        }

        const idx = currentProgression.unlocked_items.indexOf(unlockId);
        if (idx >= 0) {
            currentProgression.unlocked_items.splice(idx, 1);
        } else {
            currentProgression.unlocked_items.push(unlockId);
        }

        renderCatalog();

        try {
            await fetch('/api/progression', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    roles: currentProgression.roles,
                    career_level: currentProgression.career_level,
                    xp_progress: currentProgression.xp_progress,
                    wishlist_id: currentProgression.wishlist_id,
                    unlocked_items: currentProgression.unlocked_items
                })
            });
        } catch (err) {
            console.error('Failed saving unlocked item state:', err);
        }
    }

    function renderCatalog() {
        const tbody = document.getElementById('prog-catalog-tbody');
        if (!tbody) return;

        const countEl = document.getElementById('prog-catalog-count');
        const unlockedSet = new Set(Array.isArray(currentProgression.unlocked_items) ? currentProgression.unlocked_items : []);

        let filtered = allUnlocks.filter(item => {
            if (item.role !== currentFilterRole) {
                return false;
            }
            const isUnlocked = unlockedSet.has(item.unlock_id);
            if (hideUnlocked && isUnlocked) {
                return false;
            }
            if (currentSearchTerm) {
                const q = currentSearchTerm.toLowerCase();
                const matchName = (item.name && item.name.toLowerCase().includes(q)) || (item.name_ru && item.name_ru.toLowerCase().includes(q));
                const matchCat = (item.category_ru && item.category_ru.toLowerCase().includes(q)) || (item.subcategory && item.subcategory.toLowerCase().includes(q));
                const matchDesc = item.description && item.description.toLowerCase().includes(q);
                if (!matchName && !matchCat && !matchDesc) return false;
            }
            return true;
        });

        // Strictly sort by level ascending
        filtered.sort((a, b) => (a.level || 0) - (b.level || 0));

        if (countEl) {
            countEl.textContent = '(' + filtered.length + ' наград)';
        }

        if (filtered.length === 0) {
            tbody.innerHTML = '<tr class="prog-empty-table-row font-mono"><td colspan="6">Ничего не найдено по заданному фильтру</td></tr>';
            return;
        }

        let html = '';
        for (const item of filtered) {
            const isUnlocked = unlockedSet.has(item.unlock_id);
            const isWishlist = currentProgression.wishlist_id === item.unlock_id;

            let rowClass = '';
            if (isUnlocked) {
                rowClass = 'is-unlocked-row';
            } else if (isWishlist) {
                rowClass = 'is-wishlist-row';
            }

            const iconUrl = normalizeItemIcon(item.icon);
            const priceFormatted = item.price ? '$' + item.price.toLocaleString('en-US') : '—';
            const catName = item.category_ru || SUBCAT_RU[item.subcategory] || item.subcategory || item.tab || 'Предмет';
            const displayName = item.name_ru || item.name;

            const lockBtnHtml = isUnlocked
                ? `<button class="btn-prog-lock is-unlocked" onclick="ProgressionController.toggleItemUnlocked('${escapeHtml(item.unlock_id)}', event)" title="Разблокировано (нажмите, чтобы отметить заблокированным)"><svg class="prog-lock-svg unlocked" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#00e676" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 9.9-1"/></svg></button>`
                : `<button class="btn-prog-lock is-locked" onclick="ProgressionController.toggleItemUnlocked('${escapeHtml(item.unlock_id)}', event)" title="Заблокировано (нажмите, чтобы отметить открытым)"><svg class="prog-lock-svg locked" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#666666" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg></button>`;

            const targetBtnHtml = isWishlist
                ? `<button class="btn-prog-target is-active font-mono" onclick="ProgressionController.toggleWishlist('${escapeHtml(item.unlock_id)}', event)" title="Цель активна (нажмите для отмены)"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="6"/><circle cx="12" cy="12" r="2"/></svg></button>`
                : `<button class="btn-prog-target font-mono" onclick="ProgressionController.toggleWishlist('${escapeHtml(item.unlock_id)}', event)" title="Сделать целью"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="6"/><circle cx="12" cy="12" r="2"/></svg></button>`;

            html += `
                <tr class="${rowClass}">
                    <td class="td-lock-cell">${lockBtnHtml}</td>
                    <td class="td-lvl-cell">
                        <span class="td-lvl-badge font-mono">${item.level}</span>
                    </td>
                    <td>
                        <div class="td-item-cell">
                            <img class="td-item-icon" src="${escapeHtml(iconUrl)}" alt="${escapeHtml(displayName)}" onerror="this.src='wardogs_icon.png'">
                            <span class="td-item-name font-mono" title="${escapeHtml(displayName)}">${escapeHtml(displayName)}</span>
                        </div>
                    </td>
                    <td>
                        <span class="td-cat-text font-mono" title="${escapeHtml(catName)}">${escapeHtml(catName)}</span>
                    </td>
                    <td class="td-price-cell font-mono">${priceFormatted}</td>
                    <td style="text-align: center;">${targetBtnHtml}</td>
                </tr>
            `;
        }

        tbody.innerHTML = html;
    }

    async function toggleWishlist(unlockId, e) {
        if (e) e.stopPropagation();
        const nextId = (currentProgression.wishlist_id === unlockId) ? '' : (unlockId || '');
        currentProgression.wishlist_id = nextId;
        updateTargetBanner();
        renderCatalog();

        try {
            await fetch('/api/progression', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    roles: currentProgression.roles,
                    career_level: currentProgression.career_level,
                    xp_progress: currentProgression.xp_progress,
                    wishlist_id: nextId,
                    unlocked_items: currentProgression.unlocked_items || []
                })
            });
        } catch (e) {
            console.error('Failed updating wishlist:', e);
        }
    }

    async function setWishlist(unlockId) {
        return toggleWishlist(unlockId);
    }

    function setFilter(role) {
        currentFilterRole = role;
        document.querySelectorAll('.prog-tab-btn').forEach(btn => {
            btn.classList.toggle('active', btn.dataset.filter === role);
        });
        renderCatalog();
    }

    function onSearchInput(e) {
        currentSearchTerm = e.target.value.trim();
        renderCatalog();
    }

    // --- Screenshot Parsing via Clipboard (Ctrl+V) & File Upload ---
    async function handleGlobalPaste(e) {
        const items = (e.clipboardData || e.originalEvent.clipboardData).items;
        if (!items) return;

        for (let i = 0; i < items.length; i++) {
            if (items[i].type.indexOf('image') !== -1) {
                const blob = items[i].getAsFile();
                if (blob) {
                    e.preventDefault();
                    await uploadScreenshotBlob(blob);
                    return;
                }
            }
        }
    }

    function triggerImport() {
        const fileInput = document.getElementById('prog-file-input');
        if (fileInput) fileInput.click();
    }

    function onFileSelected(e) {
        const file = e.target.files && e.target.files[0];
        if (file) {
            uploadScreenshotBlob(file);
        }
    }

    async function uploadScreenshotBlob(blob) {
        const progEl = document.getElementById('view-progression');
        if (!progEl || !progEl.classList.contains('active')) {
            if (window.switchView) {
                window.switchView('view-progression');
            }
        }

        if (window.showToast) {
            showToast('Распознавание прогресса WARDOGS...');
        }

        try {
            const formData = new FormData();
            formData.append('image', blob, 'screenshot.png');

            const resp = await fetch('/api/progression/parse', {
                method: 'POST',
                body: formData
            });

            if (!resp.ok) {
                const errData = await resp.json();
                throw new Error(errData.error || 'Ошибка распознавания');
            }

            const data = await resp.json();
            if (data.success && data.progression) {
                currentProgression = data.progression;
                if (data.next_unlocks) {
                    nextUnlocks = data.next_unlocks;
                }
                renderAll();
                if (window.showToast) {
                    showToast('Прогресс распознан: Карьера ' + currentProgression.career_level);
                }
            }
        } catch (err) {
            console.error('Screenshot parse error:', err);
            if (window.showToast) {
                showToast('Ошибка распознавания: ' + err.message);
            }
        }
    }

    // --- Drag and Drop Handlers ---
    function onDragOver(e) {
        e.preventDefault();
        e.stopPropagation();
        const dropzone = document.getElementById('prog-dropzone');
        if (dropzone) dropzone.style.display = 'flex';
    }

    function onDragLeave(e) {
        e.preventDefault();
        e.stopPropagation();
        const dropzone = document.getElementById('prog-dropzone');
        if (dropzone && e.relatedTarget && !dropzone.contains(e.relatedTarget)) {
            dropzone.style.display = 'none';
        }
    }

    function onDrop(e) {
        e.preventDefault();
        e.stopPropagation();
        const dropzone = document.getElementById('prog-dropzone');
        if (dropzone) dropzone.style.display = 'none';

        const files = e.dataTransfer && e.dataTransfer.files;
        if (files && files.length > 0) {
            uploadScreenshotBlob(files[0]);
        }
    }

    function onOpen() {
        renderAll();
        checkFirstTimeGuide();
    }

    function getProgressionSummary() {
        if (!currentProgression) return null;
        let sum = currentProgression.career_level || 0;
        const roles = currentProgression.roles || {};
        let roleSum = 0;
        for (const r of ROLES) {
            roleSum += (roles[r] || 0);
        }
        if (sum === 0 && roleSum > 0) sum = roleSum;

        if (sum === 0 && roleSum === 0) return null;

        let targetItemInfo = null;
        if (currentProgression.wishlist_id && allUnlocks && allUnlocks.length > 0) {
            const it = allUnlocks.find(x => x.unlock_id === currentProgression.wishlist_id);
            if (it) {
                const targetRole = it.role || 'assault';
                const curLvl = targetRole === 'career' ? sum : (roles[targetRole] || 0);
                const reqLvl = it.level || 0;
                const remLvl = Math.max(0, reqLvl - curLvl);
                targetItemInfo = {
                    name: it.name_ru || it.name,
                    level: reqLvl,
                    role: targetRole,
                    curLvl: curLvl,
                    remLvl: remLvl,
                    isReached: curLvl >= reqLvl
                };
            }
        }

        return {
            hasProgression: true,
            careerLevel: sum,
            roles: {
                assault: roles.assault || 0,
                medic: roles.medic || 0,
                recon: roles.recon || 0,
                support: roles.support || 0,
                driver: roles.driver || 0,
                pilot: roles.pilot || 0
            },
            targetItem: targetItemInfo
        };
    }

    function drawRoundRect(ctx, x, y, w, h, r, fill, stroke) {
        if (typeof r === 'number') r = [r, r, r, r];
        ctx.beginPath();
        if (ctx.roundRect) {
            ctx.roundRect(x, y, w, h, r);
        } else {
            ctx.moveTo(x + r[0], y);
            ctx.lineTo(x + w - r[1], y);
            ctx.quadraticCurveTo(x + w, y, x + w, y + r[1]);
            ctx.lineTo(x + w, y + h - r[2]);
            ctx.quadraticCurveTo(x + w, y + h, x + w - r[2], y + h);
            ctx.lineTo(x + r[3], y + h);
            ctx.quadraticCurveTo(x, y + h, x, y + h - r[3]);
            ctx.lineTo(x, y + r[0]);
            ctx.quadraticCurveTo(x, y, x + r[0], y);
            ctx.closePath();
        }
        if (fill) ctx.fill();
        if (stroke) ctx.stroke();
    }

    function fillTextEllipsis(ctx, text, x, y, maxW) {
        if (!text) return;
        if (ctx.measureText(text).width <= maxW) {
            ctx.fillText(text, x, y);
            return;
        }
        let truncated = text;
        while (truncated.length > 1 && ctx.measureText(truncated + '...').width > maxW) {
            truncated = truncated.slice(0, -1);
        }
        ctx.fillText(truncated + '...', x, y);
    }

    function drawImageContain(ctx, img, x, y, maxW, maxH) {
        if (!img || !img.complete || !img.naturalWidth || !img.naturalHeight) return;
        const nw = img.naturalWidth;
        const nh = img.naturalHeight;
        const scale = Math.min(maxW / nw, maxH / nh);
        const w = Math.round(nw * scale);
        const h = Math.round(nh * scale);
        const dx = Math.round(x + (maxW - w) / 2);
        const dy = Math.round(y + (maxH - h) / 2);
        ctx.drawImage(img, dx, dy, w, h);
    }

    function drawQRCode(ctx, text, x, y, size, darkColor = '#111111', lightColor = '#FFFFFF', quietZone = 1.5) {
        if (typeof qrcode === 'undefined') return;
        try {
            const qr = qrcode(0, 'M');
            qr.addData(text);
            qr.make();
            const count = qr.getModuleCount();
            const totalCount = count + quietZone * 2;
            const cellSize = size / totalCount;

            if (lightColor) {
                ctx.fillStyle = lightColor;
                ctx.fillRect(x, y, size, size);
            }

            ctx.fillStyle = darkColor;
            for (let r = 0; r < count; r++) {
                for (let c = 0; c < count; c++) {
                    if (qr.isDark(r, c)) {
                        const cellX = x + (c + quietZone) * cellSize;
                        const cellY = y + (r + quietZone) * cellSize;
                        ctx.fillRect(cellX, cellY, cellSize + 0.05, cellSize + 0.05);
                    }
                }
            }
        } catch (e) {
            console.error('Failed drawing QR code:', e);
        }
    }

    const ROLE_MAX_LEVELS = {
        assault: 20,
        medic: 35,
        recon: 20,
        support: 40,
        driver: 30,
        pilot: 25
    };

    const shareRoleImgCache = {};
    const SHARE_ROLE_KEYS = ['assault', 'medic', 'recon', 'support', 'driver', 'pilot'];
    SHARE_ROLE_KEYS.forEach(k => {
        const img = new Image();
        img.src = 'assets/roles/' + k + '.webp';
        img.onload = () => {
            const modal = document.getElementById('prog-share-modal');
            if (modal && modal.style.display !== 'none') {
                renderShareCard();
            }
        };
        shareRoleImgCache[k] = img;
    });

    let careerBadgeImgCache = null;
    let lastCareerBadgePath = '';
    let targetWeaponImgCache = null;
    let lastTargetWeaponPath = '';

    function renderShareCard() {
        const canvas = document.getElementById('prog-share-canvas');
        if (!canvas) return;
        const ctx = canvas.getContext('2d');
        if (!ctx) return;

        try {
            const roles = (currentProgression && currentProgression.roles) || {};
            const sum = (roles.assault || 0) + (roles.medic || 0) + (roles.recon || 0) +
                        (roles.support || 0) + (roles.driver || 0) + (roles.pilot || 0);

            // Target Item (Wishlist)
            let targetItem = null;
            if (currentProgression && currentProgression.wishlist_id && allUnlocks && allUnlocks.length > 0) {
                targetItem = allUnlocks.find(x => x.unlock_id === currentProgression.wishlist_id);
            }

            const hasTarget = !!targetItem;

            // Safe zones & Layout Dimensions
            const safePadX = 16;
            const safePadY = 16;
            const cardW = 672;
            const card1H = 186;
            const card2H = 70;
            const gap = 10;

            const baseW = cardW + safePadX * 2;
            const baseH = hasTarget ? (safePadY + card1H + gap + card2H + safePadY) : (safePadY + card1H + safePadY);

            // 2.5x Retina Scale for ultra-crisp high-resolution output
            const SCALE = 2.5;
            const totalW = Math.round(baseW * SCALE);
            const totalH = Math.round(baseH * SCALE);

            canvas.width = totalW;
            canvas.height = totalH;

            ctx.save();
            ctx.scale(SCALE, SCALE);

            // 1. Solid Outer Framing Canvas Background (Safe Zone)
            ctx.fillStyle = '#0d0d0d';
            ctx.fillRect(0, 0, baseW, baseH);

            // ==========================================
            // CARD 1: MAIN METAFORGE WIDGET CARD
            // ==========================================
            const cardX = safePadX;
            const cardY = safePadY;

            // Background & Border
            ctx.fillStyle = '#141414';
            ctx.strokeStyle = '#222222';
            ctx.lineWidth = 1;
            drawRoundRect(ctx, cardX + 0.5, cardY + 0.5, cardW - 1, card1H - 1, 3, true, true);

            // --- CAREER ROW ---
            const crestX = cardX + 16;
            const crestY = cardY + 14;
            const crestSize = 34;

            // Crest Box
            ctx.fillStyle = '#181818';
            ctx.strokeStyle = '#2a2a2a';
            ctx.lineWidth = 1;
            drawRoundRect(ctx, crestX + 0.5, crestY + 0.5, crestSize - 1, crestSize - 1, 2, true, true);

            // Career Badge Icon inside Box
            const careerBadgePath = getCareerBadgePath(sum);
            if (!careerBadgeImgCache || lastCareerBadgePath !== careerBadgePath) {
                careerBadgeImgCache = new Image();
                lastCareerBadgePath = careerBadgePath;
                careerBadgeImgCache.onload = () => {
                    const modal = document.getElementById('prog-share-modal');
                    if (modal && modal.style.display !== 'none') renderShareCard();
                };
                careerBadgeImgCache.src = careerBadgePath;
            }

            if (careerBadgeImgCache && careerBadgeImgCache.complete && careerBadgeImgCache.naturalWidth > 0) {
                drawImageContain(ctx, careerBadgeImgCache, crestX + 2, crestY + 2, crestSize - 4, crestSize - 4);
            }

            // Big Career Level Number
            const lvlNumX = crestX + crestSize + 12;
            ctx.font = '800 22px "Segoe UI", sans-serif';
            ctx.fillStyle = '#FFFFFF';
            ctx.textAlign = 'left';
            ctx.textBaseline = 'alphabetic';
            ctx.fillText(String(sum), lvlNumX, crestY + 25);
            const sumW = ctx.measureText(String(sum)).width;

            // Career Next Reward Info (Inline)
            let nextCareerItem = null;
            if (nextUnlocks && nextUnlocks['career'] && nextUnlocks['career'].next_item) {
                nextCareerItem = nextUnlocks['career'].next_item;
            } else if (allUnlocks && allUnlocks.length > 0) {
                nextCareerItem = allUnlocks.find(x => x.role === 'career' && x.level > sum);
            }

            let inlineX = lvlNumX + sumW + 12;
            const textY = crestY + 22;

            ctx.font = '11px "Segoe UI", sans-serif';
            ctx.fillStyle = '#8a8a8a';
            ctx.fillText('Следующая награда: ', inlineX, textY);
            inlineX += ctx.measureText('Следующая награда: ').width;

            if (nextCareerItem) {
                const rewardName = nextCareerItem.name_ru || nextCareerItem.name;
                ctx.font = '600 11px "Segoe UI", sans-serif';
                ctx.fillStyle = '#FFFFFF';
                ctx.fillText(rewardName, inlineX, textY);
                inlineX += ctx.measureText(rewardName).width;

                ctx.font = '11px "Segoe UI", sans-serif';
                ctx.fillStyle = '#333333';
                ctx.fillText('  |  ', inlineX, textY);
                inlineX += ctx.measureText('  |  ').width;

                ctx.font = '700 11px "Segoe UI", sans-serif';
                ctx.fillStyle = '#f0b820';
                ctx.fillText('Ур. ' + nextCareerItem.level, inlineX, textY);
                inlineX += ctx.measureText('Ур. ' + nextCareerItem.level).width;

                ctx.font = '11px "Segoe UI", sans-serif';
                ctx.fillStyle = '#333333';
                ctx.fillText('  |  ', inlineX, textY);
                inlineX += ctx.measureText('  |  ').width;

                const priceStr = nextCareerItem.price ? '$' + nextCareerItem.price.toLocaleString('en-US') : '$0';
                ctx.font = '500 11px "Segoe UI", sans-serif';
                ctx.fillStyle = '#a3a3a3';
                ctx.fillText(priceStr, inlineX, textY);
            } else {
                ctx.font = '600 11px "Segoe UI", sans-serif';
                ctx.fillStyle = '#4ade80';
                ctx.fillText('Все награды получены', inlineX, textY);
            }

            // --- GITHUB REPOSITORY QR CODE & BRAND BADGE ---
            const qrSize = 34;
            const qrX = cardX + cardW - 16 - qrSize;
            const qrY = cardY + 14;

            // Draw QR Code pointing to WarLink GitHub
            drawQRCode(ctx, 'https://github.com/max-alekseyev/WarLink', qrX, qrY, qrSize, '#111111', '#FFFFFF', 1.5);

            // Border around QR Code Box
            ctx.strokeStyle = '#282828';
            ctx.lineWidth = 1;
            drawRoundRect(ctx, qrX - 0.5, qrY - 0.5, qrSize + 1, qrSize + 1, 2, false, true);

            // Brand Text to the left of QR Code
            const qrTextRight = qrX - 8;
            ctx.textAlign = 'right';
            ctx.font = '700 9px "Segoe UI", sans-serif';
            ctx.fillStyle = '#FFFFFF';
            ctx.fillText('WarLink', qrTextRight, qrY + 13);

            ctx.font = '600 8px "Segoe UI", sans-serif';
            ctx.fillStyle = '#FF5E1F';
            ctx.fillText('GitHub', qrTextRight, qrY + 25);
            ctx.textAlign = 'left';

            // Subline Divider under career row
            const divY = cardY + 58;
            ctx.strokeStyle = '#1e1e1e';
            ctx.lineWidth = 1;
            ctx.beginPath();
            ctx.moveTo(cardX + 16, divY + 0.5);
            ctx.lineTo(cardX + cardW - 16, divY + 0.5);
            ctx.stroke();

            // --- 6 CIRCULAR DIALS ROW ---
            const roleOrder = ['assault', 'medic', 'recon', 'support', 'driver', 'pilot'];
            const colW = (cardW - 32 - 5 * 8) / 6; // 100px per column
            const dialCenterY = divY + 44; // Perfectly positioned inside Card 1
            const radius = 24;
            const startAngle = 130 * Math.PI / 180;
            const totalSweep = 280 * Math.PI / 180;

            roleOrder.forEach((r, idx) => {
                const colX = cardX + 16 + idx * (colW + 8);
                const colCenterX = colX + colW / 2;
                const lvl = roles[r] || 0;
                const maxLvl = ROLE_MAX_LEVELS[r] || 30;

                const roleInfo = (nextUnlocks && nextUnlocks[r]) || null;
                let roleItem = (roleInfo && (roleInfo.item || roleInfo.next_item)) || null;
                if (!roleItem && allUnlocks && allUnlocks.length > 0) {
                    roleItem = allUnlocks.find(it => it.role === r && it.level > lvl) || null;
                }

                let xp = (currentProgression && currentProgression.xp_progress && currentProgression.xp_progress[r]) || 0;
                if (xp === 0 && lvl > 0) {
                    xp = Math.min(100, Math.round((lvl / maxLvl) * 100));
                }
                const clampedXp = Math.min(100, Math.max(0, xp));

                // 1. Dark Track Arc (280 degrees from 130 deg)
                ctx.beginPath();
                ctx.arc(colCenterX, dialCenterY, radius, startAngle, startAngle + totalSweep);
                ctx.strokeStyle = '#222222';
                ctx.lineWidth = 2.8;
                ctx.lineCap = 'butt';
                ctx.stroke();

                // 2. Progress Fill Arc
                if (clampedXp > 0) {
                    const fillSweep = (clampedXp / 100) * totalSweep;
                    const endAngle = startAngle + fillSweep;

                    ctx.beginPath();
                    ctx.arc(colCenterX, dialCenterY, radius, startAngle, endAngle);
                    ctx.strokeStyle = '#f0b820';
                    ctx.lineWidth = 2.8;
                    ctx.lineCap = 'round';
                    ctx.stroke();

                    // Glowing Dot at perimeter
                    const dotX = colCenterX + radius * Math.cos(endAngle);
                    const dotY = dialCenterY + radius * Math.sin(endAngle);
                    ctx.beginPath();
                    ctx.arc(dotX, dotY, 2.5, 0, Math.PI * 2);
                    ctx.fillStyle = '#f0b820';
                    ctx.shadowColor = 'rgba(240, 184, 32, 0.7)';
                    ctx.shadowBlur = 5;
                    ctx.fill();
                    ctx.shadowBlur = 0;
                }

                // 3. Inside Dial Center: Role Icon + Level (Clean non-overlapping layout, zero steppers)
                const rImg = shareRoleImgCache[r];
                if (rImg && rImg.complete && rImg.naturalWidth > 0) {
                    drawImageContain(ctx, rImg, colCenterX - 6, dialCenterY - 14, 12, 12);
                }

                // Level Number (Positioned cleanly below icon with 3px safe gap)
                ctx.textAlign = 'center';
                ctx.font = '700 13px "Segoe UI", sans-serif';
                ctx.fillStyle = '#FFFFFF';
                ctx.fillText(String(lvl), colCenterX, dialCenterY + 11);

                // 4. Dial Meta Below Circle
                const metaTopY = dialCenterY + radius + 7;

                // Role Title (e.g. ШТУРМОВИК)
                ctx.font = '700 8.5px "Segoe UI", sans-serif';
                ctx.fillStyle = '#8a8a8a';
                ctx.fillText(ROLE_LABELS[r] || r.toUpperCase(), colCenterX, metaTopY + 8);

                // Next Item Name (truncated with ellipsis if needed)
                let itemName = '—';
                let reqLvlStr = 'MAX';
                let reqXpOrPrice = '';

                if (roleItem) {
                    itemName = roleItem.name_ru || roleItem.name;
                    reqLvlStr = 'Ур. ' + roleItem.level;
                    if (roleItem.total_xp) {
                        reqXpOrPrice = roleItem.total_xp.toLocaleString('en-US') + ' XP';
                    } else if (roleItem.price) {
                        reqXpOrPrice = '$' + roleItem.price.toLocaleString('en-US');
                    }
                }

                ctx.font = '600 8.5px "Segoe UI", sans-serif';
                ctx.fillStyle = '#FFFFFF';
                fillTextEllipsis(ctx, itemName, colCenterX, metaTopY + 20, colW - 6);

                // Requirement line (e.g. Ур. 12 | 58,500 XP)
                const reqY = metaTopY + 32;
                if (reqXpOrPrice) {
                    ctx.font = '700 8px "Segoe UI", sans-serif';
                    const lvlW = ctx.measureText(reqLvlStr).width;
                    ctx.font = '8px "Segoe UI", sans-serif';
                    const sepW = ctx.measureText(' | ').width;
                    const xpW = ctx.measureText(reqXpOrPrice).width;
                    const totalReqW = lvlW + sepW + xpW;

                    let startReqX = colCenterX - totalReqW / 2;
                    ctx.textAlign = 'left';

                    ctx.font = '700 8px "Segoe UI", sans-serif';
                    ctx.fillStyle = '#f0b820';
                    ctx.fillText(reqLvlStr, startReqX, reqY);
                    startReqX += lvlW;

                    ctx.font = '8px "Segoe UI", sans-serif';
                    ctx.fillStyle = '#444444';
                    ctx.fillText(' | ', startReqX, reqY);
                    startReqX += sepW;

                    ctx.fillStyle = '#888888';
                    ctx.fillText(reqXpOrPrice, startReqX, reqY);
                } else {
                    ctx.textAlign = 'center';
                    ctx.font = '700 8px "Segoe UI", sans-serif';
                    ctx.fillStyle = '#f0b820';
                    ctx.fillText(reqLvlStr, colCenterX, reqY);
                }
            });

            // ==========================================
            // CARD 2: ACTIVE TARGET ROADMAP BANNER
            // ==========================================
            if (hasTarget) {
                const targetY = cardY + card1H + gap;
                const tH = card2H;

                // Background & Border
                ctx.fillStyle = '#141414';
                ctx.strokeStyle = 'rgba(255, 94, 31, 0.4)';
                ctx.lineWidth = 1;
                drawRoundRect(ctx, cardX + 0.5, targetY + 0.5, cardW - 1, tH - 1, 3, true, true);

                // Left Orange Accent Line (3px solid #FF5E1F)
                ctx.fillStyle = '#FF5E1F';
                ctx.fillRect(cardX, targetY, 3, tH);

                // Weapon Preview Box (54 x 44)
                const boxX = cardX + 16;
                const boxY = targetY + 13;
                const boxW = 54;
                const boxH = 44;

                ctx.fillStyle = '#1a1a1a';
                ctx.strokeStyle = '#282828';
                ctx.lineWidth = 1;
                drawRoundRect(ctx, boxX + 0.5, boxY + 0.5, boxW - 1, boxH - 1, 2, true, true);

                // Load Weapon Image
                const weaponIconPath = targetItem.icon || '';
                if (weaponIconPath && (!targetWeaponImgCache || lastTargetWeaponPath !== weaponIconPath)) {
                    targetWeaponImgCache = new Image();
                    lastTargetWeaponPath = weaponIconPath;
                    targetWeaponImgCache.onload = () => {
                        const modal = document.getElementById('prog-share-modal');
                        if (modal && modal.style.display !== 'none') renderShareCard();
                    };
                    targetWeaponImgCache.src = weaponIconPath;
                }

                if (targetWeaponImgCache && targetWeaponImgCache.complete && targetWeaponImgCache.naturalWidth > 0) {
                    drawImageContain(ctx, targetWeaponImgCache, boxX + 3, boxY + 3, boxW - 6, boxH - 6);
                }

                // Target Data Calculations
                const tRole = targetItem.role || 'assault';
                const tCurLvl = tRole === 'career' ? sum : (roles[tRole] || 0);
                const tReqLvl = targetItem.level || 0;
                const tRem = Math.max(0, tReqLvl - tCurLvl);
                const isReached = tCurLvl >= tReqLvl;
                const pct = tReqLvl > 0 ? Math.min(100, Math.round((tCurLvl / tReqLvl) * 100)) : 100;
                const targetPrice = targetItem.price || 0;

                let pathBudget = 0;
                if (allUnlocks && allUnlocks.length > 0) {
                    for (const it of allUnlocks) {
                        if (it.role === tRole && it.level > tCurLvl && it.level <= tReqLvl) {
                            pathBudget += (it.price || 0);
                        }
                    }
                }
                if (pathBudget === 0) pathBudget = targetPrice;

                const tCat = targetItem.category_ru || SUBCAT_RU[targetItem.subcategory] || targetItem.subcategory || 'Предмет';
                const tName = targetItem.name_ru || targetItem.name;

                // Content Left of Banner
                const metaX = boxX + boxW + 12;
                ctx.textAlign = 'left';

                // Row 1: Badge + Name + Category (Y = targetY + 22)
                const r1Y = targetY + 22;

                // Badge "ЦЕЛЬ"
                ctx.fillStyle = 'rgba(255, 94, 31, 0.12)';
                ctx.strokeStyle = 'rgba(255, 94, 31, 0.3)';
                ctx.lineWidth = 1;
                drawRoundRect(ctx, metaX, targetY + 11, 28, 13, 2, true, true);

                ctx.font = '700 8px "Segoe UI", sans-serif';
                ctx.fillStyle = '#FF5E1F';
                ctx.textAlign = 'center';
                ctx.fillText('ЦЕЛЬ', metaX + 14, targetY + 20.5);
                ctx.textAlign = 'left';

                // Weapon Name
                ctx.font = '700 11.5px "Segoe UI", sans-serif';
                ctx.fillStyle = '#FFFFFF';
                ctx.fillText(tName, metaX + 34, r1Y);
                const nameW = ctx.measureText(tName).width;

                // Category
                ctx.font = '9.5px "Segoe UI", sans-serif';
                ctx.fillStyle = '#888888';
                ctx.fillText(tCat, metaX + 34 + nameW + 6, r1Y);

                // Row 2: Role + Required + Remaining (Y = targetY + 37)
                const r2Y = targetY + 37;
                let r2X = metaX;

                ctx.font = '600 9.5px "Segoe UI", sans-serif';
                ctx.fillStyle = '#f0b820';
                const roleRu = ROLE_LABELS[tRole] || tRole.toUpperCase();
                ctx.fillText(roleRu, r2X, r2Y);
                r2X += ctx.measureText(roleRu).width;

                ctx.font = '9.5px "Segoe UI", sans-serif';
                ctx.fillStyle = '#444444';
                ctx.fillText('  •  ', r2X, r2Y);
                r2X += ctx.measureText('  •  ').width;

                ctx.fillStyle = '#a3a3a3';
                ctx.fillText('Требуется: ', r2X, r2Y);
                r2X += ctx.measureText('Требуется: ').width;

                ctx.font = '700 9.5px "Segoe UI", sans-serif';
                ctx.fillStyle = '#f0b820';
                ctx.fillText('Ур. ' + tReqLvl, r2X, r2Y);
                r2X += ctx.measureText('Ур. ' + tReqLvl).width;

                ctx.font = '9.5px "Segoe UI", sans-serif';
                ctx.fillStyle = '#444444';
                ctx.fillText('  •  ', r2X, r2Y);
                r2X += ctx.measureText('  •  ').width;

                if (isReached) {
                    ctx.fillStyle = '#00e676';
                    ctx.fillText('Цель достигнута', r2X, r2Y);
                } else {
                    ctx.fillStyle = '#a3a3a3';
                    ctx.fillText('Осталось: ' + tRem + ' ур.', r2X, r2Y);
                }

                // Row 3: Finance Info (Y = targetY + 51)
                const r3Y = targetY + 51;
                let r3X = metaX;

                ctx.font = '8.5px "Segoe UI", sans-serif';
                ctx.fillStyle = '#777777';
                ctx.fillText('Цена цели: ', r3X, r3Y);
                r3X += ctx.measureText('Цена цели: ').width;

                ctx.font = '600 8.5px "Segoe UI", sans-serif';
                ctx.fillStyle = '#00e676';
                const priceTargetStr = '$' + targetPrice.toLocaleString('en-US');
                ctx.fillText(priceTargetStr, r3X, r3Y);
                r3X += ctx.measureText(priceTargetStr).width;

                ctx.font = '8.5px "Segoe UI", sans-serif';
                ctx.fillStyle = '#444444';
                ctx.fillText('  •  ', r3X, r3Y);
                r3X += ctx.measureText('  •  ').width;

                ctx.fillStyle = '#777777';
                ctx.fillText('Бюджет ветки: ', r3X, r3Y);
                r3X += ctx.measureText('Бюджет ветки: ').width;

                ctx.font = '600 8.5px "Segoe UI", sans-serif';
                ctx.fillStyle = '#f0b820';
                ctx.fillText('$' + pathBudget.toLocaleString('en-US'), r3X, r3Y);

                // Right Side of Target Banner: Progress Bar & Attribution
                const barW = 150;
                const barX = cardX + cardW - 16 - barW;

                // Fraction & Percentage Row (Y = targetY + 28)
                ctx.font = '9px "Consolas", monospace';
                ctx.fillStyle = '#888888';
                ctx.textAlign = 'left';
                ctx.fillText(tCurLvl + ' / ' + tReqLvl, barX, targetY + 28);

                ctx.textAlign = 'right';
                ctx.fillText(pct + '%', barX + barW, targetY + 28);

                // Progress Bar Track
                ctx.fillStyle = '#222222';
                drawRoundRect(ctx, barX, targetY + 34, barW, 5, 2.5, true, false);

                // Progress Bar Fill
                const fillW = Math.round(barW * (pct / 100));
                if (fillW > 0) {
                    ctx.fillStyle = '#FF5E1F';
                    drawRoundRect(ctx, barX, targetY + 34, fillW, 5, 2.5, true, false);
                }

                // Clean Attribution under Progress Bar
                ctx.font = '8px "Consolas", monospace';
                ctx.fillStyle = '#666666';
                ctx.textAlign = 'right';
                ctx.fillText('WarLink // github.com/max-alekseyev/WarLink', barX + barW, targetY + 52);
                ctx.textAlign = 'left';
            }

            ctx.restore();
        } catch (err) {
            console.error('renderShareCard failed:', err);
        }
    }

    function openShareModal() {
        const modal = document.getElementById('prog-share-modal');
        if (modal) {
            modal.style.display = 'flex';
            if (document.fonts && document.fonts.ready) {
                document.fonts.ready.then(() => {
                    renderShareCard();
                });
            }
            renderShareCard();
        }
    }

    function closeShareModal() {
        const modal = document.getElementById('prog-share-modal');
        if (modal) modal.style.display = 'none';
    }

    async function copyShareCard() {
        const canvas = document.getElementById('prog-share-canvas');
        if (!canvas) return;
        const copyBtn = document.getElementById('btn-prog-copy-card');

        try {
            canvas.toBlob(async function(blob) {
                if (!blob) return;
                try {
                    await navigator.clipboard.write([
                        new ClipboardItem({ 'image/png': blob })
                    ]);
                    if (copyBtn) {
                        const originalHTML = copyBtn.innerHTML;
                        copyBtn.classList.add('btn-copy-success');
                        copyBtn.innerHTML = `
                            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                                <polyline points="20 6 9 17 4 12"></polyline>
                            </svg>
                            <span>Скопировано в буфер!</span>
                        `;
                        setTimeout(() => {
                            copyBtn.classList.remove('btn-copy-success');
                            copyBtn.innerHTML = originalHTML;
                        }, 2200);
                    }
                    if (typeof showToast === 'function') {
                        showToast('Карточка прогресса скопирована в буфер обмена');
                    }
                } catch (err) {
                    console.error('Clipboard copy failed:', err);
                    if (typeof showToast === 'function') {
                        showToast('Не удалось скопировать. Используйте «Сохранить PNG»');
                    }
                }
            }, 'image/png');
        } catch (e) {
            console.error('toBlob failed:', e);
        }
    }

    function saveShareCard() {
        const canvas = document.getElementById('prog-share-canvas');
        if (!canvas) return;
        const saveBtn = document.getElementById('btn-prog-save-card');

        try {
            const dataUrl = canvas.toDataURL('image/png');
            const link = document.createElement('a');
            link.download = 'warlink_wardogs_level.png';
            link.href = dataUrl;
            document.body.appendChild(link);
            link.click();
            document.body.removeChild(link);

            if (saveBtn) {
                const originalHTML = saveBtn.innerHTML;
                saveBtn.classList.add('btn-copy-success');
                saveBtn.innerHTML = `
                    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                        <polyline points="20 6 9 17 4 12"></polyline>
                    </svg>
                    <span>Сохранено!</span>
                `;
                setTimeout(() => {
                    saveBtn.classList.remove('btn-copy-success');
                    saveBtn.innerHTML = originalHTML;
                }, 2200);
            }

            if (typeof showToast === 'function') {
                showToast('Карточка сохранена: warlink_wardogs_level.png');
            }
        } catch (err) {
            console.error('Save card failed:', err);
        }
    }

    async function onSteamScreenshotSync(data) {
        try {
            await loadProgressionData();
            const shareModal = document.getElementById('prog-share-modal');
            if (shareModal && shareModal.style.display !== 'none') {
                renderShareCard();
            }
            if (typeof showToast === 'function') {
                const lvl = (currentProgression && currentProgression.career_level) || (data && data.career_level) || 0;
                showToast('Снимок Steam F12 синхронизирован. Уровень WARDOGS: ' + lvl);
            }
        } catch (e) {
            console.error('Error in onSteamScreenshotSync:', e);
            if (data) {
                currentProgression = data;
                renderAll();
            }
        }
    }

    function onClose() {
        const guideModal = document.getElementById('prog-guide-modal');
        if (guideModal) guideModal.style.display = 'none';
        const shareModal = document.getElementById('prog-share-modal');
        if (shareModal) shareModal.style.display = 'none';
    }

    // Export ProgressionController to global window
    window.ProgressionController = {
        init: init,
        onOpen: onOpen,
        onClose: onClose,
        checkFirstTimeGuide: checkFirstTimeGuide,
        resetAll: resetAll,
        stepRole: stepRole,
        setFilter: setFilter,
        onSearchInput: onSearchInput,
        setWishlist: setWishlist,
        toggleWishlist: toggleWishlist,
        toggleHideUnlocked: toggleHideUnlocked,
        toggleItemUnlocked: toggleItemUnlocked,
        triggerImport: triggerImport,
        onFileSelected: onFileSelected,
        onDragOver: onDragOver,
        onDragLeave: onDragLeave,
        onDrop: onDrop,
        getProgressionSummary: getProgressionSummary,
        getAllUnlocks: function() { return allUnlocks; },
        dismissGuide: dismissGuide,
        toggleGuide: toggleGuide,
        openShareModal: openShareModal,
        closeShareModal: closeShareModal,
        renderShareCard: renderShareCard,
        copyShareCard: copyShareCard,
        saveShareCard: saveShareCard,
        onSteamScreenshotSync: onSteamScreenshotSync
    };

    // Auto-init on DOMContentLoaded
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
