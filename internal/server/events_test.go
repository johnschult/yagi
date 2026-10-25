package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johnschult/yagi/internal/access"
)

// What happened to a tuner before Yagi restarted is on the page after it, and what happens now is written down.
func TestTunerEventsSurviveARestart(t *testing.T) {
	a := newFakeTuner(t, "10ABCC87", "antenna-box")
	past := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	var events string
	f := startFleetWith(t, access.DefaultSettings(), oneDevice(a), true, func(c *Config) {
		events = filepath.Join(filepath.Dir(c.ConfigPath), "yagi.events.jsonl")
		line := `{"device":"antenna","time":"` + past + `","kind":"err","text":"Tuner offline: before the restart"}` + "\n"
		if err := os.WriteFile(events, []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
	}, a)

	var texts []string
	waitFor(t, "the tuner to come online", func() bool {
		_, body := call(t, "GET", f.base+"/api/state", nil)
		var st struct {
			Devices []struct {
				Tuner struct {
					Events []struct{ Text string } `json:"events"`
				} `json:"tuner"`
			} `json:"devices"`
		}
		if json.Unmarshal(body, &st) != nil || len(st.Devices) != 1 {
			return false
		}
		texts = texts[:0]
		for _, e := range st.Devices[0].Tuner.Events {
			texts = append(texts, e.Text)
		}
		return strings.Contains(strings.Join(texts, "\n"), "Tuner online")
	})
	if len(texts) < 2 || texts[0] != "Tuner offline: before the restart" {
		t.Errorf("events = %q, want the one from before first", texts)
	}
	b, _ := os.ReadFile(events)
	if !strings.Contains(string(b), "Tuner online") || !strings.Contains(string(b), "before the restart") {
		t.Errorf("the file does not have both:\n%s", b)
	}
}
