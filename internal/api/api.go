// Package api is the JSON/SSE interface the web UI talks to. It is meant to be
// served behind access.Policy.AdminOnly: it can change who may use the tuner.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/johnschult/yagi/internal/access"
	"github.com/johnschult/yagi/internal/discovery"
	"github.com/johnschult/yagi/internal/firmware"
	"github.com/johnschult/yagi/internal/guide"
	"github.com/johnschult/yagi/internal/hdhr"
	"github.com/johnschult/yagi/internal/history"
	"github.com/johnschult/yagi/internal/httpx"
	"github.com/johnschult/yagi/internal/lineup"
	"github.com/johnschult/yagi/internal/live"
	"github.com/johnschult/yagi/internal/logbuf"
	"github.com/johnschult/yagi/internal/netinfo"
	"github.com/johnschult/yagi/internal/restart"
	"github.com/johnschult/yagi/internal/tailnet"
	"github.com/johnschult/yagi/internal/tuner"
	"github.com/johnschult/yagi/internal/tunercheck"
	"github.com/johnschult/yagi/internal/tunerinfo"
	"github.com/johnschult/yagi/internal/upgrade"
	"github.com/johnschult/yagi/internal/version"
)

const (
	maxBody     = httpx.MaxBody // settings are tiny; anything bigger is a mistake or an attack
	keepAlive   = 20 * time.Second
	coalesceFor = 100 * time.Millisecond // bursts of changes become one event
)

// Fleet is the set of tuners Yagi is fronting, which can change while it
// runs. The server provides it.
type Fleet interface {
	Devices() []Device
	// AddDevice starts fronting a tuner and saves it to the settings. Fields left
	// empty (the name, the port) are filled in. It returns the device as saved.
	AddDevice(ctx context.Context, d access.Device) (access.Device, error)
	// RemoveDevice stops fronting a tuner, ends any streams on it and removes it
	// from the settings.
	RemoveDevice(name string) error
	// SetListen moves a tuner to another port, keeping the old one if the new
	// can't be opened. It returns the device as saved.
	SetListen(name, listen string) (access.Device, error)
}

// NewError is an error that reaches the person as this status and message.
func NewError(code int, msg string) error { return &httpErr{code: code, msg: msg} }

// TailnetControl is the built-in Tailscale node: its status, and a nudge to match the settings after they change.
type TailnetControl interface {
	Reconcile()
	Status() tailnet.Status
}

// API serves /api/*.
type API struct {
	// Tailnet is the built-in Tailscale node; nil means this server has none.
	Tailnet TailnetControl
	// Devices is a fixed set of tuners, for when Fleet is nil.
	Devices []Device
	// Fleet, when set, is where the tuners come from, and what adds and removes
	// them.
	Fleet Fleet
	// Discover looks for tuners on the network. Nil means discovery.Discover.
	Discover func(ctx context.Context, wait time.Duration) ([]discovery.Device, error)
	// Describe returns what a tuner at an address calls itself, for offering a name. Nil means asking it over HTTP.
	Describe func(ctx context.Context, address string) string
	// Firmware reads SiliconDust's changelog; nil means no firmware checking.
	Firmware *firmware.Checker
	// Logs holds the recent log lines for the Logs page; nil means there is no page.
	Logs *logbuf.Buffer
	// Guide reads SiliconDust's program guide; nil means no guide.
	Guide *guide.Service
	Pol   *access.Policy
	Store *access.Store
	Hub   *live.Hub
	Log   *slog.Logger
	// Network lists the addresses this machine can be reached at, for the setup
	// guide. Nil means netinfo.Detect; tests supply their own.
	Network func() []netinfo.Address
	// Firewall says which firewall looks to be in use here. Nil means
	// netinfo.DetectFirewall.
	Firewall func() netinfo.Firewall
	// KeepAlive is how often an idle event stream gets a comment line, so
	// proxies don't drop it. Zero means 20s.
	KeepAlive time.Duration

	// Upgrade installs firmware on a tuner; nil means it can't.
	Upgrade *upgrade.Manager
	// Restart restarts a tuner when asked; nil means it can't.
	Restart *restart.Manager
	// Lineup reads and changes a tuner's channels; nil means it can't.
	Lineup *lineup.Client
	// Control opens a control connection to a tuner's host, for testing a channel's signal; nil means it can't.
	Control func(ctx context.Context, host string) (*hdhr.Conn, error)
	// SignalTest sets the timings of a signal test; the zero value is what it should be. Tests make them short.
	SignalTest hdhr.TestOptions
	testing    sync.Map // device name -> a signal test is running on it
	// History is who watched what, for the Activity page; nil means there is none.
	History *history.Recorder
	// Check walks a tuner's side of a failed stream, for the Check now button; nil means it can't.
	Check *tunercheck.Checker
	// Device reads what a tuner says about itself, for the Device panel; nil means it can't.
	Device *tunerinfo.Reader
	// Notify sends test messages to a notification channel; nil means there is no
	// notifying, and the test button says so.
	Notify Tester

	searching atomic.Bool // a network search is under way
	testMu    sync.Mutex
	tested    map[string]time.Time // when each channel was last tested
}

