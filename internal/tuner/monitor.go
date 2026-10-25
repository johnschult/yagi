// Package tuner watches one HDHomeRun: is it up, which tuners are in use and by
// whom, and where it has moved to if its address changes.
package tuner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/johnschult/yagi/internal/discovery"
)

// Slot is one physical tuner on the device.
type Slot struct {
	Resource       string `json:"resource"` // tuner0, tuner1, ...
	InUse          bool   `json:"inUse"`
	Channel        string `json:"channel,omitempty"`
	ChannelName    string `json:"channelName,omitempty"`
	TargetIP       string `json:"targetIP,omitempty"`
	ViaProxy       bool   `json:"viaProxy"` // streaming to this host, i.e. through us
	SignalStrength int    `json:"signalStrength,omitempty"`
	SignalQuality  int    `json:"signalQuality,omitempty"`
	BitsPerSecond  int64  `json:"bitsPerSecond,omitempty"`
}

// Firmware is what the tuner itself says about its firmware in discover.json.
type Firmware struct {
	Name    string `json:"name,omitempty"`    // e.g. "hdhomerun_dvr_atsc3"
	Version string `json:"version,omitempty"` // e.g. "20260326": a date
	// UpgradeAvailable is the newer version the tuner knows of, if it says so.
	UpgradeAvailable string `json:"upgradeAvailable,omitempty"`
}

// Event is something worth telling the owner about.
type Event struct {
	Time time.Time `json:"time"`
	Kind string    `json:"kind"` // ok, info, warn, err
	Text string    `json:"text"`
}

// Search is the last time Yagi went looking for a tuner that had stopped answering, and whether it found it.
type Search struct {
	At    time.Time `json:"at"`
	Found bool      `json:"found"`
}

// Problem kinds.
const (
	ProblemRefused    = "refused"     // the tuner turned a stream away
	ProblemNoChannels = "no-channels" // the tuner answered a lineup request with no channels
	ProblemDenied     = "denied"      // Yagi turned a viewer's stream away (their limit, the shared cap, the owner's reserve)
)

// Problem is something that went wrong between a media server and the tuner, which the media server only
// shows as "channel not available" or an empty guide. There is at most one of each kind, the latest.
//
// A run of the same trouble is one episode: Time is when it started and stays the same while it goes on, and Last
// is the latest time it happened. A media server retries every few seconds, so without that the page could not
// tell "again" from "still", and a notice that was dismissed would come back with the next retry. An episode ends
// after problemEpisode without it happening again, or, for an empty channel list, when channels are back.
type Problem struct {
	Kind string    `json:"kind"`
	Time time.Time `json:"time"`
	Last time.Time `json:"last"`
	Text string    `json:"text"`
}

// problemEpisode is how long the same kind of trouble can go quiet and still be the same episode. The page stops
// showing a problem after the same time.
const problemEpisode = 15 * time.Minute

// State is a snapshot of the tuner.
type State struct {
	Name         string    `json:"name,omitempty"` // the device's name in Yagi
	Online       bool      `json:"online"`
	ID           string    `json:"id"`
	FriendlyName string    `json:"friendlyName,omitempty"`
	Model        string    `json:"model,omitempty"`
	BaseURL      string    `json:"baseURL"`
	TunerCount   int       `json:"tunerCount"`
	Firmware     Firmware  `json:"firmware"`
	Slots        []Slot    `json:"slots"`
	LastSeen     time.Time `json:"lastSeen"`
	Since        time.Time `json:"since"` // when Online last changed
	Events       []Event   `json:"events"`
	// Search is set while the tuner isn't answering and Yagi has gone looking for it.
	Search *Search `json:"search,omitempty"`
	// Problems are the latest of each kind of trouble a media server ran into; a lineup with channels clears
	// ProblemNoChannels, and the page stops showing the others after a while.
	Problems []Problem `json:"problems,omitempty"`
	// Updating is true while Yagi is installing firmware: the tuner is expected to go away and
	// come back, so that is not an outage.
	Updating bool `json:"updating"`
	// Restarting is true while Yagi is restarting the tuner for someone: it is expected to go away
	// and come back, which is not an outage either.
	Restarting bool `json:"restarting"`
}

