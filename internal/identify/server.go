package identify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/jaisonerick/plaud-cli/internal/browser"
)

// Config is what the page needs to do its work, with the two things it cannot
// do itself passed in: fetching a recording's audio, and telling the service
// who a voice is.
type Config struct {
	Voices []Voice
	// Known is everybody the service already has, offered as you type so a
	// name is picked rather than spelt again. Two spellings of one person are
	// two people, and this is where that is cheapest to prevent.
	Known []string
	Audio func(ctx context.Context, recording string) ([]byte, error)
	Name  func(ctx context.Context, v Voice, name, company string, surnameUnknown bool) (string, error)
}

// Named is what the page settled: each voice and the person it now holds.
type Named []Settled

// Settled is one voice that stopped being SPEAKER_nn.
type Settled struct {
	Voice  Voice
	Person string
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
		voices, _ := json.Marshal(p.cfg.Voices)
		known, _ := json.Marshal(p.cfg.Known)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, map[string]any{
			"VoicesJSON": template.JS(voices),
			"KnownJSON":  template.JS(known),
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
		}
		if err := json.NewDecoder(r.Body).Decode(&asked); err != nil || asked.Index < 0 || asked.Index >= len(p.cfg.Voices) {
			answer(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
			return
		}

		voice := p.cfg.Voices[asked.Index]
		display, err := p.cfg.Name(r.Context(), voice, asked.Name, asked.Company, asked.SurnameUnknown)
		if err != nil {
			answer(w, http.StatusOK, map[string]string{"error": err.Error()})
			return
		}

		p.mu.Lock()
		p.named = append(p.named, Settled{Voice: voice, Person: display})
		p.mu.Unlock()
		answer(w, http.StatusOK, map[string]string{"named": display})
	})

	mux.HandleFunc("POST /done", func(w http.ResponseWriter, r *http.Request) {
		answer(w, http.StatusOK, map[string]string{"status": "ok"})
		p.once.Do(func() { close(p.done) })
	})

	return mux
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
