// ui/js/audio.js - Tactical Audio Cues powered by Foley.js (mechanical theme)
(function() {
    let audioEnabled = true;

    // Load saved sound preference
    try {
        const saved = localStorage.getItem('wl_sound_effects');
        if (saved !== null) {
            audioEnabled = (saved === 'true' || saved === '1');
        }
    } catch (e) {}

    // Initialize Foley with mechanical tactical theme
    function initFoley() {
        if (typeof window.Foley !== 'undefined' && typeof window.Foley.set === 'function') {
            window.Foley.set({
                theme: 'mechanical',
                volume: 0.70
            });
        }
    }

    // Unlock audio context on user gesture or window focus
    function unlockAudio() {
        if (typeof window.Foley !== 'undefined' && typeof window.Foley.unlock === 'function') {
            window.Foley.unlock();
        }
    }
    window.addEventListener('focus', unlockAudio);
    document.addEventListener('pointerdown', unlockAudio);
    document.addEventListener('click', unlockAudio);
    document.addEventListener('keydown', unlockAudio);

    window.WarLinkAudio = {
        init: initFoley,
        unlock: unlockAudio,
        setEnabled: function(val) {
            audioEnabled = Boolean(val);
            try {
                localStorage.setItem('wl_sound_effects', audioEnabled ? 'true' : 'false');
            } catch (e) {}
            // Also notify backend if supported
            fetch('/api/settings/sound', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ enable_sound_effects: audioEnabled })
            }).catch(() => {});
        },
        isEnabled: function() {
            return audioEnabled;
        },
        playNotification: function(severity) {
            if (!audioEnabled || typeof window.Foley === 'undefined') return;
            unlockAudio();
            const sev = (severity || 'info').toLowerCase();
            if (sev === 'warning' || sev === 'urgent') {
                window.Foley.play('warning');
            } else {
                window.Foley.play('ping');
            }
        },
        playDenied: function() {
            if (!audioEnabled || typeof window.Foley === 'undefined') return;
            unlockAudio();
            window.Foley.play('denied');
        }
    };

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', initFoley);
    } else {
        initFoley();
    }
})();
