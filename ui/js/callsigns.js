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

    function getDeterministicNobelCallsign(seed) {
        if (!seed) return "Аноним";
        const h1 = hashStr(seed + "_laureate");
        const h2 = hashStr(seed + "_city");
        const lau = NOBEL_LAUREATES[h1 % NOBEL_LAUREATES.length];
        const city = NOBEL_CITIES[h2 % NOBEL_CITIES.length];
        return lau + " " + city;
    }

    window.NobelCallsigns = {
        getDeterministic: getDeterministicNobelCallsign,
        sanitizeNickname: function(nick, accNumber) {
            if (!nick || nick.includes('****') || /^\d{4}-/.test(nick) || nick === 'Спонсор WarLink') {
                return accNumber ? getDeterministicNobelCallsign(accNumber) : 'Аноним';
            }
            return nick;
        }
    };
})();
