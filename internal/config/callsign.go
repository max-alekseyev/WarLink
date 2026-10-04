package config

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
	"strings"
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

// GenerateRandomNobelCallsign generates a random "Laureate City 123" callsign.
func GenerateRandomNobelCallsign() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	lau := NobelLaureates[r.Intn(len(NobelLaureates))]
	city := NobelCities[r.Intn(len(NobelCities))]
	num := r.Intn(900) + 100 // 100..999
	return fmt.Sprintf("%s %s %d", lau, city, num)
}

// GenerateDeterministicNobelCallsign generates a deterministic "Laureate City 123" callsign based on seed/account number.
func GenerateDeterministicNobelCallsign(seed string) string {
	if seed == "" {
		return GenerateRandomNobelCallsign()
	}
	h := sha256.Sum256([]byte(seed + "_wl_nobel_salt_v2"))
	v1 := binary.BigEndian.Uint32(h[0:4])
	v2 := binary.BigEndian.Uint32(h[4:8])
	v3 := binary.BigEndian.Uint32(h[8:12])
	lau := NobelLaureates[int(v1)%len(NobelLaureates)]
	city := NobelCities[int(v2)%len(NobelCities)]
	num := int(v3%900) + 100 // 100..999 (always 3 digits)
	return fmt.Sprintf("%s %s %d", lau, city, num)
}

// GenerateUniqueNobelCallsign generates a deterministic callsign ensuring it doesn't collide with used set.
func GenerateUniqueNobelCallsign(seed string, used map[string]bool) string {
	candidate := GenerateDeterministicNobelCallsign(seed)
	if used == nil || !used[candidate] {
		return candidate
	}
	// On collision, deterministically cycle salt attempts until strictly unique
	for attempt := 1; attempt <= 1000; attempt++ {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s_salt_attempt_%d", seed, attempt)))
		v1 := binary.BigEndian.Uint32(h[0:4])
		v2 := binary.BigEndian.Uint32(h[4:8])
		v3 := binary.BigEndian.Uint32(h[8:12])
		lau := NobelLaureates[int(v1)%len(NobelLaureates)]
		city := NobelCities[int(v2)%len(NobelCities)]
		num := int(v3%900) + 100
		candidate = fmt.Sprintf("%s %s %d", lau, city, num)
		if !used[candidate] {
			return candidate
		}
	}
	return candidate
}

// IsOldTwoWordNobelCallsign returns true if nick is a legacy 2-word Nobel callsign without 3 digits.
func IsOldTwoWordNobelCallsign(nick string) bool {
	parts := strings.Fields(strings.TrimSpace(nick))
	if len(parts) != 2 {
		return false
	}
	lauFound := false
	for _, l := range NobelLaureates {
		if parts[0] == l {
			lauFound = true
			break
		}
	}
	if !lauFound {
		return false
	}
	for _, c := range NobelCities {
		if parts[1] == c {
			return true
		}
	}
	return false
}
