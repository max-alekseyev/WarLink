// ==========================================================================
// WarLink Network Connection, Port Hopping & Lifecycle Manager
// ==========================================================================

(function() {
    'use strict';

    let lastIsDisconnecting = false;

    // Handles engine lifecycle status from /api/status or SSE
    window.handleNetworkStatusPayload = function(data) {
        if (!data) return;

        const isConnected = !!data.is_connected;
        const isDisconnecting = !!data.is_disconnecting;

        // Anomaly monitoring
        const loss = typeof data.packet_loss === 'number' ? data.packet_loss : (data.telemetry ? data.telemetry.loss : 0);
        const jitter = typeof data.jitter_ms === 'number' ? data.jitter_ms : (data.telemetry ? data.telemetry.jitter : 0);
        if (typeof window.checkNetworkAnomaly === 'function') {
            window.checkNetworkAnomaly(loss, jitter, isConnected);
        }

        // Prevent race condition on rapid connect/disconnect: lock main action button
        const mainBtn = document.getElementById('connect-btn');
        if (mainBtn) {
            if (isDisconnecting) {
                mainBtn.disabled = true;
                const label = mainBtn.querySelector('.btn-label') || mainBtn;
                if (!lastIsDisconnecting) {
                    mainBtn.dataset.prevText = label.textContent;
                }
                label.textContent = 'Освобождение адаптера...';
            } else if (lastIsDisconnecting && !isDisconnecting) {
                mainBtn.disabled = false;
                const label = mainBtn.querySelector('.btn-label') || mainBtn;
                if (mainBtn.dataset.prevText) {
                    label.textContent = mainBtn.dataset.prevText;
                }
            }
        }
        lastIsDisconnecting = isDisconnecting;
    };
})();
