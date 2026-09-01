package identify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/jaisonerick/plaud-cli/internal/browser"
)

// Config is what the page needs to do its work, with the things it cannot do
// itself passed in: fetching a recording's audio, and settling with the
// service what each voice is.
type Config struct {
	Voices []Voice
	// Known is everybody the service already has, offered as you type so a
	// name is picked rather than spelt again. Two spellings of one person are
	// two people, and this is where that is cheapest to prevent.
	Known []string
	// Heard is what the service can already say about each voice, by Voice.Key.
	// A page that only asks is a page that makes somebody listen to a voice the
	// service could have placed, or type a name it holds for somebody else.
	Heard map[string]Heard
	// Threshold is the distance under which the service calls two voices the
	// same person, and is its own number rather than one repeated here.
	Threshold float64
	Audio     func(ctx context.Context, recording string) ([]byte, error)
	Name      func(ctx context.Context, v Voice, name, company string, surnameUnknown, despiteTimbre bool) (string, error)
	Outside   func(ctx context.Context, v Voice, reason string) error
}

// Resemblance is somebody a voice sounds like, and how far away the service
// puts them.
type Resemblance struct {
	Name     string  `json:"name"`
	Distance float64 `json:"distance"`
}

// Heard is what the service hears in one voice: who it sounds like, the other
// unnamed voices that are this same voice, and whether somebody has already
// ruled it out of the room.
type Heard struct {
	Outside   bool          `json:"-"`
	Resembles []Resemblance `json:"resembles"`
	// Same holds the keys of the other voices, which RunServer turns into the
	// positions the page works in.
	Same map[string]float64 `json:"-"`
}

// Card is one voice as the page shows it: what the transcripts say about it,
// and what the service hears in it.
type Card struct {
	Voice
	Resembles []Resemblance `json:"resembles"`
	// Same is the other cards holding this voice, nearest first. Naming one
	// offers the name to the rest rather than saving it for them: two voices
	// that measure alike are still a person's call, and how alike is what says
	// whether that call needs making.
	Same []SameVoice `json:"same"`
}

// SameVoice is another card the service hears as this same voice.
type SameVoice struct {
	Card     int     `json:"card"`
	Distance float64 `json:"distance"`
}

// Refused is a name the service would not take on the evidence it holds.
// Overridable says somebody can insist, which is a thing only whoever was in
// the room knows: the page has to offer that rather than read it out of a
// message.
type Refused struct {
	Reason      string
	Overridable bool
}

func (r Refused) Error() string { return r.Reason }

// Named is what the page settled: each voice and the person it now holds. A
// voice ruled out of the room holds nobody, and says so.
type Named []Settled

// Settled is one voice that stopped being SPEAKER_nn.
type Settled struct {
	Voice   Voice
	Person  string
	Outside bool
}

// Files is the transcripts a run touched, which are the ones worth rewriting.
func (n Named) Files() []string {
	seen := map[string]bool{}
	var files []string
	for _, settled := range n {
		if !seen[settled.Voice.File] {
			seen[settled.Voice.File] = true
			files = append(files, settled.Voice.File)
		}
	}
	return files
}

// RunServer opens the page and returns when whoever is using it is finished.
//
// Each name is registered the moment it is typed, rather than at the end. A
// browser tab closed halfway through then leaves the voices already named
// named, which for a page that exists to work through a list is the difference
// between an interruption and a wasted sitting.
func RunServer(ctx context.Context, cfg Config) (Named, error) {
	if len(cfg.Voices) == 0 {
		return Named{}, nil
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("starting listener: %w", err)
	}

	page := &pageServer{cfg: cfg, done: make(chan struct{}), audio: map[string][]byte{}}
	server := &http.Server{Handler: page.routes()}
	go server.Serve(listener)

	url := fmt.Sprintf("http://127.0.0.1:%d", listener.Addr().(*net.TCPAddr).Port)
	fmt.Printf("Naming %d voice(s) at %s\n", len(cfg.Voices), url)
	browser.Open(url)

	select {
	case <-page.done:
	case <-ctx.Done():
	}
	server.Close()

	page.mu.Lock()
	defer page.mu.Unlock()
	return page.named, nil
}

type pageServer struct {
	cfg   Config
	done  chan struct{}
	once  sync.Once
	mu    sync.Mutex
	named Named
	audio map[string][]byte
}

