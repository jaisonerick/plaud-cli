package speaker

import (
	"sort"
	"strings"
	"unicode"
)

// Fold reduces a name to what two spellings of the same person share: no case,
// no accents, no punctuation, single spaces.
func Fold(name string) string {
	var b strings.Builder
	lastSpace := true
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case unicode.IsSpace(r) || r == '-' || r == '_':
			if !lastSpace {
				b.WriteRune(' ')
				lastSpace = true
			}
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unaccent(r))
			lastSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}

func unaccent(r rune) rune {
	accented := []rune("áàâãäéèêëíìîïóòôõöúùûüçñ")
	plain := []rune("aaaaaeeeeiiiiooooouuuucn")
	for i, a := range accented {
		if a == r {
			return plain[i]
		}
	}
	return r
}

// Match is an existing name that a new one may be another spelling of.
type Match struct {
	Name string
	Same bool // the two fold to the same string, so nothing needs deciding
}

// Similar finds the existing names that `name` may be another spelling of,
// closest first. It exists because "jaison" and "Jaison Erick" are one person
// whom nothing mechanical will ever match, and each new spelling permanently
// splits that person's samples in two.
func Similar(name string, existing []string) []Match {
	target := Fold(name)
	if target == "" {
		return nil
	}
	targetTokens := strings.Fields(target)

	type scored struct {
		Match
		rank int
	}
	var found []scored

	for _, candidate := range existing {
		folded := Fold(candidate)
		if folded == "" {
			continue
		}
		switch {
		case folded == target:
			found = append(found, scored{Match{candidate, true}, 0})
		case sharesLeadingTokens(targetTokens, strings.Fields(folded)):
			found = append(found, scored{Match{candidate, false}, 1})
		case editDistance(target, folded) <= tolerance(target):
			found = append(found, scored{Match{candidate, false}, 2})
		}
	}

	sort.SliceStable(found, func(i, j int) bool { return found[i].rank < found[j].rank })
	matches := make([]Match, len(found))
	for i, s := range found {
		matches[i] = s.Match
	}
	return matches
}

// sharesLeadingTokens reports whether the shorter name is how someone writes
// the longer one in a hurry: "amanda" for "amanda destro".
func sharesLeadingTokens(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 || len(a) == len(b) {
		return false
	}
	short, long := a, b
	if len(a) > len(b) {
		short, long = b, a
	}
	for i, token := range short {
		if long[i] != token {
			return false
		}
	}
	return true
}

// tolerance allows a typo, and a second one once the name is long enough that
// two edits still leave it unmistakable.
func tolerance(name string) int {
	switch {
	case len(name) < 5:
		return 0
	case len(name) < 12:
		return 1
	default:
		return 2
	}
}

func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, min(curr[j-1]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}
