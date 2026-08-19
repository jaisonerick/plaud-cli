package speaker

import "testing"

func TestFoldIgnoresCaseAccentsAndPunctuation(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Jaison Erick", "jaison erick"},
		{"  JAISON   erick ", "jaison erick"},
		{"Priscilla - Dinie", "priscilla dinie"},
		{"Cíntia", "cintia"},
		{"Mauricio", "mauricio"},
	} {
		if got := Fold(tc.in); got != tc.want {
			t.Errorf("Fold(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSimilarFindsAShorteningOfAKnownName(t *testing.T) {
	known := []string{"Jaison Erick", "Amanda Destro", "Mauricio Dinie", "Danilo"}

	for _, tc := range []struct{ name, want string }{
		{"jaison", "Jaison Erick"},
		{"Amanda", "Amanda Destro"},
		{"Mauricio", "Mauricio Dinie"},
	} {
		matches := Similar(tc.name, known)
		if len(matches) == 0 {
			t.Fatalf("Similar(%q) found nothing, want %q", tc.name, tc.want)
		}
		if matches[0].Name != tc.want {
			t.Errorf("Similar(%q) = %q, want %q", tc.name, matches[0].Name, tc.want)
		}
		if matches[0].Same {
			t.Errorf("Similar(%q) called %q the same spelling", tc.name, tc.want)
		}
	}
}

func TestSimilarReportsTheSameNameSpeltDifferently(t *testing.T) {
	matches := Similar("jaison erick", []string{"Jaison Erick"})
	if len(matches) != 1 || !matches[0].Same {
		t.Fatalf("Similar found %v, want one match marked as the same name", matches)
	}
}

func TestSimilarCatchesATypo(t *testing.T) {
	matches := Similar("Amanda Destroo", []string{"Amanda Destro"})
	if len(matches) == 0 || matches[0].Name != "Amanda Destro" {
		t.Errorf("Similar found %v, want Amanda Destro", matches)
	}
}

func TestSimilarLeavesDistinctPeopleAlone(t *testing.T) {
	known := []string{"Jaison Erick", "Amanda Destro", "Danilo", "Zeni"}
	for _, name := range []string{"Cintia", "Luiz", "Priscilla - Dinie", "Debora"} {
		if matches := Similar(name, known); len(matches) != 0 {
			t.Errorf("Similar(%q) = %v, want nothing", name, matches)
		}
	}
}

func TestSimilarKeepsShortNamesApart(t *testing.T) {
	// "Luiz" and "Livia" are four and five letters; a tolerance that merged
	// them would merge most first names in the book.
	if matches := Similar("Luiz", []string{"Livia"}); len(matches) != 0 {
		t.Errorf("Similar(Luiz) = %v, want nothing", matches)
	}
}

func TestIsFullWantsMoreThanAFirstName(t *testing.T) {
	for _, name := range []string{"Jaison Erick", "Amanda Destro", "Priscilla - Dinie", "rodrigo silva", "Urias Hobaik - Afinz"} {
		if !IsFull(name) {
			t.Errorf("IsFull(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"Amanda", "luca", "Vic", "Tom", "Zeni", "  Cintia  ", ""} {
		if IsFull(name) {
			t.Errorf("IsFull(%q) = true, want false", name)
		}
	}
}