func (p *pageServer) routes() http.Handler {
	tmpl := template.Must(template.New("page").Parse(pageHTML))
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		voices, _ := json.Marshal(cards(p.cfg))
		known, _ := json.Marshal(p.cfg.Known)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, map[string]any{
			"VoicesJSON": template.JS(voices),
			"KnownJSON":  template.JS(known),
			"Threshold":  p.cfg.Threshold,
		})
	})

	mux.HandleFunc("GET /audio/{recording}", func(w http.ResponseWriter, r *http.Request) {
		recording := r.PathValue("recording")
		data, err := p.audioOf(r.Context(), recording)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		http.ServeContent(w, r, recording+".mp3", time.Time{}, bytes.NewReader(data))
	})

	mux.HandleFunc("POST /name", func(w http.ResponseWriter, r *http.Request) {
		var asked struct {
			Index          int    `json:"index"`
			Name           string `json:"name"`
			Company        string `json:"company"`
			SurnameUnknown bool   `json:"surname_unknown"`
			DespiteTimbre  bool   `json:"despite_timbre"`
		}
		if !readBody(w, r, &asked) {
			return
		}
		voice, ok := p.voiceAt(w, asked.Index)
		if !ok {
			return
		}

		display, err := p.cfg.Name(r.Context(), voice, asked.Name, asked.Company, asked.SurnameUnknown, asked.DespiteTimbre)
		if err != nil {
			var refused Refused
			answer(w, http.StatusOK, map[string]any{
				"error":       err.Error(),
				"overridable": errors.As(err, &refused) && refused.Overridable,
			})
			return
		}

		p.mu.Lock()
		p.named = append(p.named, Settled{Voice: voice, Person: display})
		p.mu.Unlock()
		answer(w, http.StatusOK, map[string]string{"named": display})
	})

	mux.HandleFunc("POST /outside", func(w http.ResponseWriter, r *http.Request) {
		var asked struct {
			Index  int    `json:"index"`
			Reason string `json:"reason"`
		}
		if !readBody(w, r, &asked) {
			return
		}
		voice, ok := p.voiceAt(w, asked.Index)
		if !ok {
			return
		}

		if err := p.cfg.Outside(r.Context(), voice, asked.Reason); err != nil {
			answer(w, http.StatusOK, map[string]string{"error": err.Error()})
			return
		}

		p.mu.Lock()
		p.named = append(p.named, Settled{Voice: voice, Outside: true})
		p.mu.Unlock()
		answer(w, http.StatusOK, map[string]string{"outside": voice.Label})
	})

	mux.HandleFunc("POST /done", func(w http.ResponseWriter, r *http.Request) {
		answer(w, http.StatusOK, map[string]string{"status": "ok"})
		p.once.Do(func() { close(p.done) })
	})

	return mux
}

// cards pairs each voice with what the service hears in it, and turns the
// voices it hears together into the positions the page addresses them by.
func cards(cfg Config) []Card {
	at := make(map[string]int, len(cfg.Voices))
	for i, voice := range cfg.Voices {
		at[voice.Key()] = i
	}

	built := make([]Card, len(cfg.Voices))
	for i, voice := range cfg.Voices {
		heard := cfg.Heard[voice.Key()]

		nearest := make([]string, 0, len(heard.Same))
		for key := range heard.Same {
			if _, shown := at[key]; shown {
				nearest = append(nearest, key)
			}
		}
		sort.Slice(nearest, func(a, b int) bool {
			return heard.Same[nearest[a]] < heard.Same[nearest[b]]
		})

		card := Card{Voice: voice, Resembles: heard.Resembles}
		for _, key := range nearest {
			card.Same = append(card.Same, SameVoice{Card: at[key], Distance: heard.Same[key]})
		}
		built[i] = card
	}
	return built
}

// voiceAt is the voice a request names, or the refusal for one naming none.
func (p *pageServer) voiceAt(w http.ResponseWriter, index int) (Voice, bool) {
	if index < 0 || index >= len(p.cfg.Voices) {
		answer(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return Voice{}, false
	}
	return p.cfg.Voices[index], true
}

func readBody(w http.ResponseWriter, r *http.Request, into any) bool {
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		answer(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return false
	}
	return true
}

// audioOf downloads a recording once and keeps it for as long as the page is
// open, since every voice in one recording plays out of the same file.
func (p *pageServer) audioOf(ctx context.Context, recording string) ([]byte, error) {
	p.mu.Lock()
	held, ok := p.audio[recording]
	p.mu.Unlock()
	if ok {
		return held, nil
	}

	data, err := p.cfg.Audio(ctx, recording)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.audio[recording] = data
	p.mu.Unlock()
	return data, nil
}

func answer(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