// Device is one fronted tuner: its name, where media servers reach it, and the
// monitor watching it.
type Device struct {
	Name   string
	Listen string
	Mon    *tuner.Monitor
}

// Prefix is where the API is mounted.
const Prefix = "/api/"

func (a *API) Handler() http.Handler {
	if a.Log == nil {
		a.Log = slog.Default()
	}
	mux := http.NewServeMux()
	routes := []struct {
		path string
		by   map[string]http.HandlerFunc
	}{
		{"state", map[string]http.HandlerFunc{"GET": a.getState}},
		{"events", map[string]http.HandlerFunc{"GET": a.events}},
		{"viewers", map[string]http.HandlerFunc{"GET": a.listViewers, "POST": a.addViewer}},
		{"viewers/{name}", map[string]http.HandlerFunc{"PUT": a.replaceViewer, "DELETE": a.deleteViewer}},
		{"settings", map[string]http.HandlerFunc{"GET": a.getSettings, "PATCH": a.patchSettings}},
		{"devices", map[string]http.HandlerFunc{"POST": a.addDevice}},
		{"devices/{name}", map[string]http.HandlerFunc{"PATCH": a.patchDevice, "DELETE": a.deleteDevice}},
		{"discover", map[string]http.HandlerFunc{"POST": a.discover}},
		{"devices/{name}/guide/{channel}/icon", map[string]http.HandlerFunc{"GET": a.channelIcon}},
		{"devices/{name}/guide/{channel}/poster/{token}", map[string]http.HandlerFunc{"GET": a.programPoster}},
		{"network", map[string]http.HandlerFunc{"GET": a.network}},
		{"firmware/check", map[string]http.HandlerFunc{"POST": a.checkFirmware}},
		{"devices/{name}/channels", map[string]http.HandlerFunc{"GET": a.getChannels}},
		{"devices/{name}/channel-scan", map[string]http.HandlerFunc{"POST": a.startScan, "DELETE": a.stopScan}},
		{"devices/{name}/channels/{number}", map[string]http.HandlerFunc{"PUT": a.setChannel}},
		{"devices/{name}/channels/{number}/test", map[string]http.HandlerFunc{"POST": a.testSignal}},
		{"history", map[string]http.HandlerFunc{"GET": a.getHistory, "DELETE": a.clearHistory}},
		{"logs", map[string]http.HandlerFunc{"GET": a.getLogs, "DELETE": a.clearLogs}},
		{"devices/{name}/restart", map[string]http.HandlerFunc{"POST": a.restartTuner, "DELETE": a.dismissRestart}},
		{"devices/{name}/info", map[string]http.HandlerFunc{"GET": a.deviceInfo}},
		{"devices/{name}/check", map[string]http.HandlerFunc{"POST": a.checkTuner}},
		{"devices/{name}/firmware/upgrade", map[string]http.HandlerFunc{"POST": a.installFirmware, "DELETE": a.dismissUpgrade}},
		{"plex/admins/{id}", map[string]http.HandlerFunc{"DELETE": a.removePlexAdmin}},
		{"requests", map[string]http.HandlerFunc{"GET": a.listRequests}},
		{"requests/{id}", map[string]http.HandlerFunc{"DELETE": a.dismissRequest}},
		{"requests/{id}/allow", map[string]http.HandlerFunc{"POST": a.allowRequest}},
		{"notifications/channels", map[string]http.HandlerFunc{"POST": a.addChannel}},
		{"notifications/channels/{id}", map[string]http.HandlerFunc{"PUT": a.replaceChannel, "DELETE": a.deleteChannel}},
		{"notifications/test", map[string]http.HandlerFunc{"POST": a.testChannel}},
	}
	for _, rt := range routes {
		var allow []string
		for method, h := range rt.by {
			mux.HandleFunc(method+" "+Prefix+rt.path, h)
			allow = append(allow, method)
		}
		sort.Strings(allow)
		// Any other method on a known path: a JSON 405 instead of the mux's text one.
		mux.HandleFunc(Prefix+rt.path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", strings.Join(allow, ", "))
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		})
	}
	mux.HandleFunc(Prefix, func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "no such endpoint")
	})
	return guardWrites(mux)
}

// ---- the state document ----

// Summary is the tuner's capacity at a glance.
type Summary struct {
	Total    int `json:"total"`    // tuners on the device
	InUse    int `json:"inUse"`    // in use by anyone
	Remote   int `json:"remote"`   // of those, through Yagi
	Local    int `json:"local"`    // of those, the owner's own devices
	Reserved int `json:"reserved"` // always kept free for the owner
	Free     int `json:"free"`
}

