// ui/js/troubleshooter.js - Network & Driver Troubleshooter
(function() {
    'use strict';

    let currentReport = null;
    let isRunning = false;

    window.openTroubleshooterModal = function(e) {
        if (e) e.stopPropagation();
        const modal = document.getElementById('modal-troubleshooter');
        if (modal) {
            modal.style.display = 'flex';
            runTroubleshooterDiagnostics();
        }
    };

    window.closeTroubleshooterModal = function() {
        const modal = document.getElementById('modal-troubleshooter');
        if (modal) modal.style.display = 'none';
    };

    window.runTroubleshooterDiagnostics = async function() {
        if (isRunning) return;
        isRunning = true;

        const bannerTitle = document.getElementById('tb-banner-title');
        const bannerSub = document.getElementById('tb-banner-sub');
        const bannerBox = document.getElementById('tb-status-banner');
        const listEl = document.getElementById('tb-checks-list');
        const fixBtn = document.getElementById('btn-tb-fix');
        const recheckBtn = document.getElementById('btn-tb-recheck');
        const ticketBtn = document.getElementById('btn-tb-send-ticket');
        const remBox = document.getElementById('tb-remediation-box');

        if (remBox) remBox.style.display = 'none';
        if (fixBtn) fixBtn.style.display = 'none';
        if (recheckBtn) {
            recheckBtn.disabled = true;
            recheckBtn.innerHTML = `
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" class="spin-icon" style="animation: spin 0.8s linear infinite; margin-right: 4px; vertical-align: middle;">
                    <path d="M21 12a9 9 0 1 1-6.219-8.56"/>
                </svg>
                <span>Проверка...</span>
            `;
        }

        if (bannerTitle) bannerTitle.textContent = 'Диагностика сетевого стека...';
        if (bannerSub) bannerSub.textContent = 'Проверка драйверов, Wintun, DNS и шлюзов...';
        if (bannerBox) {
            bannerBox.style.borderLeftColor = 'var(--border-base)';
        }

        if (listEl) {
            listEl.innerHTML = `
                <div class="info-card skeleton-card" style="height: 38px; display: flex; align-items: center; padding: 0 10px;">
                    <div class="skeleton" style="width: 140px; height: 12px;"></div>
                </div>
                <div class="info-card skeleton-card" style="height: 38px; display: flex; align-items: center; padding: 0 10px;">
                    <div class="skeleton" style="width: 180px; height: 12px;"></div>
                </div>
                <div class="info-card skeleton-card" style="height: 38px; display: flex; align-items: center; padding: 0 10px;">
                    <div class="skeleton" style="width: 160px; height: 12px;"></div>
                </div>
            `;
        }

        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), 12000);

        try {
            const resp = await fetch('/api/troubleshoot/run?t=' + Date.now(), { 
                cache: 'no-store',
                signal: controller.signal
            });
            clearTimeout(timeoutId);
            if (!resp.ok) throw new Error('HTTP ' + resp.status);
            const report = await resp.json();
            currentReport = report;
            renderReport(report);
        } catch (err) {
            clearTimeout(timeoutId);
            const errMsg = err.name === 'AbortError' ? 'Превышено время ожидания проверки (таймаут 12с)' : (err.message || 'Не удалось связаться с локальным ядром');
            if (bannerTitle) bannerTitle.textContent = 'Ошибка выполнения проверки';
            if (bannerSub) bannerSub.textContent = errMsg;
            if (bannerBox) bannerBox.style.borderLeftColor = 'var(--accent-red)';
            if (listEl) {
                listEl.innerHTML = `<div class="info-card" style="color: var(--accent-red); font-size: 11px;">Сбой API диагностики: ${escapeHtml(errMsg)}</div>`;
            }
        } finally {
            isRunning = false;
            if (recheckBtn) {
                recheckBtn.disabled = false;
                recheckBtn.innerHTML = `<span>Повторить</span>`;
            }
        }
    };

    function renderReport(report) {
        const bannerTitle = document.getElementById('tb-banner-title');
        const bannerSub = document.getElementById('tb-banner-sub');
        const bannerBox = document.getElementById('tb-status-banner');
        const listEl = document.getElementById('tb-checks-list');
        const fixBtn = document.getElementById('btn-tb-fix');
        const ticketBtn = document.getElementById('btn-tb-send-ticket');

        let hasFixable = false;

        if (report.overall_status === 'healthy') {
            if (bannerTitle) bannerTitle.textContent = 'Все системы работают штатно';
            if (bannerSub) bannerSub.textContent = 'Конфликтов драйверов, DNS и сетевых сбоев не обнаружено';
            if (bannerBox) bannerBox.style.borderLeftColor = '#3fb950';
        } else if (report.overall_status === 'warning') {
            if (bannerTitle) bannerTitle.textContent = 'Обнаружены некритичные замечания';
            if (bannerSub) bannerSub.textContent = 'Рекомендуется применить исправление или проверить настройки';
            if (bannerBox) bannerBox.style.borderLeftColor = '#ffb800';
        } else {
            if (bannerTitle) bannerTitle.textContent = 'Обнаружен критический сбой сети или драйвера';
            if (bannerSub) bannerSub.textContent = 'Ошибки блокируют подключение к игровым серверам';
            if (bannerBox) bannerBox.style.borderLeftColor = 'var(--accent-red)';
        }

        if (listEl) {
            let html = '';
            for (const c of report.checks) {
                if (c.can_auto_fix && c.status !== 'ok') {
                    hasFixable = true;
                }

                let badgeColor = '#3fb950';
                let badgeText = 'В норме';
                let iconColor = '#3fb950';
                if (c.status === 'warning') {
                    badgeColor = '#ffb800';
                    badgeText = 'Внимание';
                    iconColor = '#ffb800';
                } else if (c.status === 'error') {
                    badgeColor = 'var(--accent-red)';
                    badgeText = 'Сбой';
                    iconColor = 'var(--accent-red)';
                }

                html += `
                    <div class="info-card" style="display: flex; flex-direction: column; gap: 4px; padding: 8px 12px; background: #161616;">
                        <div style="display: flex; justify-content: space-between; align-items: center;">
                            <span class="font-mono" style="font-size: 11px; font-weight: 600; color: var(--text-bright);">${escapeHtml(c.title)}</span>
                            <span class="font-mono" style="font-size: 10px; color: ${badgeColor}; border: 1px solid ${badgeColor}; padding: 1px 5px; border-radius: 2px;">${badgeText}</span>
                        </div>
                        <div style="font-size: 11px; color: var(--text);">${escapeHtml(c.message)}</div>
                        ${c.detail && c.status !== 'ok' ? `<div class="font-mono" style="font-size: 10px; color: var(--text-muted); word-break: break-all;">${escapeHtml(c.detail)}</div>` : ''}
                    </div>
                `;
            }
            listEl.innerHTML = html;
        }

        if (fixBtn) {
            fixBtn.style.display = hasFixable ? 'inline-block' : 'none';
        }

        // Check if user is in an active ticket view
        if (ticketBtn) {
            const hasActiveTicket = window.currentSupportTicket && window.currentSupportTicket.id;
            ticketBtn.style.display = hasActiveTicket ? 'inline-block' : 'none';
        }
    }

    window.applyTroubleshooterFix = async function() {
        const fixBtn = document.getElementById('btn-tb-fix');
        const remBox = document.getElementById('tb-remediation-box');
        const remList = document.getElementById('tb-remediation-list');

        if (fixBtn) fixBtn.disabled = true;

        try {
            const resp = await fetch('/api/troubleshoot/fix', { method: 'POST' });
            if (!resp.ok) throw new Error('HTTP ' + resp.status);
            const data = await resp.json();

            if (remBox && remList) {
                let html = '';
                for (const act of data.actions || []) {
                    html += `<li>${escapeHtml(act)}</li>`;
                }
                remList.innerHTML = html;
                remBox.style.display = 'block';
            }

            if (typeof showToast === 'function') {
                showToast('Исправление применено. Повторная проверка...');
            }

            setTimeout(runTroubleshooterDiagnostics, 1200);
        } catch (err) {
            if (typeof showToast === 'function') {
                showToast('Ошибка исправления: ' + err.message);
            }
        } finally {
            if (fixBtn) fixBtn.disabled = false;
        }
    };

    window.copyTroubleshooterReport = function() {
        if (!currentReport || !currentReport.summary_text) {
            if (typeof showToast === 'function') showToast('Сначала выполните диагностику');
            return;
        }
        navigator.clipboard.writeText(currentReport.summary_text).then(() => {
            if (typeof showToast === 'function') showToast('Отчет скопирован в буфер обмена');
        }).catch(() => {
            if (typeof showToast === 'function') showToast('Не удалось скопировать отчет');
        });
    };

    window.sendTroubleshooterToTicket = async function() {
        const activeTicket = window.currentSupportTicket;
        if (!activeTicket || !activeTicket.id) {
            if (typeof showToast === 'function') showToast('Нет активного обращения в поддержку');
            return;
        }

        try {
            const resp = await fetch('/api/support/troubleshoot-report', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ticket_id: activeTicket.id })
            });
            if (resp.ok) {
                if (typeof showToast === 'function') {
                    showToast('Отчет диагностики прикреплен к обращению');
                }
                closeTroubleshooterModal();
                if (typeof fetchSupportChat === 'function') {
                    fetchSupportChat(true, activeTicket.id);
                }
            } else {
                throw new Error('HTTP ' + resp.status);
            }
        } catch (err) {
            if (typeof showToast === 'function') {
                showToast('Ошибка отправки отчета: ' + err.message);
            }
        }
    };

    function escapeHtml(text) {
        if (!text) return '';
        return String(text)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#039;');
    }
})();
