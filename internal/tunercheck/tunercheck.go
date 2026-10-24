// Package tunercheck walks a tuner's side of a failed stream, in order, and says what it finds in plain words:
// does the tuner answer, is it the tuner Yagi expects, does it have channels, is a tuner free, and does it report
// errors of its own. It only ever reads. Nothing here tunes a channel or changes a setting.
package tunercheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/johnschult/yagi/internal/hdhr"
)

// Status is how a step went.
type Status string

const (
	OK      Status = "ok"
	Problem Status = "problem"
	Note    Status = "note" // worth knowing, but not a fault
	Skipped Status = "skipped"
)

// Step is one thing that was checked, and what was found.
type Step struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
}

// Debug is what the tuner printed about itself for one part: its system, or one of its tuners.
type Debug struct {
	Name  string   `json:"name"`
	Lines []string `json:"lines"`
}

// Report is the result of a check.
type Report struct {
	At    time.Time `json:"at"`
	Steps []Step    `json:"steps"`
	Debug []Debug   `json:"debug,omitempty"`
}

// Slot is what Yagi already knows about one of the tuner's tuners.
type Slot struct {
	InUse    bool
	ViaProxy bool
}

// Target is the tuner to check.
type Target struct {
	BaseURL string
	ID      string // the DeviceID Yagi follows
	Slots   []Slot
}

const (
	maxBody  = 4 << 20
	maxLines = 40
	maxLine  = 200
)

// Checker runs checks.
type Checker struct {
	// Dial opens a control connection to a tuner's host. Required.
	Dial   func(ctx context.Context, host string) (*hdhr.Conn, error)
	Client *http.Client
	Now    func() time.Time
}