// SettingsDoc is the part of the settings that isn't per device or per viewer.
// OwnerReserve and SharedCap are the defaults devices fall back to.
type SettingsDoc struct {
	Mode          string       `json:"mode"` // "no-viewers" or "enforced"
	SharedCap     int          `json:"sharedCap"`
	OwnerReserve  int          `json:"ownerReserve"`
	Admin         []string     `json:"admin"`
	FirmwareCheck bool         `json:"firmwareCheck"`
	Guide         bool         `json:"guide"`
	History       bool         `json:"history"`
	LogLevel      string       `json:"logLevel"`
	Plex          PlexDoc      `json:"plex"`
	Tailscale     TailscaleDoc `json:"tailscale"`
	// Notifications is where messages go and which events send them, without
	// the secrets in the channels.
	Notifications NotificationsDoc `json:"notifications"`
}

// PlexDoc is the state of signing in with Plex. The client ID is deliberately
// not here.
type PlexDoc struct {
	Enabled bool                 `json:"enabled"`
	Admins  []access.PlexAccount `json:"admins"`
}

// TailscaleDoc is the settings for the built-in Tailscale node. Where the node is now is State.Tailscale.
type TailscaleDoc struct {
	Enabled     bool   `json:"enabled"`
	Hostname    string `json:"hostname"`
	TailnetOnly bool   `json:"tailnetOnly"`
	HTTPS       bool   `json:"https"`
}

// DeviceDoc is one tuner as the dashboard shows it. OwnerReserve and
// SharedCap are the values in force for it.
type DeviceDoc struct {
	Name         string          `json:"name"`
	Label        string          `json:"label,omitempty"` // display name, if the owner set one
	Listen       string          `json:"listen"`
	Tuner        tuner.State     `json:"tuner"`
	Streams      []access.Active `json:"streams"`
	Summary      Summary         `json:"summary"`
	OwnerReserve int             `json:"ownerReserve"`
	SharedCap    int             `json:"sharedCap"`
	Firmware     firmware.Status `json:"firmware"`
	// Upgrade is the latest firmware install on this tuner, while it runs and after: what it is doing, or
	// how it ended. Absent when there has been none.
	Upgrade *upgrade.Status `json:"upgrade,omitempty"`
	// Restart is the latest restart of this tuner someone asked for, while it runs and after.
	Restart *restart.Status `json:"restart,omitempty"`
	// Guide is what the program guide says about each channel being watched
	// right now, by channel number. Empty when the guide is off or unknown.
	Guide map[string]guide.Entry `json:"guide"`
}

// State is everything the dashboard shows.
type State struct {
	Time    time.Time             `json:"time"`
	Devices []DeviceDoc           `json:"devices"`
	Viewers []access.ViewerStatus `json:"viewers"`
	// Requests are people who asked to use a tuner and aren't viewers yet, to be allowed.
	Requests []RequestDoc `json:"requests"`
	Settings SettingsDoc  `json:"settings"`
	Editable bool         `json:"editable"` // false: started without -config
	// Tailscale is the built-in Tailscale node: whether this build has it, and where it is.
	Tailscale tailnet.Status `json:"tailscale"`
	// Version is this build's version, VersionURL its page on GitHub, and DocsURL
	// the documentation site.
	Version    string `json:"version"`
	VersionURL string `json:"versionUrl"`
	DocsURL    string `json:"docsUrl"`
}

func (a *API) deviceDoc(d Device, streams []access.Active) DeviceDoc {
	t := d.Mon.Snapshot()
	reserve, capN := a.Pol.Limits(d.Name)
	total, via := t.InUse()
	free := t.TunerCount - total
	if free < 0 {
		free = 0
	}
	doc := DeviceDoc{
		Name: d.Name, Label: a.Pol.Label(d.Name), Listen: d.Listen, Tuner: t, Streams: []access.Active{},
		OwnerReserve: reserve, SharedCap: capN,
		Firmware: a.Firmware.StatusFor(firmware.Device{
			Model: t.Model, FirmwareName: t.Firmware.Name, Version: t.Firmware.Version, UpgradeAvailable: t.Firmware.UpgradeAvailable,
		}),
		Summary: Summary{Total: t.TunerCount, InUse: total, Remote: via, Local: total - via, Reserved: reserve, Free: free},
	}
	if a.Upgrade != nil {
		doc.Upgrade = a.Upgrade.Status(d.Name)
	}
	if a.Restart != nil {
		doc.Restart = a.Restart.Status(d.Name)
	}
	doc.Guide = map[string]guide.Entry{}
	if a.Guide != nil && a.Pol.Settings().GuideEnabled() {
		now := time.Now()
		for _, sl := range t.Slots {
			if !sl.InUse || sl.Channel == "" {
				continue
			}
			if e, ok := a.Guide.Lookup(d.Name, sl.Channel, now); ok {
				doc.Guide[sl.Channel] = e
			}
		}
	}
	for _, s := range streams {
		if s.Device == d.Name {
			doc.Streams = append(doc.Streams, s)
		}
	}
	if doc.Tuner.Slots == nil {
		doc.Tuner.Slots = []tuner.Slot{}
	}
	if doc.Tuner.Events == nil {
		doc.Tuner.Events = []tuner.Event{}
	}
	return doc
}

