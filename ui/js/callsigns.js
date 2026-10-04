// ui/js/callsigns.js - Deterministic Nobel CallSign Generator (Zero Emoji, Clean Cloudflare)
(function() {
    const NOBEL_LAUREATES = [
        "Рентген", "Кюри", "Бор", "Планк", "Эйнштейн",
        "Ферми", "Гейзенберг", "Шрёдингер", "Дирак", "Паули",
        "Фейнман", "Ландау", "Капица", "Сахаров", "Семенов",
        "Басов", "Прохоров", "Тамм", "Франк", "Черенков",
        "Алферов", "Гинзбург", "Абрикосов", "Павлов", "Мечников",
        "Резерфорд", "Борн", "Маркони", "Лоренц", "Лауэ",
        "Чедвик", "Юкава", "Купер", "Бардин", "Шокли",
        "Таунс", "Хиггс", "Пенроуз", "Гейм", "Новоселов"
    ];

    const NOBEL_CITIES = [
        "Стокгольм", "Осло", "Женева", "Берн", "Цюрих",
        "Мюнхен", "Вена", "Рим", "Париж", "Лондон",
        "Кембридж", "Оксфорд", "Лейпциг", "Геттинген", "Копенгаген",
        "Бостон", "Чикаго", "Принстон", "Москва", "Дубна",
        "Воронеж", "Баку", "Рязань", "Минск", "Киото"
    ];

    function hashStr(str) {
        let h = 0x811c9dc5;
        for (let i = 0; i < str.length; i++) {
            h ^= str.charCodeAt(i);
            h = Math.imul(h, 0x01000193);
        }
        return h >>> 0;
    }

    function isLegacyTwoWordCallsign(name) {
        if (!name || typeof name !== 'string') return false;
        const parts = name.trim().split(/\s+/);
        if (parts.length !== 2) return false;
        return NOBEL_LAUREATES.includes(parts[0]) && NOBEL_CITIES.includes(parts[1]);
    }

    function getDeterministicNobelCallsign(seed, usedSet = null) {
        if (!seed) return "Оператор 101";
        let attempt = 0;
        while (attempt < 1000) {
            const salt = attempt === 0 ? "_v2" : `_attempt_${attempt}`;
            const h1 = hashStr(seed + "_laureate" + salt);
            const h2 = hashStr(seed + "_city" + salt);
            const h3 = hashStr(seed + "_num" + salt);
            const lau = NOBEL_LAUREATES[h1 % NOBEL_LAUREATES.length];
            const city = NOBEL_CITIES[h2 % NOBEL_CITIES.length];
            const num = (h3 % 900) + 100; // 100..999 (strictly 3 digits)
            const candidate = `${lau} ${city} ${num}`;
            if (!usedSet || !usedSet.has(candidate)) {
                if (usedSet) usedSet.add(candidate);
                return candidate;
            }
            attempt++;
        }
        return "Оператор 999";
    }

    window.NobelCallsigns = {
        getDeterministic: getDeterministicNobelCallsign,
        isLegacyTwoWord: isLegacyTwoWordCallsign,
        sanitizeNickname: function(nick, accNumber, usedSet = null) {
            if (!nick || nick.includes('****') || /^\d{4}-/.test(nick) || nick === 'Спонсор WarLink' || isLegacyTwoWordCallsign(nick)) {
                return accNumber ? getDeterministicNobelCallsign(accNumber, usedSet) : 'Оператор 101';
            }
            if (usedSet) usedSet.add(nick);
            return nick;
        }
    };
})();
