package identify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"sort"
	"time"

	"github.com/jaisonerick/plaud-cli/internal/transcript"
)

const maxSamples = 3

// SpeakerSample holds a representative audio excerpt for a speaker.
type SpeakerSample struct {
	StartSec float64 `json:"start_sec"`
	EndSec   float64 `json:"end_sec"`
	Text     string  `json:"text"`
}

// SpeakerInfo groups samples for a single unresolved speaker.
type SpeakerInfo struct {
	ID      string          `json:"id"`
	Samples []SpeakerSample `json:"samples"`
}

// Config holds everything the identify server needs.
type Config struct {
	AudioData []byte
	AudioID   string
	Speakers  map[string]string    // full speaker map from transcription
	Segments  []transcript.Segment
}

// Result contains the name assignments collected from the web UI.
type Result struct {
	Names map[string]string // speakerID -> name
}

// RunServer starts a local web server for speaker identification.
// It returns the name assignments once the user submits or skips.
func RunServer(ctx context.Context, cfg Config) (*Result, error) {
	unresolvedIDs := unresolvedSpeakers(cfg.Speakers)
	if len(unresolvedIDs) == 0 {
		return &Result{Names: map[string]string{}}, nil
	}

	speakerInfos := buildSpeakerInfo(unresolvedIDs, cfg.Segments)
	speakerJSON, _ := json.Marshal(speakerInfos)

	resultCh := make(chan *Result, 1)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("starting listener: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	tmpl := template.Must(template.New("page").Parse(pageHTML))

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, map[string]any{
			"SpeakersJSON": template.JS(speakerJSON),
			"Speakers":     speakerInfos,
		})
	})

	mux.HandleFunc("/audio", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "audio.mp3", time.Time{}, bytes.NewReader(cfg.AudioData))
	})

	mux.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var names map[string]string
		if err := json.NewDecoder(r.Body).Decode(&names); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

		resultCh <- &Result{Names: names}
	})

	mux.HandleFunc("/skip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

		resultCh <- &Result{Names: map[string]string{}}
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener)

	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	fmt.Printf("Opening browser at %s\n", url)
	openBrowser(url)

	select {
	case result := <-resultCh:
		// Close immediately — don't wait for open connections to drain
		server.Close()
		return result, nil
	case <-ctx.Done():
		server.Close()
		return nil, ctx.Err()
	}
}

// UnresolvedSpeakers returns speaker IDs that were not matched to a known name.
func UnresolvedSpeakers(speakers map[string]string) []string {
	return unresolvedSpeakers(speakers)
}

func unresolvedSpeakers(speakers map[string]string) []string {
	var ids []string
	for k, v := range speakers {
		if k == v {
			ids = append(ids, k)
		}
	}
	sort.Strings(ids)
	return ids
}

func buildSpeakerInfo(unresolvedIDs []string, segments []transcript.Segment) []SpeakerInfo {
	var speakers []SpeakerInfo

	for _, id := range unresolvedIDs {
		var segs []transcript.Segment
		for _, seg := range segments {
			if seg.Speaker == id {
				segs = append(segs, seg)
			}
		}

		// Pick the longest segments as representative samples
		sort.Slice(segs, func(i, j int) bool {
			return (segs[i].EndTime - segs[i].StartTime) > (segs[j].EndTime - segs[j].StartTime)
		})

		n := maxSamples
		if len(segs) < n {
			n = len(segs)
		}

		samples := make([]SpeakerSample, n)
		for i := 0; i < n; i++ {
			samples[i] = SpeakerSample{
				StartSec: float64(segs[i].StartTime) / 1000.0,
				EndSec:   float64(segs[i].EndTime) / 1000.0,
				Text:     segs[i].Content,
			}
		}

		speakers = append(speakers, SpeakerInfo{
			ID:      id,
			Samples: samples,
		})
	}

	return speakers
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "darwin":
		exec.Command("open", url).Start()
	case "linux":
		exec.Command("xdg-open", url).Start()
	case "windows":
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
}
