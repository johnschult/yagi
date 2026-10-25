package server

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/johnschult/yagi/internal/access"
	"github.com/johnschult/yagi/internal/history"
	"github.com/johnschult/yagi/internal/tuner"
)

// historyFile is where who watched what is kept: beside the settings file, and empty (so kept in memory
// only) when there isn't one.
func historyFile(configPath string) string {
	if configPath == "" {
		return ""
	}
	return strings.TrimSuffix(configPath, filepath.Ext(configPath)) + ".history.jsonl"
}

// eventsFile is where what happened to the tuners is kept: beside the settings file, and empty (so kept in memory
// only) when there isn't one.
func eventsFile(configPath string) string {
	if configPath == "" {
		return ""
	}
	return strings.TrimSuffix(configPath, filepath.Ext(configPath)) + ".events.jsonl"
}

// observations says what is in use on every tuner right now, for the Activity page.
func (s *Server) observations(now time.Time) []history.Observation {
	streams := s.policy.Active()
	var out []history.Observation
	for _, d := range s.deviceList() {
		name := d.cfg.Name
		title := func(channel string) string {
			if e, ok := s.guide.Lookup(name, channel, now); ok && e.Now != nil {
				return e.Now.Title
			}
			return ""
		}
		out = append(out, observe(name, d.mon.Snapshot(), streams, title)...)
	}
	return out
}

// observe is what is in use on one tuner. A stream through Yagi is paired with the viewer it belongs
// to by channel, as the Live page does; one that can't be paired is "remote". Anything else in use is the
// owner's own device, straight from the tuner. An offline tuner has nothing in use.
func observe(device string, t tuner.State, streams []access.Active, title func(channel string) string) []history.Observation {
	if !t.Online {
		return nil
	}
	var unmatched []access.Active
	for _, st := range streams {
		if st.Device == device {
			unmatched = append(unmatched, st)
		}
	}
	var out []history.Observation
	for _, slot := range t.Slots {
		if !slot.InUse {
			continue
		}
		o := history.Observation{Device: device, Tuner: slot.Resource, Channel: slot.Channel, ChannelName: slot.ChannelName, Title: title(slot.Channel),
			BitsPerSecond: slot.BitsPerSecond, Quality: slot.SignalQuality}
		if slot.ViaProxy {
			o.Kind = history.KindRemote
			for j, st := range unmatched {
				if st.Channel != "" && st.Channel == slot.Channel {
					o.Kind, o.Viewer = history.KindViewer, st.Viewer
					unmatched = append(unmatched[:j], unmatched[j+1:]...)
					break
				}
			}
		} else {
			o.Kind, o.Client = history.KindLocal, slot.TargetIP
		}
		out = append(out, o)
	}
	return out
}

// stepHistory is one look at the tuners. With the history switched off it looks at nothing, and ends any
// session that was going, so nothing is recorded from the moment it is switched off.
func stepHistory(r *history.Recorder, enabled bool, now time.Time, observe func() []history.Observation) {
	if !enabled {
		r.Observe(now, nil)
		return
	}
	r.Observe(now, observe())
}

// recordHistory keeps the Activity page's sessions up to date until ctx ends, then closes any still going.
func (s *Server) recordHistory(ctx context.Context) {
	t := time.NewTicker(s.cfg.Poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.history.Close()
			return
		case now := <-t.C:
			stepHistory(s.history, s.policy.Settings().HistoryEnabled(), now, func() []history.Observation { return s.observations(now) })
		}
	}
}

// logLevel is a level named in the settings, as the logger's.
func logLevel(name string) slog.Level {
	switch name {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	return slog.LevelInfo
}

// keepEvent returns what a tuner's monitor calls for each new event, or nil when there is nowhere to keep it.
func (s *Server) keepEvent(device string) func(tuner.Event) {
	if s.events == nil {
		return nil
	}
	return func(e tuner.Event) { s.events.Append(device, e) }
}
