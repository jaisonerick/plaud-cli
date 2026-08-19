package speaker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// AliasesFile is where the spellings found in transcripts are mapped to the
// name the store should hold.
const AliasesFile = "speaker-names.json"

// Aliases maps a name as transcripts spell it to the full name it belongs to.
// Transcripts call people whatever the person typing felt like — "luca", "Vic",
// "Amanda" — and only someone who knows them can say who that is.
type Aliases struct {
	names map[string]string
}

// LoadAliases reads the mapping, returning an empty one when the file is absent.
func LoadAliases(path string) (*Aliases, error) {
	aliases := &Aliases{names: map[string]string{}}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return aliases, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	for spelling, full := range raw {
		aliases.names[Fold(spelling)] = full
	}
	return aliases, nil
}

// Resolve returns the full name a spelling stands for, or the spelling itself.
func (a *Aliases) Resolve(name string) string {
	if full, ok := a.names[Fold(name)]; ok && full != "" {
		return full
	}
	return name
}

// WriteTemplate records the names still waiting for a full spelling, keeping
// every answer already given. It returns how many are still blank.
func WriteTemplate(path string, needed []string) (int, error) {
	entries := map[string]string{}

	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &entries); err != nil {
			return 0, fmt.Errorf("parsing %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return 0, fmt.Errorf("reading %s: %w", path, err)
	}

	for _, name := range needed {
		if _, ok := entries[name]; !ok {
			entries[name] = ""
		}
	}

	blank := 0
	for _, full := range entries {
		if full == "" {
			blank++
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return 0, fmt.Errorf("creating directory for %s: %w", path, err)
	}
	data, err := marshalSorted(entries)
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return 0, fmt.Errorf("writing %s: %w", path, err)
	}
	return blank, nil
}

// marshalSorted keeps the file's order stable so that filling one name in does
// not reshuffle the rest.
func marshalSorted(entries map[string]string) ([]byte, error) {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	ordered := make([]byte, 0, len(entries)*32)
	ordered = append(ordered, "{\n"...)
	for i, key := range keys {
		name, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		full, err := json.Marshal(entries[key])
		if err != nil {
			return nil, err
		}
		ordered = append(ordered, "  "...)
		ordered = append(ordered, name...)
		ordered = append(ordered, ": "...)
		ordered = append(ordered, full...)
		if i < len(keys)-1 {
			ordered = append(ordered, ',')
		}
		ordered = append(ordered, '\n')
	}
	ordered = append(ordered, "}\n"...)
	return ordered, nil
}
