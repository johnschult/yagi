package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/johnschult/yagi/internal/logbuf"
	"github.com/johnschult/yagi/internal/tuner"
	"github.com/johnschult/yagi/internal/tunercheck"
)

// reportDoc is a report on how Yagi and its tuners are doing right now, as text a person can read, copy and send.
type reportDoc struct {
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

const (
	reportLogLines = 300 // the newest info-and-above lines; diagnostics lines are among them
	reportEvents   = 20  // the newest tuner events for each tuner
)

// makeReport is the "Report a problem" button. It gathers in one place what is otherwise spread over four tabs: the
// settings in force, each tuner's state, problems and events, a fresh Check now for each tuner, and the newest log
// lines. It is a POST because it checks the tuners every time. It reads only. Nothing in it is a secret: the log
// hides secrets before it keeps a line, and a tuner's DeviceAuth and the viewers' keys are never put in.
func (a *API) makeReport(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	st := a.state()

	var b strings.Builder
	line := func(f string, args ...any) { fmt.Fprintf(&b, f+"\n", args...) }
	head := func(t string) { line("\n== %s ==", t) }
	stamp := func(t time.Time) string {
		if t.IsZero() {
			return "never"
		}
		return t.UTC().Format("2006-01-02 15:04:05Z")
	}

	line("Yagi report")
	line("Made:       %s", stamp(now))
	line("Version:    %s (%s/%s)", st.Version, runtime.GOOS, runtime.GOARCH)
	if st.Diagnostics.On && st.Diagnostics.Until != nil {
		line("Diagnostics mode: on, until %s", stamp(*st.Diagnostics.Until))
	} else {
		line("Diagnostics mode: off")
	}

	head("Settings")
	s := st.Settings
	line("Mode: %s; owner reserve: %d; shared cap: %d; log level: %s", s.Mode, s.OwnerReserve, s.SharedCap, s.LogLevel)
	line("Viewers: %d; waiting requests: %d; Tailscale: %v", len(st.Viewers), len(st.Requests), s.Tailscale.Enabled)

	// A fresh check for every tuner, at once; each takes a few seconds when the tuner is slow to answer.
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	reports := make([]*tunercheck.Report, len(st.Devices))
	if a.Check != nil {
		var wg sync.WaitGroup
		for i, d := range st.Devices {
			target := tunercheck.Target{BaseURL: d.Tuner.BaseURL, ID: d.Tuner.ID}
			for _, sl := range d.Tuner.Slots {
				target.Slots = append(target.Slots, tunercheck.Slot{InUse: sl.InUse, ViaProxy: sl.ViaProxy})
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				rep := a.Check.Run(ctx, target)
				reports[i] = &rep
			}()
		}
		wg.Wait()
	}

	for i, d := range st.Devices {
		t := d.Tuner
		head("Tuner " + d.Name)
		line("Model: %s; firmware: %s; ID: %s; tuners: %d", orDash(t.Model), orDash(t.Firmware.Version), orDash(t.ID), t.TunerCount)
		where := "unknown"
		if u, err := url.Parse(t.BaseURL); err == nil && u.Host != "" {
			where = u.Host
		}
		if t.Online {
			line("Answering at %s since %s (last seen %s)", where, stamp(t.Since), stamp(t.LastSeen))
		} else {
			line("NOT answering at %s since %s (last seen %s)", where, stamp(t.Since), stamp(t.LastSeen))
		}
		if t.Search != nil {
			found := "did not find it"
			if t.Search.Found {
				found = "found it"
			}
			line("Yagi last went looking for it at %s and %s", stamp(t.Search.At), found)
		}
		total, via := t.InUse()
		line("In use: %d of %d (%d through Yagi, %d straight from the tuner); owner reserve %d, shared cap %d", total, t.TunerCount, via, total-via, d.OwnerReserve, d.SharedCap)
		for _, sl := range t.Slots {
			if sl.InUse {
				who := "straight from the tuner"
				if sl.ViaProxy {
					who = "through Yagi"
				}
				line("  %s: channel %s %s, %s", sl.Resource, orDash(sl.Channel), sl.ChannelName, who)
			}
		}
		if len(t.Problems) > 0 {
			line("Trouble media servers ran into:")
			for _, p := range t.Problems {
				line("  %s", problemLine(p))
			}
		}
		if ev := t.Events; len(ev) > 0 {
			if len(ev) > reportEvents {
				ev = ev[len(ev)-reportEvents:]
			}
			line("Latest events:")
			for _, e := range ev {
				line("  %s [%s] %s", stamp(e.Time), e.Kind, e.Text)
			}
		}
		if rep := reports[i]; rep != nil {
			line("Check now, just run:")
			for _, step := range rep.Steps {
				line("  [%s] %s: %s", step.Status, step.Title, step.Detail)
			}
			for _, dbg := range rep.Debug {
				line("  The tuner's own debug text, %s:", dbg.Name)
				for _, l := range dbg.Lines {
					line("    %s", l)
				}
			}
		}
	}

	head("Log")
	if a.Logs == nil {
		line("There is no log.")
	} else {
		res := a.Logs.Find(logbuf.Query{Level: "info", Limit: reportLogLines})
		line("The newest %d lines at info and above (%d kept of %d it can hold). Times are UTC.", len(res.Entries), res.Kept, res.Capacity)
		for _, e := range res.Entries {
			line("%s %-5s %s%s", e.Time.UTC().Format("15:04:05.000"), e.Level, e.Message, attrText(e.Attrs))
		}
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, reportDoc{At: now, Text: b.String()})
}

func attrText(attrs []logbuf.Attr) string {
	if len(attrs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(attrs))
	for _, a := range attrs {
		parts = append(parts, a.Key+"="+a.Value)
	}
	return " " + strings.Join(parts, " ")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// problemLine says when a kind of trouble started and last happened, and what it was.
func problemLine(p tuner.Problem) string {
	when := p.Time.UTC().Format("2006-01-02 15:04:05Z")
	if p.Last.After(p.Time) {
		when += " to " + p.Last.UTC().Format("15:04:05Z")
	}
	return fmt.Sprintf("%s (%s): %s", when, p.Kind, p.Text)
}