// InUse counts slots in use, and how many of those are ours.
func (s State) InUse() (total, viaProxy int) {
	for _, sl := range s.Slots {
		if sl.InUse {
			total++
			if sl.ViaProxy {
				viaProxy++
			}
		}
	}
	return
}

// Finder looks for the tuner with the given DeviceID when it has gone missing.
// lastKnown is the base URL it was last seen at.
type Finder func(ctx context.Context, id, lastKnown string) (discovery.Device, error)

// Config for a Monitor.
type Config struct {
	Name       string        // the device's name, shown in the UI
	BaseURL    string        // where to look first
	ID         string        // DeviceID to follow; learned from BaseURL if empty
	StreamPort string        // tuner's streaming port, default 5004
	Interval   time.Duration // poll period, default 5s
	Failures   int           // consecutive failures before searching, default 3
	Find       Finder        // default: UDP discovery, then a /24 HTTP scan
	LocalIPs   func() []string
	// Streams, if set, lists the channel of every stream Yagi has open on this tuner (empty when the path
	// named none). The tuner only says who is streaming by address, which is the same for Yagi and for a media
	// server on the same machine, and not what Yagi sees when it runs in a container with its own addresses.
	// With Streams, a slot is Yagi's when Yagi has a stream open on that channel; without it, when the address
	// is this host's. It is called without the Monitor's lock held, and must not call back into it.
	Streams func() []string
	// OnChange, if set, is called after every poll and every Note, so a UI can
	// refresh. It must not block.
	OnChange func()
	// OnTransition, if set, is called when the tuner is first seen online and each
	// time it goes offline or comes back, with the lock released. It must not block.
	OnTransition func(online bool)
	// OnMoved, if set, is called once the tuner has been found at a new address and has answered there, with
	// the address it was at and the one it is at now, and the lock released. It is how the new address gets saved.
	OnMoved func(from, to string)
	// Past is what happened to this tuner before Yagi started, oldest first, from the file they are kept in. The
	// page shows them above the new ones.
	Past []Event
	// OnEvent, if set, hears of each new event, to keep it. It is called with the Monitor's lock held, so it must
	// be quick and must not call back into it.
	OnEvent func(Event)
	Logger  *slog.Logger
}

// Monitor polls the tuner and tracks where it lives.
type Monitor struct {
	cfg    Config
	client *http.Client
	log    *slog.Logger

	kick chan struct{}

	mu         sync.RWMutex
	st         State
	fails      int
	seen       bool                 // the tuner has answered at least once since this Monitor started
	noted      map[string]time.Time // when each kind of event that repeats was last added
	lastSearch time.Time
	// updatingSince is when SetUpdating(true) was called.
	updatingSince   time.Time
	restartingSince time.Time
	// deviceAuth is the tuner's token for SiliconDust's guide service. It is
	// kept out of State on purpose, so it can't end up in an API response.
	deviceAuth string
}

// DeviceAuth is the tuner's token for SiliconDust's guide service, as last
// reported in discover.json; empty until the tuner has been reached.
func (m *Monitor) DeviceAuth() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.deviceAuth
}

const maxEvents = 50

// maxUpdating is the longest a tuner is believed to be updating. An upgrade that dies without saying so
// must not leave the tuner marked as updating, and so unreported as offline, for good.
const maxUpdating = 30 * time.Minute

// SetUpdating says Yagi is about to install firmware on the tuner (or has finished). While it is set
// the tuner going away is expected: no "offline" event, no notification, no search for it at another
// address, and State.Updating tells the page to say "updating" and not "offline". Clearing it polls the
// tuner at once. It clears itself after maxUpdating.
func (m *Monitor) SetUpdating(on bool) {
	m.mu.Lock()
	m.st.Updating = on
	if on {
		m.updatingSince = time.Now()
	}
	m.mu.Unlock()
	if m.cfg.OnChange != nil {
		m.cfg.OnChange()
	}
	if !on {
		m.Kick()
	}
}

// maxRestarting is the longest a tuner is believed to be restarting on request.
const maxRestarting = 10 * time.Minute

