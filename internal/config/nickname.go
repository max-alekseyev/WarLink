package config

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	zeroWidthReplacer = strings.NewReplacer(
		"\u200B", "", // Zero Width Space
		"\u200C", "", // Zero Width Non-Joiner
		"\u200D", "", // Zero Width Joiner
		"\uFEFF", "", // Zero Width No-Break Space / BOM
		"\u00AD", "", // Soft Hyphen
		"\u202E", "", // Right-to-Left Override
		"\u202D", "", // Left-to-Right Override
		"\u2060", "", // Word Joiner
		"\u200E", "", // Left-to-Right Mark
		"\u200F", "", // Right-to-Left Mark
	)

	// Safe words whitelist (Scunthorpe problem prevention)
	safeWordsWhitelist = []string{
		"колебан", "загребать", "огребать", "хлеб", "стебель", "грабеж", "соскребать",
		"употреблять", "оскорблять", "влюбляться", "сабля", "рубль", "рубля", "гребля",
		"ссуда", "пассат", "скипидар", "застраховать", "художник", "худой", "хутор",
		"педикюр", "википедия", "ортопедик", "барсук", "посукно", "мудрый", "мудрец",
		"мудрость", "замудреный", "изумруд", "парикмахер", "парикмахерская", "чмоканье",
		"пассаж", "касса", "трасса", "масса", "класс", "колосс", "сша", "высший",
		"classic", "assassin", "cocktail", "cockpit", "dickens", "tombstone", "basement",
		"therapist", "pushing", "butter", "title", "titov", "document", "button",
	}

	reservedImpersonation = []string{
		"admin", "administrator", "админ", "администратор",
		"warlink", "варлинк", "aeza", "аеза", "stockholm", "frankfurt",
		"hysteria", "singbox", "wintun",
		"support", "саппорт", "техподдержка", "поддержка",
		"moderator", "модератор", "root", "system", "систем", "рут",
		"developer", "разработчик", "owner", "владелец", "creator", "создатель",
		"official", "официальный", "security", "безопасность",
		"helpdesk", "billing", "donate", "billing_bot", "payment",
		"valve", "steam", "discord", "telegram",
	}

	forbiddenProfanities = []string{
		// Russian basic obscenity roots
		"хуй", "хуе", "хуя", "хули", "хуло", "хуем", "хуяр", "хер", "хера", "херов",
		"пизд", "пезд", "пизда", "пиздец", "пиздук", "пиздобол", "спиздил",
		"ебат", "ебан", "ебли", "ебл", "ебок", "ебуч", "ебаш", "заеб", "выеб", "уеб", "въеб", "доеб", "приеб", "наеб", "поеб", "проеб", "долбоеб",
		"бляд", "блят", "бля", "блядин", "блядств",
		"мудак", "мудил", "мудозвон", "муде",
		// Severe insults & platform bans
		"сука", "сучк", "сучар", "сучий", "ссука",
		"гандон", "гондон", "шлюх", "шлюшк", "шалав", "шаболд", "шмар", "курв", "давалк",
		"чмо", "чмошник", "чмоня", "залуп", "говно", "гавно", "говноед", "говниш", "дерьмо", "дрисн",
		"пидор", "пидар", "педик", "пидрил", "педрил", "глиномес", "петушар",
		"ублюд", "вырод", "выбляд", "гнид", "мраз", "мразот", "падл", "тварь",
		"жопа", "дроч", "сись", "манда", "целк",
		// Ethnofaulisms & extremism
		"хач", "хачик", "чурк", "чурбан", "хохол", "хохл", "москал", "жид", "пархат", "кацап", "русн",
		"зига", "зигхайл", "гитлер", "hitler", "nazi", "reich", "рейх", "whitepower",
		// Toxic gaming abbreviations & parents insults
		"мамоеб", "мамкоеб", "сыниншлюх", "сыншлюх",
		// Latin translit profanities
		"hui", "xui", "xyu", "xuy", "hooy", "hyi", "xuj", "xyj",
		"pizd", "pezd", "ebat", "eban", "ebal", "eblan", "eblo", "zaebal", "yebal", "jeban",
		"blyad", "blyat", "bliad", "bliat", "blya", "bleat",
		"suka", "cyka", "mudak", "mydak", "gandon", "gavno", "govno", "pidor", "pidoras", "pedik", "zalup",
		"shlyuh", "shalav", "ublyud", "chmo",
		// English profanity & severe slurs
		"fuck", "shit", "bitch", "cunt", "dick", "asshole",
		"nigger", "nigga", "niger", "niga", "nigr", "n1gger", "n1gga", "niggas", "niggers",
		"whore", "bastard", "cock", "fag", "faggot", "slut", "retard", "kike", "chink", "spic", "wetback",
		"twat", "wank", "motherfucker", "dipshit", "jackass",
	}

	naziNumericPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:^|[^0-9])14[_\-\s/]?88(?:[^0-9]|$)`),
		regexp.MustCompile(`(?i)(?:^|[^0-9])88[_\-\s/]?14(?:[^0-9]|$)`),
		regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:c18|combat18)(?:[^a-z0-9]|$)`),
	}
)

