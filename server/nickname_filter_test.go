package main

import (
	"context"
	"os"
	"sync"
	"testing"
)

func TestFastValidateNicknameValid(t *testing.T) {
	valid := []string{
		"CyberWolf",
		"Танкист_777",
		"Shadow-Sniper",
		"Котик",
		"Max_Pro",
		"WardogPlayer",
		"StarGazer",
		"Phoenix",
		// Scunthorpe safe words
		"Assassin",
		"ClassicGamer",
		"Мудрый_Волк",
		"Парикмахер",
		"Колебания",
		"Скипидар",
	}

	for _, name := range valid {
		if err := FastValidateNickname(name); err != nil {
			t.Errorf("expected valid nickname %q, but got error: %v", name, err)
		}
	}
}

func TestFastValidateNicknameForbidden(t *testing.T) {
	forbidden := []string{
		// Impersonation
		"admin",
		"Admin_WarLink",
		"WarLink_Support",
		"aeza_official",
		"root_user",
		"модератор",
		"Техподдержка",
		// Mixed-script spoofing
		"aдмин",       // Latin 'a' with Cyrillic 'дмин'
		"WаrLink",     // Cyrillic 'а' with Latin 'WrLink'
		"Aeзa",        // Cyrillic 'з'
		// Obscenity & stretching
		"хххуууййй",
		"п_и_з_д_а",
		"ебать_вардогс",
		"ссууккаа",
		"пидорас",
		"чмо",
		"гандон",
		"ублюдок",
		// Translit & Leetspeak
		"xui",
		"p1zd@",
		"eblan",
		"blyad",
		"suka_blyat",
		// English slurs & profanity
		"fuck_you",
		"b!tch",
		"nigger",
		"n1gg3r",
		// Nazi codes
		"1488_gamer",
		"user_14/88",
		"pro_c18",
		// Mother insults
		"mq_player",
		"mky",
	}

	for _, name := range forbidden {
		if err := FastValidateNickname(name); err == nil {
			t.Errorf("expected nickname %q to be rejected, but it PASSED", name)
		}
	}
}

func TestValidateNicknameHybridWithGemini(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("skipping Gemini AI test: GEMINI_API_KEY not set")
	}
	var cache sync.Map

	// 1. Valid nickname should pass AI
	err := ValidateNicknameHybrid(context.Background(), "QuantumRanger", apiKey, &cache)
	if err != nil {
		t.Fatalf("expected QuantumRanger to pass, got: %v", err)
	}

	// 2. Toxic / veiled offensive nickname that might evade simple regex should be caught by AI
	veiledBad := "kill_urself_now"
	err = ValidateNicknameHybrid(context.Background(), veiledBad, apiKey, &cache)
	if err == nil {
		t.Fatalf("expected veiled bad nickname %q to be caught by Gemini AI, but it passed", veiledBad)
	}
	t.Logf("Successfully caught veiled bad nickname %q: %v", veiledBad, err)
}