// SetRestarting is SetUpdating for a restart someone asked for: the tuner is expected to go away and
// come back, so that is not an outage, and State.Restarting tells the page to say "restarting". It
// clears itself after maxRestarting.
func (m *Monitor) SetRestarting(on bool) {
	m.mu.Lock()
	m.st.Restarting = on
	if on {
		m.restartingSince = time.Now()
	}
	m.mu.Unlock()
	if m.cfg.OnChange != nil {
		m.cfg.OnChange()
	}
	if !on {
		m.Kick()
	}
}

// updatingLocked says whether the tuner is expected to be away (being updated, or restarted on
// request); m.mu must be held.
func (m *Monitor) updatingLocked() bool {
	if m.st.Updating && time.Since(m.updatingSince) > maxUpdating {
		m.st.Updating = false
	}
	if m.st.Restarting && time.Since(m.restartingSince) > maxRestarting {
		m.st.Restarting = false
	}
	return m.st.Updating || m.st.Restarting
}

func NewMonitor(cfg Config) *Monitor {
	if cfg.StreamPort == "" {
		cfg.StreamPort = "5004"
	}
	if cfg.Interval == 0 {
		cfg.Interval = 5 * time.Second
	}
	if cfg.Failures == 0 {
		cfg.Failures = 3
	}
	if cfg.LocalIPs == nil {
		cfg.LocalIPs = localIPs
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	m := &Monitor{cfg: cfg, client: &http.Client{Timeout: 3 * time.Second}, log: cfg.Logger, kick: make(chan struct{}, 1)}
	m.st = State{Name: cfg.Name, ID: strings.ToUpper(cfg.ID), BaseURL: discovery.NormalizeBase(cfg.BaseURL), Since: time.Now()}
	if n := len(cfg.Past); n > 0 {
		if n > maxEvents {
			cfg.Past = cfg.Past[n-maxEvents:]
		}
		m.st.Events = append([]Event(nil), cfg.Past...)
	}
	if cfg.Find == nil {
		m.cfg.Find = m.defaultFind
	}
	return m
}

// Snapshot returns a copy of the current state.
func (m *Monitor) Snapshot() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := m.st
	s.Slots = append([]Slot(nil), s.Slots...)
	s.Events = append([]Event(nil), s.Events...)
	s.Problems = append([]Problem(nil), s.Problems...)
	if s.Search != nil {
		cp := *s.Search
		s.Search = &cp
	}
	return s
}

// Targets returns the tuner's API and streaming URLs as they are right now.
func (m *Monitor) Targets() (api, stream *url.URL) {
	base := m.Snapshot().BaseURL
	api, err := url.Parse(base)
	if err != nil {
		return nil, nil
	}
	s := *api
	s.Host = net.JoinHostPort(api.Hostname(), m.cfg.StreamPort)
	return api, &s
}

// Run polls until ctx is done.
func (m *Monitor) Run(ctx context.Context) {
	m.Poll(ctx)
	t := time.NewTicker(m.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Poll(ctx)
		case <-m.kick:
			// Something probably just changed on the tuner (a stream started
			// or ended). Give it a moment to register, then look right away
			// instead of waiting out the interval. Kicks arriving meanwhile
			// collapse into this one.
			select {
			case <-time.After(kickDelay):
			case <-ctx.Done():
				return
			}
			m.Poll(ctx)
		}
	}
}

const kickDelay = 400 * time.Millisecond

// Kick asks Run for an early poll. It never blocks.
func (m *Monitor) Kick() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// Poll does one health check, and one search if the tuner has been gone long
// enough.
func (m *Monitor) Poll(ctx context.Context) {
	m.poll(ctx)
	if m.cfg.OnChange != nil {
		m.cfg.OnChange()
	}
}

func (m *Monitor) poll(ctx context.Context) {
	base := m.Snapshot().BaseURL
	if err := m.check(ctx, base); err != nil {
		m.fail(ctx, err)
		return
	}
	m.mu.Lock()
	m.fails = 0
	m.mu.Unlock()
}

