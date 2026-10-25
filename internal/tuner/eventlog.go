package tuner

import (
	"bufio"
	"encoding/json"
	"os"
	"sync"
	"time"
)

// KeepEvents is how long a tuner's events are kept on disk.
const KeepEvents = 30 * 24 * time.Hour

// maxEventFile is the most the file may grow to between starts. A tuner that flaps all day writes a line each time;
// past this, new events are still shown on the page but not written, so the file can't fill a disk.
const maxEventFile = 1 << 20

// loggedEvent is one line of the file: an event, and the tuner it was about.
type loggedEvent struct {
	Device string    `json:"device"`
	Time   time.Time `json:"time"`
	Kind   string    `json:"kind"`
	Text   string    `json:"text"`
}

// EventLog keeps what happened to the tuners (offline, back, moved, turned streams away) in a file beside the
// settings, one JSON object per line, so it is still there after Yagi restarts. That is the very time someone
// wants it: an update or a restart is when "the tuner moved" or "it went offline" needs a record to look at.
type EventLog struct {
	Path string
	// OnError, if set, hears of an event that could not be written. The event is still kept in memory.
	OnError func(error)

	mu   sync.Mutex
	size int64
}

// Load reads the events in the file that are newer than KeepEvents, for each tuner by name, the newest maxEvents
// of each, oldest first. Lines it can't read are skipped. It tidies the file when something was left out.
func (l *EventLog) Load(now time.Time) map[string][]Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.Open(l.Path)
	if err != nil {
		return nil
	}
	if fi, err := f.Stat(); err == nil {
		l.size = fi.Size()
	}
	var all []loggedEvent
	dropped := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		var e loggedEvent
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil || e.Device == "" || e.Text == "" || now.Sub(e.Time) > KeepEvents {
			dropped++
			continue
		}
		all = append(all, e)
	}
	f.Close()

	by := map[string][]Event{}
	for _, e := range all {
		by[e.Device] = append(by[e.Device], Event{Time: e.Time, Kind: e.Kind, Text: e.Text})
	}
	kept := all[:0:0]
	count := map[string]int{}
	for i := len(all) - 1; i >= 0; i-- { // the newest of each tuner
		e := all[i]
		if count[e.Device] >= maxEvents {
			dropped++
			continue
		}
		count[e.Device]++
		kept = append(kept, e)
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	for d, evs := range by {
		if len(evs) > maxEvents {
			by[d] = evs[len(evs)-maxEvents:]
		}
	}
	if dropped > 0 {
		_ = l.rewrite(kept) // best effort: what is kept loads again next time, and the old lines are dropped again
	}
	return by
}

// Append adds one event to the end of the file.
func (l *EventLog) Append(device string, e Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.size >= maxEventFile {
		return
	}
	b, err := json.Marshal(loggedEvent{Device: device, Time: e.Time, Kind: e.Kind, Text: e.Text})
	if err == nil {
		var f *os.File
		if f, err = os.OpenFile(l.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			var n int
			n, err = f.Write(append(b, '\n'))
			l.size += int64(n)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
		}
	}
	if err != nil && l.OnError != nil {
		l.OnError(err)
	}
}

// rewrite replaces the file with these events, by writing a new one and renaming it into place; l.mu must be held.
func (l *EventLog) rewrite(events []loggedEvent) error {
	tmp := l.Path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	var size int64
	for _, e := range events {
		b, err := json.Marshal(e)
		if err != nil {
			f.Close()
			return err
		}
		n, _ := w.Write(append(b, '\n')) // a failed write shows at Flush: the writer keeps its first error
		size += int64(n)
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	l.size = size
	return os.Rename(tmp, l.Path)
}