func StripInvisibleCharacters(s string) string {
	return zeroWidthReplacer.Replace(s)
}

func CheckMixedScript(nick string) error {
	words := strings.FieldsFunc(nick, func(r rune) bool {
		return r == ' ' || r == '-' || r == '_' || r == '.'
	})

	for _, w := range words {
		hasLatin := false
		hasCyrillic := false
		for _, r := range w {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				hasLatin = true
			} else if (r >= 'а' && r <= 'я') || (r >= 'А' && r <= 'Я') || r == 'ё' || r == 'Ё' {
				hasCyrillic = true
			}
			if hasLatin && hasCyrillic {
				return fmt.Errorf("смешивание латиницы и кириллицы в одном слове запрещено для защиты от подделки имен")
			}
		}
	}
	return nil
}

func CollapseRepeatedRunes(s string, maxKeep int) string {
	if len(s) == 0 || maxKeep < 1 {
		return s
	}
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(runes))

	var lastR rune
	count := 0

	for _, r := range runes {
		if r == lastR {
			count++
			if count <= maxKeep {
				b.WriteRune(r)
			}
		} else {
			lastR = r
			count = 1
			b.WriteRune(r)
		}
	}
	return b.String()
}

func UnrollLigatures(s string) string {
	r := strings.NewReplacer(
		"vv", "w",
		"rn", "m",
		"cl", "d",
	)
	return r.Replace(s)
}

