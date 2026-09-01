package identify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func twoVoices() []Voice {
	return []Voice{
		{Recording: "rec1", ID: "v_aaa", Label: "SPEAKER_00", File: "a.md"},
		{Recording: "rec1", ID: "v_bbb", Label: "SPEAKER_01", File: "a.md"},
		{Recording: "rec2", ID: "v_ccc", Label: "SPEAKER_00", File: "b.md"},
	}
}

func TestCardsTurnVoiceKeysIntoThePositionsThePageWorksIn(t *testing.T) {
	voices := twoVoices()
	cfg := Config{
		Voices: voices,
		Heard: map[string]Heard{
			voices[0].Key(): {Same: map[string]float64{voices[2].Key(): 0.11}},
		},
	}

	built := cards(cfg)

	if len(built[0].Same) != 1 || built[0].Same[0].Card != 2 || built[0].Same[0].Distance != 0.11 {
		t.Errorf("the voice heard as the same one is at %v, want [2]", built[0].Same)
	}
	if len(built[1].Same) != 0 {
		t.Errorf("a voice nothing was heard about was given company: %v", built[1].Same)
	}
}

func TestCardsOfferTheNearestVoiceFirst(t *testing.T) {
	voices := twoVoices()
	cfg := Config{
		Voices: voices,
		Heard: map[string]Heard{
			voices[0].Key(): {Same: map[string]float64{
				voices[1].Key(): 0.30,
				voices[2].Key(): 0.08,
			}},
		},
	}

	built := cards(cfg)

	if len(built[0].Same) != 2 || built[0].Same[0].Card != 2 {
		t.Errorf("the same voices came back as %v, want the nearest first", built[0].Same)
	}
}

// A voice the service could say nothing about is still a voice to name: the
// page is the way in even when the comparison is not there.
func TestCardsSurviveAServiceThatSaidNothing(t *testing.T) {
	built := cards(Config{Voices: twoVoices()})

	if len(built) != 3 {
		t.Fatalf("built %d card(s) with nothing heard", len(built))
	}
	if built[0].Label != "SPEAKER_00" {
		t.Errorf("the voice itself was lost: %+v", built[0])
	}
}

func TestThePageRendersEveryVoiceItWasGiven(t *testing.T) {
	voices := twoVoices()
	page := &pageServer{cfg: Config{
		Voices:    voices,
		Known:     []string{"Jaison Erick (NexaEdge)"},
		Threshold: 0.35,
		Heard: map[string]Heard{
			voices[0].Key(): {Resembles: []Resemblance{{Name: "Jaison Erick (NexaEdge)", Distance: 0.2}}},
		},
	}}

	recorder := httptest.NewRecorder()
	page.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	body := recorder.Body.String()
	if !strings.Contains(body, "SPEAKER_01") {
		t.Error("a voice was not written into the page")
	}
	if !strings.Contains(body, "Jaison Erick (NexaEdge)") {
		t.Error("what the service hears was not written into the page")
	}
	if !strings.Contains(body, "const THRESHOLD =  0.35 ") {
		t.Error("the page was left to hold its own idea of the same voice")
	}
}

func TestNamingSaysWhenSomebodyCanInsist(t *testing.T) {
	page := &pageServer{cfg: Config{
		Voices: twoVoices(),
		Name: func(context.Context, Voice, string, string, bool, bool) (string, error) {
			return "", Refused{Reason: "this voice is somebody else", Overridable: true}
		},
	}}

	answer := post(t, page, "/name", `{"index":0,"name":"Jaison Erick","company":"NexaEdge"}`)

	if answer["overridable"] != true {
		t.Errorf("a refusal only the person in the room can overrule was not offered: %v", answer)
	}
}

func TestNamingDoesNotOfferToOverruleWhatCannotBeOverruled(t *testing.T) {
	page := &pageServer{cfg: Config{
		Voices: twoVoices(),
		Name: func(context.Context, Voice, string, string, bool, bool) (string, error) {
			return "", Refused{Reason: "a company is required"}
		},
	}}

	answer := post(t, page, "/name", `{"index":0,"name":"Jaison Erick","company":""}`)

	if answer["overridable"] != false {
		t.Errorf("a refusal nobody can overrule was offered as one: %v", answer)
	}
}

func TestAVoiceRuledOutOfTheRoomIsSettledWithoutAPerson(t *testing.T) {
	var told string
	page := &pageServer{cfg: Config{
		Voices: twoVoices(),
		Outside: func(_ context.Context, v Voice, reason string) error {
			told = reason
			return nil
		},
	}}

	post(t, page, "/outside", `{"index":1,"reason":"garcom"}`)

	if told != "garcom" {
		t.Errorf("the reason reached the service as %q", told)
	}
	if len(page.named) != 1 || !page.named[0].Outside || page.named[0].Person != "" {
		t.Errorf("a voice ruled out came back as %+v", page.named)
	}
	if page.named[0].Voice.Label != "SPEAKER_01" {
		t.Errorf("the wrong voice was ruled out: %q", page.named[0].Voice.Label)
	}
}

func TestARequestNamingNoVoiceIsRefused(t *testing.T) {
	page := &pageServer{cfg: Config{Voices: twoVoices()}}

	recorder := httptest.NewRecorder()
	page.routes().ServeHTTP(recorder,
		httptest.NewRequest(http.MethodPost, "/outside", strings.NewReader(`{"index":9,"reason":"x"}`)))

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("a voice that does not exist answered %d", recorder.Code)
	}
}

func post(t *testing.T, page *pageServer, path, body string) map[string]any {
	t.Helper()
	recorder := httptest.NewRecorder()
	page.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))

	var answer map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &answer); err != nil {
		t.Fatalf("reading the answer to %s: %v (%s)", path, err, recorder.Body)
	}
	return answer
}
