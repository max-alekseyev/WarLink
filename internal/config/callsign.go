package config

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand"
	"time"
)

// NobelLaureates provides a curated list of Nobel laureates (Russian transcription)
var NobelLaureates = []string{
	"Рентген", "Кюри", "Бор", "Планк", "Эйнштейн",
	"Ферми", "Гейзенберг", "Шрёдингер", "Дирак", "Паули",
	"Фейнман", "Ландау", "Капица", "Сахаров", "Семенов",
	"Басов", "Прохоров", "Тамм", "Франк", "Черенков",
	"Алферов", "Гинзбург", "Абрикосов", "Павлов", "Мечников",
	"Резерфорд", "Борн", "Маркони", "Лоренц", "Лауэ",
	"Чедвик", "Юкава", "Купер", "Бардин", "Шокли",
	"Таунс", "Хиггс", "Пенроуз", "Гейм", "Новоселов",
}

// NobelCities provides a curated list of iconic scientific & cultural cities (Russian transcription)
var NobelCities = []string{
	"Стокгольм", "Осло", "Женева", "Берн", "Цюрих",
	"Мюнхен", "Вена", "Рим", "Париж", "Лондон",
	"Кембридж", "Оксфорд", "Лейпциг", "Геттинген", "Копенгаген",
	"Бостон", "Чикаго", "Принстон", "Москва", "Дубна",
	"Воронеж", "Баку", "Рязань", "Минск", "Киото",
}

// GenerateRandomNobelCallsign generates a random "Laureate City" callsign.
func GenerateRandomNobelCallsign() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	lau := NobelLaureates[r.Intn(len(NobelLaureates))]
	city := NobelCities[r.Intn(len(NobelCities))]
	return lau + " " + city
}

// GenerateDeterministicNobelCallsign generates a deterministic "Laureate City" callsign based on seed/account number.
func GenerateDeterministicNobelCallsign(seed string) string {
	if seed == "" {
		return GenerateRandomNobelCallsign()
	}
	h := sha256.Sum256([]byte(seed + "_wl_nobel_salt"))
	v1 := binary.BigEndian.Uint32(h[0:4])
	v2 := binary.BigEndian.Uint32(h[4:8])
	lau := NobelLaureates[int(v1)%len(NobelLaureates)]
	city := NobelCities[int(v2)%len(NobelCities)]
	return lau + " " + city
}