func (c *Checker) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 6 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Checker) getJSON(ctx context.Context, base, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("it answered %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(v)
}

// Run checks the tuner. Each step stands alone: one that fails doesn't stop the rest, except that nothing
// else is worth trying when the tuner doesn't answer at all.
func (c *Checker) Run(ctx context.Context, t Target) Report {
	r := Report{At: c.now()}
	add := func(id, title string, st Status, detail string) {
		r.Steps = append(r.Steps, Step{ID: id, Title: title, Status: st, Detail: detail})
	}

	u, err := url.Parse(t.BaseURL)
	if err != nil || u.Hostname() == "" {
		add("reach", "The tuner answers", Problem, "Yagi doesn't know where the tuner is.")
		return r
	}

	// 1. Does it answer on the network, and is it the right one?
	var d struct {
		DeviceID   string `json:"DeviceID"`
		TunerCount int    `json:"TunerCount"`
	}
	if err := c.getJSON(ctx, t.BaseURL, "/discover.json", &d); err != nil {
		add("reach", "The tuner answers", Problem, fmt.Sprintf("Nothing came back from %s (%s). The tuner is off, unplugged, or has moved to another address.", u.Host, shortErr(err)))
		for _, s := range []struct{ id, title string }{{"identity", "It is the right tuner"}, {"channels", "It has channels"}, {"free", "A tuner is free"}, {"errors", "The tuner's own error counters"}} {
			add(s.id, s.title, Skipped, "Not checked: the tuner didn't answer.")
		}
		return r
	}
	add("reach", "The tuner answers", OK, fmt.Sprintf("It answered at %s.", u.Host))

	// 2. Is it the tuner Yagi follows?
	switch {
	case t.ID == "":
		add("identity", "It is the right tuner", Skipped, "Yagi hasn't learned this tuner's ID yet.")
	case strings.EqualFold(d.DeviceID, t.ID):
		add("identity", "It is the right tuner", OK, "Its ID is "+strings.ToUpper(d.DeviceID)+", the one Yagi follows.")
	default:
		add("identity", "It is the right tuner", Problem, fmt.Sprintf("%s has tuner %s on it, but Yagi follows %s. Another tuner has taken that address.", u.Host, strings.ToUpper(d.DeviceID), strings.ToUpper(t.ID)))
	}

	// 3. Does it have channels? An empty list is what makes a media server's guide come up blank.
	var lineup []json.RawMessage
	switch err := c.getJSON(ctx, t.BaseURL, "/lineup.json", &lineup); {
	case err != nil:
		add("channels", "It has channels", Problem, "The tuner's channel list couldn't be read ("+shortErr(err)+").")
	case len(lineup) == 0:
		add("channels", "It has channels", Problem, "The channel list is empty. A media server shows that as a blank guide. Run a channel scan on the tuner's page.")
	default:
		add("channels", "It has channels", OK, plural(len(lineup), "channel")+".")
	}

	// 4. Is a tuner free?
	inUse, mine := 0, 0
	for _, s := range t.Slots {
		if s.InUse {
			inUse++
			if s.ViaProxy {
				mine++
			}
		}
	}
	switch n := len(t.Slots); {
	case n == 0:
		add("free", "A tuner is free", Skipped, "Yagi hasn't read the tuner's status yet.")
	case inUse >= n:
		add("free", "A tuner is free", Problem, fmt.Sprintf("All %d of the tuner's tuners are in use (%d through Yagi, %d straight from the tuner). A media server that asks now is told the channel isn't available.", n, mine, inUse-mine))
	default:
		add("free", "A tuner is free", OK, fmt.Sprintf("%d of %d are free.", n-inUse, n))
	}

	// 5. What does the tuner say about itself?
	count := d.TunerCount
	if count == 0 {
		count = len(t.Slots)
	}
	c.readDebug(ctx, u.Hostname(), count, &r)
	return r
}

// readDebug asks the tuner for its own debug text. It is a read of /sys/debug and /tuner<N>/debug, which are
// counters and short status lines; each is cut down so a surprising reply can't fill the page.
func (c *Checker) readDebug(ctx context.Context, host string, tuners int, r *Report) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	conn, err := c.Dial(ctx, host)
	if err != nil {
		r.Steps = append(r.Steps, Step{ID: "errors", Title: "The tuner's own error counters", Status: Skipped, Detail: "The tuner's control port didn't answer, so its own counters weren't read."})
		return
	}
	defer conn.Close()

	var found []string
	read := func(name, label string) {
		v, err := conn.Get(ctx, name)
		if err != nil {
			return // a tuner without this value is not a fault
		}
		lines := cleanLines(v)
		if len(lines) == 0 {
			return
		}
		r.Debug = append(r.Debug, Debug{Name: label, Lines: lines})
		for _, l := range lines {
			found = append(found, counters(label, l)...)
		}
	}
	read("/sys/debug", "System")
	for i := 0; i < tuners && i < 16; i++ {
		read(fmt.Sprintf("/tuner%d/debug", i), fmt.Sprintf("Tuner %d", i+1))
	}

	switch {
	case len(r.Debug) == 0:
		r.Steps = append(r.Steps, Step{ID: "errors", Title: "The tuner's own error counters", Status: Skipped, Detail: "This tuner doesn't give out debug counters."})
	case len(found) > 0:
		r.Steps = append(r.Steps, Step{ID: "errors", Title: "The tuner's own error counters", Status: Note, Detail: "The tuner counted: " + strings.Join(found, "; ") + ". Counts like these mean a weak signal or a network that drops data between the tuner and whoever is watching. They start over when a stream starts."})
	default:
		r.Steps = append(r.Steps, Step{ID: "errors", Title: "The tuner's own error counters", Status: OK, Detail: "Every error counter the tuner keeps is at zero."})
	}
}

// cleanLines makes the tuner's text safe to show: some lines, one line each, not long.
func cleanLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.Join(strings.Fields(l), " ")
		if l == "" {
			continue
		}
		if len(l) > maxLine {
			l = l[:maxLine]
		}
		out = append(out, l)
		if len(out) == maxLines {
			break
		}
	}
	return out
}

// errorCounters are the counters in a debug line that mean data was lost, and what each says in words.
var errorCounters = map[string]string{
	"resync": "stream resyncs", "overflow": "buffer overflows", "te": "transport errors",
	"crc": "bad checksums", "err": "network errors", "stop": "network stops",
}

var counterRe = regexp.MustCompile(`\b([a-z]+)=(\d+)\b`)

// counters picks the error counters that are not zero out of one debug line.
func counters(where, line string) []string {
	var out []string
	for _, m := range counterRe.FindAllStringSubmatch(line, -1) {
		word, ok := errorCounters[m[1]]
		if !ok {
			continue
		}
		if n, _ := strconv.Atoi(m[2]); n > 0 {
			out = append(out, fmt.Sprintf("%s: %d %s", where, n, word))
		}
	}
	return out
}

// shortErr says why a request failed without the URL or any detail a person can't use.
func shortErr(err error) string {
	var ne interface{ Timeout() bool }
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "no answer in time"
	case errors.Is(err, context.Canceled):
		return "stopped"
	}
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}
