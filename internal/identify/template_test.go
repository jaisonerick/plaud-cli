package identify

import (
	"math"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func renderedPage(t *testing.T) string {
	t.Helper()
	page := &pageServer{cfg: Config{Voices: twoVoices(), Threshold: 0.35}}
	recorder := httptest.NewRecorder()
	page.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	return recorder.Body.String()
}

// tokensIn reads the --name: #hex declarations of the CSS block that opens with opening.
func tokensIn(t *testing.T, page, opening string) map[string]string {
	t.Helper()
	start := strings.Index(page, opening)
	if start < 0 {
		t.Fatalf("the page has no block opening with %q", opening)
	}
	end := strings.Index(page[start:], "}")
	tokens := map[string]string{}
	for _, found := range regexp.MustCompile(`(--[\w-]+):\s*(#[0-9a-f]+)`).FindAllStringSubmatch(page[start:start+end], -1) {
		tokens[found[1]] = found[2]
	}
	return tokens
}

func luminance(t *testing.T, color string) float64 {
	t.Helper()
	digits := strings.TrimPrefix(color, "#")
	if len(digits) == 3 {
		digits = string([]byte{digits[0], digits[0], digits[1], digits[1], digits[2], digits[2]})
	}
	var linear [3]float64
	for i := range linear {
		channel, err := strconv.ParseUint(digits[i*2:i*2+2], 16, 8)
		if err != nil {
			t.Fatalf("%q is not a color: %v", color, err)
		}
		value := float64(channel) / 255
		if value <= 0.04045 {
			linear[i] = value / 12.92
		} else {
			linear[i] = math.Pow((value+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*linear[0] + 0.7152*linear[1] + 0.0722*linear[2]
}

func contrast(t *testing.T, foreground, background string) float64 {
	t.Helper()
	lighter, darker := luminance(t, foreground), luminance(t, background)
	if darker > lighter {
		lighter, darker = darker, lighter
	}
	return (lighter + 0.05) / (darker + 0.05)
}

func TestThePageAppliesASavedThemeChoiceBeforeItsFirstStyle(t *testing.T) {
	page := renderedPage(t)

	if !strings.Contains(page, `<meta name="color-scheme" content="light dark">`) {
		t.Error("the page does not say it has both a light and a dark scheme")
	}
	saved := strings.Index(page, "localStorage.getItem('theme')")
	if saved < 0 || saved > strings.Index(page, "<style>") {
		t.Error("a saved theme choice is not applied before the first style, so a dark choice would flash light")
	}
}

func TestTheThemeButtonSitsInTheHeaderAfterTheTitle(t *testing.T) {
	page := renderedPage(t)

	header := regexp.MustCompile(`<header class="top">\s*<h1>[^<]*</h1>\s*<button type="button" class="theme-toggle`)
	if !header.MatchString(page) {
		t.Error("the theme button is not a button in the header after the title")
	}
}

func TestTheSystemAndExplicitDarkBlocksCarryTheSameTokens(t *testing.T) {
	page := renderedPage(t)

	system := tokensIn(t, page, `:root:where(:not([data-theme="light"])) {`)
	explicit := tokensIn(t, page, `:root[data-theme="dark"] {`)

	if len(system) == 0 || len(system) != len(explicit) {
		t.Fatalf("system dark has %d tokens and explicit dark %d", len(system), len(explicit))
	}
	for name, value := range system {
		if explicit[name] != value {
			t.Errorf("%s is %s for the system and %s for the explicit choice", name, value, explicit[name])
		}
	}
}

func TestTextMeetsItsContrastFloorOnTheSurfaceItSitsOnInBothThemes(t *testing.T) {
	page := renderedPage(t)
	themes := map[string]map[string]string{
		"light": tokensIn(t, page, ":root {"),
		"dark":  tokensIn(t, page, `:root[data-theme="dark"] {`),
	}
	floors := []struct {
		foreground, background string
		floor                  float64
	}{
		{"--ink", "--bg", 7}, {"--ink", "--card", 7}, {"--ink", "--hint", 7},
		{"--muted", "--bg", 6}, {"--muted", "--card", 6},
		{"--accent", "--bg", 4.5}, {"--accent", "--card", 4.5},
		{"--ok", "--card", 4.5}, {"--bad", "--card", 4.5},
		{"--on-accent", "--accent", 4.5}, {"--on-bad", "--bad", 4.5},
	}

	for theme, tokens := range themes {
		for _, pair := range floors {
			got := contrast(t, tokens[pair.foreground], tokens[pair.background])
			if got < pair.floor {
				t.Errorf("%s: %s on %s is %.2f, below %.1f", theme, pair.foreground, pair.background, got, pair.floor)
			}
		}
	}
}