func (a *API) state() State {
	set := a.Pol.Settings()
	streams := a.Pol.Active()
	all := a.devices()
	devices := make([]DeviceDoc, 0, len(all))
	for _, d := range all {
		devices = append(devices, a.deviceDoc(d, streams))
	}
	return State{
		Time:     time.Now(),
		Devices:  devices,
		Viewers:  a.Pol.ViewerStatuses(),
		Requests: requestDocs(a.Pol.Requests()),
		Settings: SettingsDoc{
			Mode: a.Pol.Mode(), SharedCap: set.SharedCap, OwnerReserve: set.OwnerReserve, Admin: nonNil(set.Admin),
			FirmwareCheck: set.FirmwareCheckEnabled(),
			Guide:         set.GuideEnabled(),
			History:       set.HistoryEnabled(),
			LogLevel:      set.LogLevelName(),
			Plex:          plexDoc(set),
			Tailscale:     TailscaleDoc{Enabled: set.TailscaleEnabled(), Hostname: set.TailscaleHostname(), TailnetOnly: set.Tailscale != nil && set.Tailscale.TailnetOnly, HTTPS: set.Tailscale != nil && set.Tailscale.HTTPS},
			Notifications: notificationsDoc(set),
		},
		Editable:   a.Store.Editable(),
		Tailscale:  a.tailscale(),
		Version:    version.Get(),
		VersionURL: version.URL(version.Get()),
		DocsURL:    version.DocsURL(),
	}
}

func plexDoc(set access.Settings) PlexDoc {
	d := PlexDoc{Enabled: set.PlexEnabled(), Admins: []access.PlexAccount{}}
	if set.Plex != nil {
		d.Admins = append(d.Admins, set.Plex.Admins...)
	}
	return d
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (a *API) getState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.state())
}

// ---- live updates ----

// events streams the state as server-sent events: one right away, then one
// after each change, plus a comment every 20s so proxies keep the line open.
func (a *API) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming is not supported here")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // don't let nginx buffer the stream
	w.WriteHeader(http.StatusOK)

	send := func() error {
		b, err := json.Marshal(a.state())
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: state\ndata: %s\n\n", b); err != nil {
			return err
		}
		fl.Flush()
		return nil
	}

	changes, cancel := a.Hub.Subscribe()
	defer cancel()
	if send() != nil {
		return
	}
	every := a.KeepAlive
	if every == 0 {
		every = keepAlive
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-changes:
			// Let a burst of changes settle, then send once.
			select {
			case <-time.After(coalesceFor):
			case <-r.Context().Done():
				return
			}
			select { // drop the signal(s) that arrived while we waited
			case <-changes:
			default:
			}
			if send() != nil {
				return
			}
		case <-tick.C:
			if _, err := io.WriteString(w, ": keep-alive\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

// ---- viewers ----

type viewerInput struct {
	Name      string         `json:"name"`
	Addresses []string       `json:"addresses"`
	Limit     int            `json:"limit"`
	Limits    map[string]int `json:"limits"` // per-device overrides, by device name
}

func (v viewerInput) viewer() access.Viewer {
	var limits map[string]int
	if len(v.Limits) > 0 {
		limits = v.Limits
	}
	return access.Viewer{Name: strings.TrimSpace(v.Name), Addresses: nonNil(v.Addresses), Limit: v.Limit, Limits: limits}
}

func (a *API) listViewers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.Pol.ViewerStatuses())
}

func (a *API) viewerStatus(name string) (access.ViewerStatus, bool) {
	for _, v := range a.Pol.ViewerStatuses() {
		if strings.EqualFold(v.Name, name) {
			return v, true
		}
	}
	return access.ViewerStatus{}, false
}

func indexOf(vs []access.Viewer, name string) int {
	for i, v := range vs {
		if strings.EqualFold(v.Name, strings.TrimSpace(name)) {
			return i
		}
	}
	return -1
}

// refusePublic is the error for an address list that names a public internet address. Yagi serves private networks
// only (this network, a VPN, Tailscale), so one could never connect.
func refusePublic(entries []string) error {
	if pub := access.PublicEntries(entries); len(pub) > 0 {
		return &httpErr{http.StatusBadRequest, fmt.Sprintf("%s is a public internet address. Yagi serves private networks only: put the viewer on Tailscale or a VPN, and list the address Yagi sees from there", pub[0])}
	}
	return nil
}

