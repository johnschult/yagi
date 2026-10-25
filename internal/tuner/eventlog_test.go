package tuner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func ev(offset time.Duration, kind, text string) Event {
	return Event{Time: t0.Add(offset), Kind: kind, Text: text}
}

func TestEventsComeBackAfterARestartOldestFirstPerTuner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yagi.events.jsonl")
	l := &EventLog{Path: path}
	l.Append("antenna", ev(0, "ok", "Tuner online"))
	l.Append("attic", ev(time.Minute, "err", "Tuner offline"))
	l.Append("antenna", ev(2*time.Minute, "info", "Tuner moved .132 to .140"))

	got := (&EventLog{Path: path}).Load(t0.Add(time.Hour)) // a new Yagi reading what the last one wrote
	if a := got["antenna"]; len(a) != 2 || a[0].Text != "Tuner online" || a[1].Text != "Tuner moved .132 to .140" || a[1].Kind != "info" || !a[1].Time.Equal(t0.Add(2*time.Minute)) {
		t.Errorf("antenna = %+v", a)
	}
	if a := got["attic"]; len(a) != 1 || a[0].Kind != "err" {
		t.Errorf("attic = %+v", a)
	}
}

func TestOldEventsAndGarbageAreLeftOutAndTheFileTidied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yagi.events.jsonl")
	l := &EventLog{Path: path}
	l.Append("antenna", ev(-KeepEvents-time.Hour, "ok", "ancient"))
	l.Append("antenna", ev(0, "ok", "recent"))
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("not json\n{\"device\":\"\",\"text\":\"no tuner\"}\n")
	f.Close()

	got := (&EventLog{Path: path}).Load(t0)
	if a := got["antenna"]; len(a) != 1 || a[0].Text != "recent" {
		t.Errorf("got %+v", got)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "ancient") || strings.Contains(string(b), "not json") || strings.Count(string(b), "\n") != 1 {
		t.Errorf("the file was not tidied:\n%s", b)
	}
}

func TestOnlyTheNewestEventsOfEachTunerAreKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yagi.events.jsonl")
	l := &EventLog{Path: path}
	for i := 0; i < maxEvents+10; i++ {
		l.Append("antenna", ev(time.Duration(i)*time.Second, "info", fmt.Sprintf("e%d", i)))
	}
	l.Append("attic", ev(0, "info", "quiet"))
	got := (&EventLog{Path: path}).Load(t0.Add(time.Hour))
	a := got["antenna"]
	if len(a) != maxEvents || a[0].Text != "e10" || a[len(a)-1].Text != fmt.Sprintf("e%d", maxEvents+9) {
		t.Errorf("antenna has %d, first %q, last %q", len(a), a[0].Text, a[len(a)-1].Text)
	}
	if len(got["attic"]) != 1 {
		t.Errorf("a chatty tuner pushed a quiet one out: %+v", got["attic"])
	}
}

func TestTheFileStopsGrowingPastItsLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yagi.events.jsonl")
	l := &EventLog{Path: path}
	big := strings.Repeat("x", 4000)
	for i := 0; i < 600; i++ {
		l.Append("antenna", ev(time.Duration(i)*time.Second, "info", big))
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Size() > maxEventFile+8000 {
		t.Errorf("size = %v, err = %v", fi, err)
	}
}

func TestAMonitorStartsWithItsPastAndReportsNewEvents(t *testing.T) {
	f := newHDHR("10ABCC87")
	defer f.Close()
	var heard []Event
	m := NewMonitor(Config{
		BaseURL: f.URL, ID: "10ABCC87", Failures: 2, Find: neverFind,
		LocalIPs: func() []string { return nil },
		Past:     []Event{ev(-time.Hour, "err", "Tuner offline: before the restart")},
		OnEvent:  func(e Event) { heard = append(heard, e) },
	})
	if s := m.Snapshot(); len(s.Events) != 1 || s.Events[0].Text != "Tuner offline: before the restart" {
		t.Fatalf("events at the start = %+v", s.Events)
	}
	m.Poll(context.Background())
	s := m.Snapshot()
	if len(s.Events) < 2 || s.Events[0].Kind != "err" || !strings.Contains(s.Events[len(s.Events)-1].Text, "online") {
		t.Errorf("events = %+v", s.Events)
	}
	if len(heard) == 0 || !strings.Contains(heard[len(heard)-1].Text, "online") {
		t.Errorf("heard = %+v", heard)
	}
}

func TestAClosedLogWritesNothingMore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yagi.events.jsonl")
	l := &EventLog{Path: path}
	l.Append("antenna", ev(0, "ok", "before"))
	l.Close()
	l.Append("antenna", ev(time.Second, "ok", "after"))
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "before") || strings.Contains(string(b), "after") {
		t.Errorf("file:\n%s", b)
	}
	var none *EventLog
	none.Close() // there is none when there is no settings file
}
