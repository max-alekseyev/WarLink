// ==========================================================================
// WarLink Network Diagnostics, System Tweaks & Anomaly Alert Monitor
// ==========================================================================

(function() {
    'use strict';

    // State for telemetry anomaly detection
    let anomalyStreak = 0;
    let lastAnomalyAlertTime = 0;
    const ANOMALY_COOLDOWN_MS = 180000; // 3 minutes cooldown between toasts

    // Process real-time telemetry metrics and display laconic notification if persistent loss/jitter
    window.checkNetworkAnomaly = function(lossPct, jitterMs, isConnected) {
        if (!isConnected) {
            anomalyStreak = 0;
            return;
        }

        const hasLoss = typeof lossPct === 'number' && lossPct >= 10;
        const hasJitter = typeof jitterMs === 'number' && jitterMs >= 40;

        if (hasLoss || hasJitter) {
            anomalyStreak++;
        } else {
            anomalyStreak = 0;
        }

        // Trigger after 3 consecutive degraded readings
        if (anomalyStreak >= 3) {
            const now = Date.now();
            if (now - lastAnomalyAlertTime > ANOMALY_COOLDOWN_MS) {
                lastAnomalyAlertTime = now;
                const lossStr = hasLoss ? `Loss: ${Math.round(lossPct)}%` : '';
                const jitterStr = hasJitter ? `Jitter: ${Math.round(jitterMs)} мс` : '';
                const detail = [lossStr, jitterStr].filter(Boolean).join(', ');

                if (typeof showToast === 'function') {
                    showToast(`Зафиксированы потери пакетов на внешнем интернет-узле (${detail})`);
                }
            }
        }
    };

    // Windows Gaming Tweaks UI
    window.fetchSystemTweaks = async function() {
        const groupEl = document.getElementById('sys-tweaks-group');
        if (!groupEl) return;

        try {
            const res = await fetch('/api/system/tweaks');
            if (!res.ok) return;
            const data = await res.json();

            const throttleEl = document.getElementById('tweak-stat-throttling');
            const dscpEl = document.getElementById('tweak-stat-dscp');
            const winsockEl = document.getElementById('tweak-stat-winsock');

            if (throttleEl) {
                throttleEl.textContent = data.throttling_disabled ? 'ОТКЛЮЧЕН (0xFFFFFFFF)' : 'АКТИВЕН (ПО УМОЛЧАНИЮ)';
                throttleEl.style.color = data.throttling_disabled ? 'var(--accent)' : 'var(--accent-red)';
            }
            if (dscpEl) {
                dscpEl.textContent = data.dscp_nla_bypassed ? 'РАЗБЛОКИРОВАНА (DSCP 46)' : 'СТАНДАРТНАЯ';
                dscpEl.style.color = data.dscp_nla_bypassed ? 'var(--accent)' : 'var(--text-muted)';
            }
            if (winsockEl) {
                winsockEl.textContent = data.fast_datagram_threshold ? 'АКТИВНА (MTU 1500)' : 'СТАНДАРТНАЯ';
                winsockEl.style.color = data.fast_datagram_threshold ? 'var(--accent)' : 'var(--text-muted)';
            }
        } catch (err) {
            console.warn('[TWEAKS] Failed to fetch tweaks status:', err);
        }
    };

    window.applySystemTweaks = async function(e) {
        if (e) e.stopPropagation();
        const btn = document.getElementById('btn-apply-sys-tweaks');
        if (btn) btn.disabled = true;

        try {
            const res = await fetch('/api/system/tweaks', { method: 'POST' });
            if (!res.ok) throw new Error('HTTP ' + res.status);
            const data = await res.json();

            if (typeof showToast === 'function') {
                showToast('Оптимизация сетевого стека Windows успешно применена');
            }
            await window.fetchSystemTweaks();
        } catch (err) {
            if (typeof showToast === 'function') {
                showToast('Ошибка применения твиков: ' + err.message);
            }
        } finally {
            if (btn) btn.disabled = false;
        }
    };

    // Emergency Network Doctor repair
    window.runEmergencyNetworkRepair = async function(e) {
        if (e) e.stopPropagation();
        const btn = document.getElementById('btn-emergency-repair');
        if (btn) btn.disabled = true;

        if (typeof showToast === 'function') {
            showToast('Сброс Wintun-адаптера, очистка DNS и завершение конфликтов...');
        }

        try {
            const res = await fetch('/api/network/repair', { method: 'POST' });
            if (!res.ok) throw new Error('HTTP ' + res.status);
            const data = await res.json();

            if (typeof showToast === 'function') {
                showToast('Сетевой стек успешно восстановлен');
            }
            if (typeof runTroubleshooterDiagnostics === 'function') {
                setTimeout(runTroubleshooterDiagnostics, 1000);
            }
        } catch (err) {
            if (typeof showToast === 'function') {
                showToast('Ошибка восстановления: ' + err.message);
            }
        } finally {
            if (btn) btn.disabled = false;
        }
    };
})();