func NormalizeNicknameVariants(nick string) []string {
	lower := strings.ToLower(StripInvisibleCharacters(nick))

	var clean strings.Builder
	for _, r := range lower {
		if r != ' ' && r != '-' && r != '_' && r != '.' && r != ',' && r != '~' && r != '\'' && r != '"' && r != '/' && r != '\\' && r != '+' {
			clean.WriteRune(r)
		}
	}
	sClean := clean.String()

	sCollapsed1 := CollapseRepeatedRunes(sClean, 1)
	sCollapsed2 := CollapseRepeatedRunes(sClean, 2)
	sUnrolled := UnrollLigatures(sCollapsed1)

	leetReplacer := strings.NewReplacer(
		"0", "o",
		"1", "i", "!", "i", "|", "i",
		"3", "e",
		"4", "a", "@", "a",
		"5", "s", "$", "s",
		"7", "t",
		"8", "b",
		"9", "g",
	)

	cyrToLat := strings.NewReplacer(
		"а", "a", "в", "b", "е", "e", "к", "k", "м", "m",
		"н", "h", "о", "o", "р", "p", "с", "c", "т", "t",
		"у", "y", "х", "x", "і", "i",
	)

	latToCyr := strings.NewReplacer(
		"a", "а", "b", "в", "e", "е", "k", "к", "m", "м",
		"h", "н", "o", "о", "p", "р", "c", "с", "t", "т",
		"y", "у", "x", "х", "i", "и",
	)

	sLeetClean := leetReplacer.Replace(sClean)
	sLatClean := cyrToLat.Replace(sLeetClean)
	sCyrClean := latToCyr.Replace(sLeetClean)

	sLeet1 := leetReplacer.Replace(sCollapsed1)
	sLeetUnrolled := leetReplacer.Replace(sUnrolled)

	sLat1 := cyrToLat.Replace(sLeet1)
	sLatUnrolled := cyrToLat.Replace(sLeetUnrolled)

	sCyr1 := latToCyr.Replace(sLeet1)
	sCyrUnrolled := latToCyr.Replace(sLeetUnrolled)

	return []string{
		lower,
		sClean,
		sLeetClean,
		sLatClean,
		sCyrClean,
		sCollapsed1,
		sCollapsed2,
		sLeet1,
		sLeetUnrolled,
		sLat1,
		sLatUnrolled,
		sCyr1,
		sCyrUnrolled,
	}
}

func IsWhitelisted(token string) bool {
	tokenLower := strings.ToLower(token)
	for _, safe := range safeWordsWhitelist {
		if strings.Contains(tokenLower, safe) {
			return true
		}
	}
	return false
}

// ValidateNickname checks length, character set, mixed script, impersonation, profanity, homoglyphs and leetspeak.
func ValidateNickname(nick string) error {
	cleaned := StripInvisibleCharacters(strings.TrimSpace(nick))
	if cleaned == "" {
		return nil
	}

	runes := []rune(cleaned)
	if len(runes) < 2 || len(runes) > 20 {
		return fmt.Errorf("длина никнейма должна быть от 2 до 20 символов")
	}

	hasLetterOrDigit := false
	for _, r := range runes {
		isLatin := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		isCyrillic := (r >= 'а' && r <= 'я') || (r >= 'А' && r <= 'Я') || r == 'ё' || r == 'Ё'
		isDigit := r >= '0' && r <= '9'
		isSep := r == ' ' || r == '-' || r == '_'
		if !isLatin && !isCyrillic && !isDigit && !isSep {
			return fmt.Errorf("никнейм может содержать только буквы, цифры, дефис и подчеркивание")
		}
		if isLatin || isCyrillic || isDigit {
			hasLetterOrDigit = true
		}
	}
	if !hasLetterOrDigit {
		return fmt.Errorf("никнейм должен содержать буквы или цифры")
	}

	if err := CheckMixedScript(cleaned); err != nil {
		return err
	}

	for _, p := range naziNumericPatterns {
		if p.MatchString(cleaned) {
			return fmt.Errorf("никнейм содержит экстремистские или запрещенные числовые коды")
		}
	}

	lowerClean := strings.ToLower(cleaned)
	if regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:mq|mky|m-q|m_q)(?:[^a-z0-9]|$)`).MatchString(lowerClean) {
		return fmt.Errorf("никнейм содержит токсичные игровые оскорбления")
	}

	variants := NormalizeNicknameVariants(cleaned)

	for _, v := range variants {
		for _, imp := range reservedImpersonation {
			if strings.Contains(v, imp) {
				return fmt.Errorf("этот никнейм зарезервирован администрацией WarLink")
			}
		}
	}

	if IsWhitelisted(cleaned) {
		return nil
	}

	for _, v := range variants {
		for _, prof := range forbiddenProfanities {
			if strings.Contains(v, prof) {
				if IsWhitelisted(v) || IsWhitelisted(cleaned) {
					continue
				}
				return fmt.Errorf("никнейм содержит недопустимые или нецензурные выражения")
			}
		}
	}

	return nil
}