type discoverDoc struct {
	DeviceAuth       string
	FriendlyName     string
	ModelNumber      string
	DeviceID         string
	TunerCount       int
	FirmwareName     string
	FirmwareVersion  string
	UpgradeAvailable string
}

type statusEntry struct {
	Resource              string
	VctNumber             string
	VctName               string
	TargetIP              string
	SignalStrengthPercent int
	SignalQualityPercent  int
	NetworkRate           int64
}

func (m *Monitor) getJSON(ctx context.Context, base, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s%s: %s", base, path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// check confirms that the device at base is our tuner and records its state.
func (m *Monitor) check(ctx context.Context, base string) error {
	var d discoverDoc
	if err := m.getJSON(ctx, base, "/discover.json", &d); err != nil {
		return err
	}
	id := strings.ToUpper(d.DeviceID)

	m.mu.Lock()
	if m.st.ID == "" {
		m.st.ID = id // first contact: this is the tuner we follow from now on
		m.addEvent("info", fmt.Sprintf("Following tuner %s at %s", id, base))
	}
	want := m.st.ID
	m.mu.Unlock()
	if id != want {
		return fmt.Errorf("%s is tuner %s, not %s", base, id, want)
	}

	var raw []statusEntry
	statusErr := m.getJSON(ctx, base, "/status.json", &raw)
	local := map[string]bool{}
	for _, ip := range m.cfg.LocalIPs() {
		local[ip] = true
	}
	var streams []string
	if m.cfg.Streams != nil {
		streams = m.cfg.Streams() // before the lock: it asks the access policy, which asks us for snapshots
	}

	// Registered before the lock's own defer, so it runs after the lock is released.
	cameOnline := false
	defer func() {
		if cameOnline {
			m.announce(true)
		}
	}()
	m.mu.Lock()
	defer m.mu.Unlock()
	wasOnline := m.st.Online
	m.seen = true
	m.st.FriendlyName, m.st.Model, m.st.TunerCount = d.FriendlyName, d.ModelNumber, d.TunerCount
	m.st.Firmware = Firmware{Name: d.FirmwareName, Version: d.FirmwareVersion, UpgradeAvailable: d.UpgradeAvailable}
	m.deviceAuth = d.DeviceAuth
	m.st.LastSeen = time.Now()
	if statusErr == nil {
		m.st.Slots = slotsFrom(raw, local)
		if m.cfg.Streams != nil {
			claimStreams(m.st.Slots, streams)
		}
	}
	if !wasOnline {
		m.st.Online, m.st.Since = true, time.Now()
		m.st.Search = nil
		if !m.updatingLocked() { // coming back from a restart that was planned is not news
			cameOnline = true
			m.addEvent("ok", fmt.Sprintf("Tuner online at %s", base))
			m.log.Info("tuner online", "id", id, "base", base)
		}
	}
	return nil
}

func slotsFrom(raw []statusEntry, local map[string]bool) []Slot {
	var out []Slot
	for _, e := range raw {
		if !strings.HasPrefix(e.Resource, "tuner") {
			continue
		}
		s := Slot{Resource: e.Resource, TargetIP: e.TargetIP}
		if e.VctNumber != "" || e.TargetIP != "" {
			s.InUse = true
			s.Channel, s.ChannelName = e.VctNumber, e.VctName
			s.ViaProxy = local[e.TargetIP]
			s.SignalStrength, s.SignalQuality, s.BitsPerSecond = e.SignalStrengthPercent, e.SignalQualityPercent, e.NetworkRate
		}
		out = append(out, s)
	}
	return out
}

// claimStreams decides which in-use slots are Yagi's own, from the streams Yagi has open, and not from the
// address the tuner reports: a media server on the same machine as Yagi streams from the same address, and Yagi
// in a container streams from an address it doesn't know. A slot is Yagi's when Yagi has a stream open on its
// channel, one stream to one slot; the rest are the owner's own devices. Slots whose address is this host's
// (ViaProxy as slotsFrom set it) are claimed first, so a media server on another machine is never mistaken
// for Yagi's stream on the same channel. A stream whose path named no channel claims a slot at this host's address.
func claimStreams(slots []Slot, channels []string) {
	open := map[string]int{}
	unnamed := 0
	for _, c := range channels {
		if c == "" {
			unnamed++
		} else {
			open[c]++
		}
	}
	atHost := make([]bool, len(slots))
	claimed := make([]bool, len(slots))
	for i := range slots {
		atHost[i] = slots[i].ViaProxy
		slots[i].ViaProxy = false
	}
	claim := func(i int) {
		slots[i].ViaProxy, claimed[i] = true, true
	}
	for _, onlyHost := range []bool{true, false} {
		for i := range slots {
			if !slots[i].InUse || claimed[i] || slots[i].Channel == "" || open[slots[i].Channel] == 0 || (onlyHost && !atHost[i]) {
				continue
			}
			open[slots[i].Channel]--
			claim(i)
		}
	}
	for i := range slots {
		if unnamed > 0 && slots[i].InUse && !claimed[i] && atHost[i] {
			unnamed--
			claim(i)
		}
	}
}

func (m *Monitor) fail(ctx context.Context, err error) {
	m.mu.Lock()
	m.fails++
	fails := m.fails
	id, last := m.st.ID, m.st.BaseURL
	// A tuner that has never answered since Yagi started, and has a DeviceID to look for, is searched for at once:
	// the address that was saved is most likely out of date, and waiting through the failures only keeps it offline.
	if fails < m.cfg.Failures && (m.seen || id == "") {
		m.mu.Unlock()
		m.log.Debug("tuner check failed", "n", fails, "err", err)
		return
	}
	wentOffline := false
	if m.st.Online {
		m.st.Online, m.st.Since = false, time.Now()
		m.st.Slots = nil
		if !m.updatingLocked() {
			m.addEvent("err", fmt.Sprintf("Tuner offline: %v", err))
			m.log.Warn("tuner offline", "err", err)
			wentOffline = true
		}
	}
	if m.updatingLocked() {
		// It is restarting, and will be back where it was: don't go looking for it elsewhere.
		m.mu.Unlock()
		return
	}
	// Don't hammer the network: search at most every 30s.
	if time.Since(m.lastSearch) < 30*time.Second {
		m.mu.Unlock()
		if wentOffline {
			m.announce(false)
		}
		return
	}
	m.lastSearch = time.Now()
	m.mu.Unlock()
	if wentOffline { // before the search, which can take a while
		m.announce(false)
	}

	if id == "" {
		return // never saw it, so there's nothing to search for
	}
	sctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	dev, ferr := m.cfg.Find(sctx, id, last)
	if ferr != nil || dev.BaseURL == "" {
		m.mu.Lock()
		m.st.Search = &Search{At: time.Now(), Found: false}
		m.mu.Unlock()
		return
	}
	if dev.BaseURL == last {
		return
	}
	m.mu.Lock()
	m.st.BaseURL = dev.BaseURL
	m.addEvent("info", fmt.Sprintf("Tuner moved %s → %s (found by DeviceID %s)", last, dev.BaseURL, id))
	m.mu.Unlock()
	m.log.Info("tuner moved", "from", last, "to", dev.BaseURL, "id", id)
	m.Poll(ctx) // confirm right away instead of waiting a full interval
	if m.cfg.OnMoved != nil {
		if st := m.Snapshot(); st.Online && st.BaseURL == dev.BaseURL {
			m.cfg.OnMoved(last, dev.BaseURL)
		}
	}
}

// repeatEvery adds an event unless one of the same kind was added within the last every. The caller holds the lock.
func (m *Monitor) repeatEvery(kind string, every time.Duration, level, text string) {
	if m.noted == nil {
		m.noted = map[string]time.Time{}
	}
	if last, ok := m.noted[kind]; ok && time.Since(last) < every {
		return
	}
	m.noted[kind] = time.Now()
	m.addEvent(level, text)
}

// setProblem keeps the latest problem of a kind; a zero text clears it. The caller holds the lock.
func (m *Monitor) setProblem(kind, text string) {
	now := time.Now()
	started := now
	kept := m.st.Problems[:0:0]
	for _, p := range m.st.Problems {
		if p.Kind != kind {
			kept = append(kept, p)
		} else if kind == ProblemNoChannels || now.Sub(p.Last) < problemEpisode {
			started = p.Time // the same episode: it goes on
		}
	}
	if text != "" {
		kept = append(kept, Problem{Kind: kind, Time: started, Last: now, Text: text})
	}
	m.st.Problems = kept
}

// StreamRefused is told when the tuner turned a stream away: the status it sent and, if it gave one, its reason.
func (m *Monitor) StreamRefused(status, reason string) {
	text := "The tuner turned a stream away: " + status
	if reason != "" {
		text += " (it said: " + reason + ")"
	}
	m.mu.Lock()
	m.setProblem(ProblemRefused, text)
	m.repeatEvery(ProblemRefused, time.Minute, "warn", text)
	m.mu.Unlock()
	if m.cfg.OnChange != nil {
		m.cfg.OnChange()
	}
}

// StreamDenied is told when Yagi turned a viewer's stream away, in words for the owner. The timeline already has the
// event, so this only keeps the latest as a problem the page can show while it is recent.
func (m *Monitor) StreamDenied(text string) {
	m.mu.Lock()
	m.setProblem(ProblemDenied, text)
	m.mu.Unlock()
	if m.cfg.OnChange != nil {
		m.cfg.OnChange()
	}
}

// Lineup is told how many channels the tuner gave a media server. None is a problem, which stays until it gives some.
func (m *Monitor) Lineup(channels int) {
	m.mu.Lock()
	had := false
	for _, p := range m.st.Problems {
		had = had || p.Kind == ProblemNoChannels
	}
	switch {
	case channels == 0:
		text := "The tuner answered a media server's request for its channels with an empty list"
		m.setProblem(ProblemNoChannels, text)
		m.repeatEvery(ProblemNoChannels, time.Minute, "warn", text)
	case had:
		m.setProblem(ProblemNoChannels, "")
		m.addEvent("ok", fmt.Sprintf("The tuner has its channels again (%d)", channels))
	default:
		m.mu.Unlock()
		return // the usual case: nothing changed, and nothing to tell the page
	}
	m.mu.Unlock()
	if m.cfg.OnChange != nil {
		m.cfg.OnChange()
	}
}

// announce tells OnTransition, if there is one, that the tuner went offline or came online.
func (m *Monitor) announce(online bool) {
	if m.cfg.OnTransition != nil {
		m.cfg.OnTransition(online)
	}
}

// Note adds an event from outside the monitor, e.g. a refused stream.
func (m *Monitor) Note(kind, text string) {
	m.mu.Lock()
	m.addEvent(kind, text)
	m.mu.Unlock()
	if m.cfg.OnChange != nil {
		m.cfg.OnChange()
	}
}

// addEvent records an event; m.mu must be held.
func (m *Monitor) addEvent(kind, text string) {
	e := Event{Time: time.Now(), Kind: kind, Text: text}
	m.st.Events = append(m.st.Events, e)
	if m.cfg.OnEvent != nil {
		m.cfg.OnEvent(e)
	}
	if n := len(m.st.Events); n > maxEvents {
		m.st.Events = m.st.Events[n-maxEvents:]
	}
}

// defaultFind tries UDP discovery, then scans the /24 around the last address.
func (m *Monitor) defaultFind(ctx context.Context, id, lastKnown string) (discovery.Device, error) {
	hostHint := ""
	if u, err := url.Parse(lastKnown); err == nil {
		hostHint = u.Hostname()
	}
	if devs, err := discovery.Discover(ctx, 2*time.Second); err == nil {
		for _, d := range devs {
			if d.ID == id {
				return d, nil
			}
		}
	}
	if ip := net.ParseIP(hostHint); ip != nil {
		if p, ok := discovery.PrefixAround(ip); ok {
			for _, d := range discovery.Scan(ctx, p, id) {
				if d.ID == id {
					return d, nil
				}
			}
		}
	}
	return discovery.Device{}, fmt.Errorf("tuner %s not found", id)
}

// localIPs lists this host's IPv4 addresses; a stream whose TargetIP is one of
// them is flowing through Yagi.
func localIPs() []string {
	var out []string
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
			out = append(out, ipn.IP.String())
		}
	}
	return out
}