func (a *API) addViewer(w http.ResponseWriter, r *http.Request) {
	var in viewerInput
	if !decode(w, r, &in) {
		return
	}
	v := in.viewer()
	if err := refusePublic(v.Addresses); err != nil {
		a.fail(w, err)
		return
	}
	_, err := a.Store.Edit(func(s *access.Settings) error {
		if indexOf(s.Viewers, v.Name) >= 0 {
			return &httpErr{http.StatusConflict, fmt.Sprintf("viewer %q already exists", v.Name)}
		}
		s.Viewers = append(s.Viewers, v)
		return nil
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	vs, _ := a.viewerStatus(v.Name)
	writeJSON(w, http.StatusCreated, vs)
}

func (a *API) replaceViewer(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var in viewerInput
	if !decode(w, r, &in) {
		return
	}
	v := in.viewer()
	if err := refusePublic(v.Addresses); err != nil {
		a.fail(w, err)
		return
	}
	_, err := a.Store.Edit(func(s *access.Settings) error {
		i := indexOf(s.Viewers, name)
		if i < 0 {
			return &httpErr{http.StatusNotFound, fmt.Sprintf("no viewer named %q", name)}
		}
		s.Viewers[i] = v
		return nil
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	vs, _ := a.viewerStatus(v.Name)
	writeJSON(w, http.StatusOK, vs)
}

func (a *API) deleteViewer(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	_, err := a.Store.Edit(func(s *access.Settings) error {
		i := indexOf(s.Viewers, name)
		if i < 0 {
			return &httpErr{http.StatusNotFound, fmt.Sprintf("no viewer named %q", name)}
		}
		s.Viewers = append(s.Viewers[:i:i], s.Viewers[i+1:]...)
		return nil
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- settings ----

func (a *API) tailscale() tailnet.Status {
	if a.Tailnet == nil {
		return tailnet.Status{State: tailnet.Unavailable, Hostname: access.DefaultTailscaleHostname}
	}
	return a.Tailnet.Status()
}

func (a *API) settingsDoc() SettingsDoc { return a.state().Settings }

func (a *API) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.settingsDoc())
}

// settingsPatch changes only the fields that are present.
type settingsPatch struct {
	SharedCap     *int      `json:"sharedCap"`
	OwnerReserve  *int      `json:"ownerReserve"`
	Admin         *[]string `json:"admin"`
	FirmwareCheck *bool     `json:"firmwareCheck"`
	Guide         *bool     `json:"guide"`
	History       *bool     `json:"history"`
	LogLevel      *string   `json:"logLevel"`
	PlexEnabled   *bool     `json:"plexEnabled"`
	// TailscaleEnabled turns the built-in Tailscale node on and off, and TailscaleHostname names it on the tailnet.
	TailscaleEnabled  *bool   `json:"tailscaleEnabled"`
	TailscaleHostname *string `json:"tailscaleHostname"`
	// TailnetOnly refuses connections from the LAN while the node is connected. It can only be turned on while the node
	// is connected, so it is never switched on with nothing to take its place.
	TailnetOnly *bool `json:"tailnetOnly"`
	// TailscaleHTTPS also serves the admin pages over HTTPS at the node's tailnet name.
	TailscaleHTTPS *bool `json:"tailscaleHttps"`
	// NotifyEvents switches kinds of event on and off for notifications.
	NotifyEvents *notifyEventsPatch `json:"notifyEvents"`
}

func (a *API) patchSettings(w http.ResponseWriter, r *http.Request) {
	var in settingsPatch
	if !decode(w, r, &in) {
		return
	}
	_, err := a.Store.Edit(func(s *access.Settings) error {
		if in.SharedCap != nil {
			s.SharedCap = *in.SharedCap
		}
		if in.OwnerReserve != nil {
			s.OwnerReserve = *in.OwnerReserve
		}
		if in.Admin != nil {
			if err := refusePublic(*in.Admin); err != nil {
				return err
			}
			s.Admin = *in.Admin
		}
		if in.FirmwareCheck != nil {
			b := *in.FirmwareCheck
			s.FirmwareCheck = &b
		}
		if in.Guide != nil {
			b := *in.Guide
			s.Guide = &b
		}
		if in.LogLevel != nil {
			if !slices.Contains(access.LogLevels, *in.LogLevel) {
				return &httpErr{http.StatusBadRequest, fmt.Sprintf("logLevel must be one of %s", strings.Join(access.LogLevels, ", "))}
			}
			s.LogLevel = *in.LogLevel
			if s.LogLevel == "info" {
				s.LogLevel = "" // the default is not written
			}
		}
		if in.History != nil {
			b := *in.History
			s.History = &b
		}
		if in.PlexEnabled != nil {
			if s.Plex == nil {
				s.Plex = &access.PlexSettings{}
			}
			s.Plex.Enabled = *in.PlexEnabled
		}
		if in.TailnetOnly != nil {
			if *in.TailnetOnly && (!s.TailscaleEnabled() || a.tailscale().State != tailnet.Running) {
				return &httpErr{http.StatusConflict, "Yagi's own Tailscale has to be on and connected first, so there is a way in once the LAN is closed"}
			}
			if s.Tailscale == nil {
				s.Tailscale = &access.TailscaleSettings{}
			}
			s.Tailscale.TailnetOnly = *in.TailnetOnly
		}
		if in.TailscaleEnabled != nil || in.TailscaleHostname != nil || in.TailscaleHTTPS != nil {
			if s.Tailscale == nil {
				s.Tailscale = &access.TailscaleSettings{}
			}
			if in.TailscaleEnabled != nil {
				s.Tailscale.Enabled = *in.TailscaleEnabled
			}
			if in.TailscaleHTTPS != nil {
				s.Tailscale.HTTPS = *in.TailscaleHTTPS
			}
			if in.TailscaleHostname != nil {
				h := strings.ToLower(strings.TrimSpace(*in.TailscaleHostname))
				if h == access.DefaultTailscaleHostname {
					h = "" // the default is not written
				}
				if h != "" {
					if err := access.ValidateTailscaleHostname(h); err != nil {
						return &httpErr{http.StatusBadRequest, err.Error()}
					}
				}
				s.Tailscale.Hostname = h
			}
		}
		if in.NotifyEvents != nil {
			// There is nothing to be told about while the firmware check is off. (Turning the check
			// off later leaves the switch as it was, and it takes effect again when the check is on.)
			if in.NotifyEvents.Firmware != nil && *in.NotifyEvents.Firmware && !s.FirmwareCheckEnabled() {
				return &httpErr{http.StatusBadRequest, "turn on “Check for new tuner firmware” first: there is nothing to tell you about while it is off"}
			}
			if s.Notifications == nil {
				s.Notifications = &access.Notifications{}
			}
			in.NotifyEvents.apply(s.Notifications)
		}
		return nil
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	if in.FirmwareCheck != nil && *in.FirmwareCheck && a.Firmware != nil {
		a.Firmware.Kick() // switched on: look now rather than at the next interval
	}
	if in.Guide != nil && *in.Guide && a.Guide != nil {
		a.Guide.Kick()
	}
	if (in.TailscaleEnabled != nil || in.TailscaleHostname != nil || in.TailnetOnly != nil || in.TailscaleHTTPS != nil) && a.Tailnet != nil {
		a.Tailnet.Reconcile()
	}
	writeJSON(w, http.StatusOK, a.settingsDoc())
}

// devicePatch changes only the fields that are present.
type devicePatch struct {
	SharedCap    *int    `json:"sharedCap"`
	OwnerReserve *int    `json:"ownerReserve"`
	Label        *string `json:"label"`  // "" clears it
	Listen       *string `json:"listen"` // ":5010"; a bare port number also works
}

// patchDevice sets a device's display name, port, owner reserve and shared cap. A device declared
// in the settings file keeps its own values; a device that comes from
// command-line flags has none of its own, so the top-level ones are changed.
func (a *API) patchDevice(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	dev := a.device(name)
	if dev == nil {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("no device named %q", name))
		return
	}
	var in devicePatch
	if !decode(w, r, &in) {
		return
	}
	if in.Listen != nil {
		if a.Fleet == nil {
			writeErr(w, http.StatusConflict, "the port can't be changed here")
			return
		}
		if _, err := a.Fleet.SetListen(name, *in.Listen); err != nil {
			a.fail(w, err)
			return
		}
		if d := a.device(name); d != nil {
			dev = d
		}
	}
	_, err := a.Store.Edit(func(s *access.Settings) error {
		if in.Label != nil {
			label := *in.Label
			if label == "" {
				delete(s.Labels, name)
			} else {
				if s.Labels == nil {
					s.Labels = map[string]string{}
				}
				s.Labels[name] = label
			}
		}
		for i := range s.Devices {
			if s.Devices[i].Name != name {
				continue
			}
			if in.SharedCap != nil {
				n := *in.SharedCap
				s.Devices[i].SharedCap = &n
			}
			if in.OwnerReserve != nil {
				n := *in.OwnerReserve
				s.Devices[i].OwnerReserve = &n
			}
			return nil
		}
		if in.SharedCap != nil {
			s.SharedCap = *in.SharedCap
		}
		if in.OwnerReserve != nil {
			s.OwnerReserve = *in.OwnerReserve
		}
		return nil
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.deviceDoc(*dev, a.Pol.Active()))
}

// devices is the tuners now being fronted.
func (a *API) devices() []Device {
	if a.Fleet != nil {
		return a.Fleet.Devices()
	}
	return a.Devices
}

func (a *API) device(name string) *Device {
	for _, d := range a.devices() {
		if d.Name == name {
			d := d
			return &d
		}
	}
	return nil
}

// ---- adding, finding and removing tuners ----

// deviceInput is a tuner to start fronting. Only the address (or DeviceID) is
// needed; the name and port are chosen if left out.
type deviceInput struct {
	Label  string `json:"label"`
	Name   string `json:"name"`
	Listen string `json:"listen"`
	Tuner  string `json:"tuner"`
	ID     string `json:"id"`
}

func (a *API) addDevice(w http.ResponseWriter, r *http.Request) {
	if a.Fleet == nil {
		writeErr(w, http.StatusConflict, "tuners can't be added here")
		return
	}
	var in deviceInput
	if !decode(w, r, &in) {
		return
	}
	d, err := a.Fleet.AddDevice(r.Context(), access.Device{
		Name: strings.TrimSpace(in.Name), Listen: strings.TrimSpace(in.Listen),
		Tuner: strings.TrimSpace(in.Tuner), ID: strings.ToUpper(strings.TrimSpace(in.ID)),
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	if label := strings.TrimSpace(in.Label); label != "" {
		if _, err := a.Store.Edit(func(s *access.Settings) error {
			if s.Labels == nil {
				s.Labels = map[string]string{}
			}
			s.Labels[d.Name] = label
			return nil
		}); err != nil {
			a.Log.Warn("the tuner was added but its display name wasn't saved", "device", d.Name, "err", err)
		}
	}
	dev := a.device(d.Name)
	if dev == nil {
		writeErr(w, http.StatusInternalServerError, "the tuner was added but can't be shown")
		return
	}
	writeJSON(w, http.StatusCreated, a.deviceDoc(*dev, a.Pol.Active()))
}

func (a *API) deleteDevice(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if a.Fleet == nil {
		writeErr(w, http.StatusConflict, "tuners can't be removed here")
		return
	}
	if a.device(name) == nil {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("no device named %q", name))
		return
	}
	if err := a.Fleet.RemoveDevice(name); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Found is a tuner seen on the network.
type Found struct {
	ID      string `json:"id"`
	Address string `json:"address"` // where it answered
	Tuners  int    `json:"tuners"`  // how many tuners it has
	Added   string `json:"added"`   // the device it is already, if it is one
	Label   string `json:"label"`   // a display name to offer, from the tuner's own name
	Name    string `json:"name"`    // a device name to offer, such as flex-4k-87; it shows in addresses
}

const discoverWait = 3 * time.Second

// discover looks for HDHomeRuns on the network now. It is a POST because it
// sends broadcasts, and one search at a time is plenty.
func (a *API) discover(w http.ResponseWriter, r *http.Request) {
	if !a.searching.CompareAndSwap(false, true) {
		writeErr(w, http.StatusTooManyRequests, "already looking; try again in a moment")
		return
	}
	defer a.searching.Store(false)
	find := a.Discover
	if find == nil {
		find = func(ctx context.Context, wait time.Duration) ([]discovery.Device, error) {
			return discovery.Discover(ctx, wait)
		}
	}
	found, err := find(r.Context(), discoverWait)
	if err != nil && len(found) == 0 {
		a.Log.Warn("looking for tuners failed", "err", err)
		writeErr(w, http.StatusBadGateway, "couldn't search the network for tuners")
		return
	}
	have := map[string]string{} // DeviceID -> device name
	for _, d := range a.devices() {
		if id := strings.ToUpper(d.Mon.Snapshot().ID); id != "" {
			have[id] = d.Name
		}
	}
	for _, d := range a.Pol.Settings().Devices {
		if d.ID != "" {
			have[strings.ToUpper(d.ID)] = d.Name
		}
	}
	out := make([]Found, 0, len(found))
	for _, f := range found {
		out = append(out, Found{ID: f.ID, Address: f.BaseURL, Tuners: f.TunerCount, Added: have[strings.ToUpper(f.ID)]})
	}
	a.nameFound(r.Context(), out)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	writeJSON(w, http.StatusOK, map[string]any{"tuners": out})
}

// checkFirmware reads the changelog now, rather than waiting for the next
// scheduled look. The new status reaches the UI through the event stream.
func (a *API) checkFirmware(w http.ResponseWriter, r *http.Request) {
	if a.Firmware == nil {
		writeErr(w, http.StatusConflict, "the firmware check is not available")
		return
	}
	switch err := a.Firmware.Refresh(r.Context()); {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"checkedAt": a.Firmware.CheckedAt()})
	case errors.Is(err, firmware.ErrTooSoon):
		writeErr(w, http.StatusTooManyRequests, "the changelog was checked a moment ago; try again in a little while")
	case !a.Pol.Settings().FirmwareCheckEnabled():
		writeErr(w, http.StatusConflict, "the firmware check is turned off in the settings")
	default:
		a.Log.Warn("firmware changelog check failed", "err", err)
		writeErr(w, http.StatusBadGateway, "couldn't read SiliconDust's changelog: "+err.Error())
	}
}

// removePlexAdmin takes a Plex account off the list of those who may sign in.
// Anyone signed in as that account is signed out by their next request.
func (a *API) removePlexAdmin(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "that is not a Plex account ID")
		return
	}
	_, err = a.Store.Edit(func(s *access.Settings) error {
		if s.Plex != nil {
			for i, acct := range s.Plex.Admins {
				if acct.ID == id {
					s.Plex.Admins = append(s.Plex.Admins[:i:i], s.Plex.Admins[i+1:]...)
					return nil
				}
			}
		}
		return &httpErr{http.StatusNotFound, fmt.Sprintf("no Plex account %d is allowed to sign in", id)}
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- plumbing ----

type httpErr struct {
	code int
	msg  string
}

func (e *httpErr) Error() string { return e.msg }

func (a *API) fail(w http.ResponseWriter, err error) {
	var he *httpErr
	var ve *access.ValidationError
	switch {
	case errors.As(err, &he):
		writeErr(w, he.code, he.msg)
	case errors.As(err, &ve):
		writeErr(w, http.StatusBadRequest, ve.Error())
	case errors.Is(err, access.ErrNoFile):
		writeErr(w, http.StatusConflict, err.Error())
	default:
		a.Log.Error("settings edit failed", "err", err)
		writeErr(w, http.StatusInternalServerError, "could not save the settings")
	}
}

func writeJSON(w http.ResponseWriter, code int, v any)          { httpx.WriteJSON(w, code, v) }
func writeErr(w http.ResponseWriter, code int, msg string)      { httpx.WriteErr(w, code, msg) }
func decode(w http.ResponseWriter, r *http.Request, v any) bool { return httpx.Decode(w, r, v) }
func guardWrites(next http.Handler) http.Handler                { return httpx.GuardWrites(next) }

// channelIcon serves a channel's logo, fetched through Yagi so the
// browser never contacts SiliconDust. Only a channel the guide knows has one;
// the address it is fetched from comes from the guide, never from this request.
func (a *API) channelIcon(w http.ResponseWriter, r *http.Request) {
	if a.Guide == nil || !a.Pol.Settings().GuideEnabled() {
		writeErr(w, http.StatusNotFound, "the guide is off")
		return
	}
	body, typ, err := a.Guide.Icon(r.Context(), r.PathValue("name"), r.PathValue("channel"))
	switch {
	case errors.Is(err, guide.ErrNoIcon):
		writeErr(w, http.StatusNotFound, "no logo for that channel")
	case err != nil:
		writeErr(w, http.StatusBadGateway, err.Error())
	default:
		w.Header().Set("Content-Type", typ)
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(body)
	}
}

// programPoster serves a program's picture, fetched through Yagi so the browser never contacts
// SiliconDust. The token in the path only says which of the channel's programs is meant; the address
// it is fetched from comes from the guide. The picture at a token never changes, so it may be kept.
func (a *API) programPoster(w http.ResponseWriter, r *http.Request) {
	if a.Guide == nil || !a.Pol.Settings().GuideEnabled() {
		writeErr(w, http.StatusNotFound, "the guide is off")
		return
	}
	body, typ, err := a.Guide.Poster(r.Context(), r.PathValue("name"), r.PathValue("channel"), r.PathValue("token"))
	switch {
	case errors.Is(err, guide.ErrNoPoster):
		writeErr(w, http.StatusNotFound, "no picture for that program")
	case err != nil:
		writeErr(w, http.StatusBadGateway, err.Error())
	default:
		w.Header().Set("Content-Type", typ)
		w.Header().Set("Cache-Control", "private, max-age=86400, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(body)
	}
}

// network lists this machine's Tailscale and LAN addresses and which firewall
// looks to be in use, so the setup guide can offer the right ones. It is read
// from the machine's own interfaces and a few small files only.
func (a *API) network(w http.ResponseWriter, r *http.Request) {
	detect := a.Network
	if detect == nil {
		detect = netinfo.Detect
	}
	addrs := detect()
	if addrs == nil {
		addrs = []netinfo.Address{}
	}
	firewall := a.Firewall
	if firewall == nil {
		firewall = netinfo.DetectFirewall
	}
	writeJSON(w, http.StatusOK, map[string]any{"addresses": addrs, "firewall": firewall()})
}
