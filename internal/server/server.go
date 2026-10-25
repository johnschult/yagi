// Package server wires the pieces together: for each device a monitor, a
// proxy and a listener, around one shared access policy, API and web UI.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/johnschult/yagi/internal/access"
	"github.com/johnschult/yagi/internal/api"
	"github.com/johnschult/yagi/internal/claim"
	"github.com/johnschult/yagi/internal/diag"
	"github.com/johnschult/yagi/internal/discovery"
	"github.com/johnschult/yagi/internal/firmware"
	"github.com/johnschult/yagi/internal/guide"
	"github.com/johnschult/yagi/internal/hdhr"
	"github.com/johnschult/yagi/internal/history"
	"github.com/johnschult/yagi/internal/lineup"
	"github.com/johnschult/yagi/internal/live"
	"github.com/johnschult/yagi/internal/logbuf"
	"github.com/johnschult/yagi/internal/netinfo"
	"github.com/johnschult/yagi/internal/notify"
	"github.com/johnschult/yagi/internal/plexauth"
	"github.com/johnschult/yagi/internal/proxy"
	"github.com/johnschult/yagi/internal/restart"
	"github.com/johnschult/yagi/internal/tailnet"
	"github.com/johnschult/yagi/internal/tuner"
	"github.com/johnschult/yagi/internal/tunercheck"
	"github.com/johnschult/yagi/internal/tunerinfo"
	"github.com/johnschult/yagi/internal/ui"
	"github.com/johnschult/yagi/internal/upgrade"
	"github.com/johnschult/yagi/internal/version"
)

// Locator finds where a tuner is to start with: from its address if given,
// otherwise by searching the network for its DeviceID.
type Locator func(ctx context.Context, addr, id string) (baseURL string, err error)

// Config is everything needed to run.
type Config struct {
	// AllowLAN keeps the LAN ports open whatever the tailnet-only setting says (-allow-lan, YAGI_ALLOW_LAN): the way
	// back in if the setting leaves someone locked out, for instance in Docker, where the machine's own browser
	// does not arrive as loopback.
	AllowLAN bool
	// Settings as read from the settings file (or the defaults).
	Settings access.Settings
	// Devices to run: Settings.Devices, or one built from command-line flags.
	// Kept apart from Settings so that a flags-only setup never writes a
	// devices list into the settings file when someone edits a viewer.
	Devices    []access.Device
	ConfigPath string // where edits are saved; empty means read-only
	// AdminListen is where the admin pages are served (default ":9244"): a port of their own, which no tuner
	// can use, so the tuner ports carry nothing but what a media server asks of a tuner.
	AdminListen string
	// TailscaleAuthKey signs the built-in Tailscale node in without a link (TS_AUTHKEY). It is never saved.
	TailscaleAuthKey string
	// TailscaleStart makes the built-in Tailscale node, for tests; nil means the real one (tailnet.NewNode), which
	// is nil in a build without it.
	TailscaleStart tailnet.Starter
	Poll           time.Duration
	Log            *slog.Logger
	// LogLevel is the level the logger filters at, which the log level setting moves; nil means the setting has
	// no effect. Logs is where the Logs page reads the recent lines from; nil means there is no page to show.
	LogLevel *slog.LevelVar
	Logs     *logbuf.Buffer
	// Locate is DefaultLocate unless a test supplies another.
	Locate Locator
	// Listeners are pre-bound listeners by device name; any device without one
	// is bound from its Listen address when Run starts.
	Listeners map[string]net.Listener
	// FirmwareURL and FirmwareInterval override where and how often the
	// firmware changelog is read; they exist for tests.
	FirmwareURL      string
	FirmwareInterval time.Duration
	// GuideURL and GuideIconBase override where the program guide and
	// channel logos come from; they exist for tests, which must never reach
	// SiliconDust.
	GuideURL      string
	GuideIconBase string
	// PlexURL and PlexAuthURL override plex.tv's addresses, for tests.
	PlexURL, PlexAuthURL string
	// PushoverURL overrides where Pushover messages go, for tests.
	PushoverURL string
	// FirmwareDownloadBase overrides where firmware images are downloaded from, and ControlPort the port
	// a tuner's control protocol is on (65001), for tests and the demo, which must never reach SiliconDust or
	// flash a real tuner. UpgradeConfig, if set, may change the install's timings.
	FirmwareDownloadBase string
	ControlPort          string
	UpgradeConfig        func(*upgrade.Config)
	// RestartConfig, if set, may change the timings of restarting a tuner (the demo and tests).
	RestartConfig func(*restart.Config)
	// Find looks for a tuner that has gone missing, at another address. Nil means a UDP discover and a scan
	// of the nearby network; tests supply one that doesn't, so a fake tuner that goes quiet never sends them
	// looking for a real one.
	Find tuner.Finder
	// OfflineGrace is how long a tuner must be offline before the owner is
	// messaged; zero means two minutes. Tests shorten it.
	OfflineGrace time.Duration
}

type device struct {
	cfg     access.Device
	mon     *tuner.Monitor
	handler http.Handler

	// Set when the device is serving.
	cancel context.CancelFunc
	srv    *http.Server
	ln     net.Listener
}

// Server runs the devices.
type Server struct {
	cfg          Config
	log          *slog.Logger
	policy       *access.Policy
	store        *access.Store
	hub          *live.Hub
	nets         localNets   // the networks this machine is on, to tell a public source from one of our own
	lan          lanConns    // connections from outside the machine, so they can be cut when the LAN closes
	lanWasClosed atomic.Bool // the LAN was closed when enforceLAN last looked
	firmware     *firmware.Checker
	guide        *guide.Service
	notify       *notify.Notifier
	upgrade      *upgrade.Manager
	tunerinfo    *tunerinfo.Reader
	tunercheck   *tunercheck.Checker
	diag         *diag.Mode
	restart      *restart.Manager
	history      *history.Recorder
	events       *tuner.EventLog // nil when there is no settings file
	pastEvents   map[string][]tuner.Event
	plex         *plexauth.Auth
	claim        *claim.Claimer
	tailnet      *tailnet.Manager
	tail         tailServing  // the tailnet side of the ports
	admin        http.Handler // the admin pages, shared by every listener
	// fromFlags: the devices came from command-line flags, so there is no
	// devices list in the settings file to add to or remove from.
	fromFlags bool

	devMu   sync.RWMutex
	devices []*device

	mu        sync.Mutex
	announced map[string]string // device -> the firmware version we last told the owner about

	// fleetMu serializes adding and removing devices. The fields below it are
	// set by Run and used by them.
	fleetMu sync.Mutex
	runCtx  context.Context // nil until Run
	errc    chan error
	adminLn *adminListener // serves the admin pages
}

// deviceList is a copy of the devices now running.
func (s *Server) deviceList() []*device {
	s.devMu.RLock()
	defer s.devMu.RUnlock()
	return append([]*device(nil), s.devices...)
}

// New builds the server: it finds each tuner and prepares everything, but
// listens on nothing until Run. With no tuners it still works: the admin pages
// are served on their own so tuners can be added from there.
func New(ctx context.Context, cfg Config) (*Server, error) {
	if err := (access.Settings{Devices: cfg.Devices}).Validate(); err != nil {
		return nil, err
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Poll == 0 {
		cfg.Poll = 5 * time.Second
	}
	if cfg.Locate == nil {
		cfg.Locate = DefaultLocate
	}
	if cfg.AdminListen == "" {
		cfg.AdminListen = access.DefaultAdminListen
		if cfg.Listeners != nil { // tests hand in their own listeners and must not all fight over one real port
			cfg.AdminListen = "127.0.0.1:0"
		}
	}

	policy, err := access.NewPolicy(cfg.Settings, cfg.Log)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg: cfg, log: cfg.Log, policy: policy, hub: live.NewHub(), announced: map[string]string{}, nets: localNets{read: interfaceNets},
		fromFlags: len(cfg.Settings.Devices) == 0 && len(cfg.Devices) > 0,
	}
	s.firmware = firmware.NewChecker(firmware.Config{
		URL: cfg.FirmwareURL, Interval: cfg.FirmwareInterval, Logger: cfg.Log,
		// Read live, so switching the setting takes effect at once.
		Enabled:  func() bool { return policy.Settings().FirmwareCheckEnabled() },
		OnChange: func() { s.hub.Notify(); s.announceFirmware() },
	})

	s.guide = guide.NewService(guide.Config{
		URL: cfg.GuideURL, IconBase: cfg.GuideIconBase, Logger: cfg.Log,
		Enabled:  func() bool { return policy.Settings().GuideEnabled() },
		OnChange: s.hub.Notify,
	})

	// Messages to the owner. Nothing is contacted until a channel is set up, and the
	// settings are read on each event, so changing them needs no restart.
	s.notify = notify.New(notify.Config{
		Settings: policy.Settings, Log: cfg.Log, PushoverURL: cfg.PushoverURL, OfflineGrace: cfg.OfflineGrace,
		StatePath: notifiedFile(cfg.ConfigPath),
	})
	policy.SetEvents(s.notify)
	if cfg.LogLevel != nil {
		// The level follows the setting, now and whenever it changes (an edit, or a reload of the file).
		policy.SetOnSettings(func(set access.Settings) { cfg.LogLevel.Set(logLevel(set.LogLevelName())) })
	}

	// Installing firmware: only ever started by an admin pressing the button, one tuner at a time.
	ucfg := upgrade.Config{
		Download: firmware.Downloader{Base: cfg.FirmwareDownloadBase},
		OnChange: s.hub.Notify,
		Log:      cfg.Log,
	}
	if cfg.ControlPort != "" {
		port := cfg.ControlPort
		ucfg.Dial = func(ctx context.Context, host string) (*hdhr.Conn, error) {
			return hdhr.Dial(ctx, net.JoinHostPort(host, port))
		}
	}
	if cfg.UpgradeConfig != nil {
		cfg.UpgradeConfig(&ucfg)
	}
	s.upgrade = upgrade.New(ctx, ucfg)
	dial := ucfg.Dial
	if dial == nil {
		dial = func(ctx context.Context, host string) (*hdhr.Conn, error) {
			return hdhr.Dial(ctx, net.JoinHostPort(host, hdhr.ControlPort))
		}
	}
	s.tunerinfo = &tunerinfo.Reader{Dial: dial}
	s.tunercheck = &tunercheck.Checker{Dial: dial}
	s.diag = &diag.Mode{OnChange: func(on bool) {
		if on {
			cfg.Log.Info("diagnostics mode is on: each request is recorded, and the tuner is checked when it fails one", "until", s.diag.Until().Format(time.RFC3339))
		} else {
			cfg.Log.Info("diagnostics mode is off")
		}
		s.hub.Notify()
	}}
	rcfg := restart.Config{Dial: dial, OnChange: s.hub.Notify, Log: cfg.Log}
	if cfg.RestartConfig != nil {
		cfg.RestartConfig(&rcfg)
	}
	s.restart = restart.New(ctx, rcfg)
	var hstore *history.Store
	if p := historyFile(cfg.ConfigPath); p != "" {
		hstore = &history.Store{Path: p}
	}
	if p := eventsFile(cfg.ConfigPath); p != "" {
		s.events = &tuner.EventLog{Path: p, OnError: func(err error) { cfg.Log.Warn("couldn't save a tuner event", "err", err) }}
		s.pastEvents = s.events.Load(time.Now())
	}
	s.history = history.New(hstore, time.Now())
	s.history.OnError = func(err error) { cfg.Log.Warn("couldn't save the activity history", "err", err) }

	// A stream starting or ending changes the tuners' own status too: tell the
	// dashboards now, and have the monitors look again straight away.
	policy.SetOnChange(func() {
		s.hub.Notify()
		s.firmware.Kick() // a no-op unless the firmware check was just switched on
		for _, d := range s.deviceList() {
			d.mon.Kick()
		}
	})

	s.store = access.NewStore(cfg.ConfigPath, policy)

	// While nobody has been made an admin, a one-time link in the log lets the
	// owner become one from another device.
	s.claim = claim.New(claim.Config{Policy: policy, Store: s.store, Log: cfg.Log})

	// Signing in with Plex is a second way to be an admin, next to the address.
	s.plex = plexauth.New(plexauth.Config{
		Base: cfg.PlexURL, AuthBase: cfg.PlexAuthURL,
		Settings: policy.Settings, Edit: s.store.Edit, IsAdmin: policy.IsAdmin, Log: cfg.Log,
		SessionsFile: sessionsFile(cfg.ConfigPath),
	})
	policy.SetSessionCheck(s.plex.IsSignedInAdmin)

	start := cfg.TailscaleStart
	if start == nil {
		start = tailnet.NewNode
	}
	tsDir := "" // the node's identity is kept beside the settings file; with no file there is nowhere to keep it
	if cfg.ConfigPath != "" {
		tsDir = filepath.Join(filepath.Dir(cfg.ConfigPath), "tailscale")
	}
	s.tailnet = tailnet.NewManager(policy.Settings, tsDir, cfg.TailscaleAuthKey, cfg.Log, start)

	admin := http.NewServeMux()
	admin.Handle("/streams", policy.AdminOnly(policy.ActiveHandler()))
	admin.Handle(api.Prefix, policy.AdminOnly((&api.API{
		Tailnet: tailnetControl{s}, Logs: cfg.Logs, Fleet: s, Firmware: s.firmware, Guide: s.guide, Notify: s.notify, Upgrade: s.upgrade, Device: s.tunerinfo, Check: s.tunercheck, Diag: s.diag, Restart: s.restart, History: s.history, Lineup: &lineup.Client{}, Control: dial, Pol: policy, Store: s.store, Hub: s.hub, Log: cfg.Log,
	}).Handler()))
	// The page itself is just static files with no data in them. It has to load
	// for someone who is not yet signed in, or there would be nowhere to sign in;
	// everything it shows comes from the admin-only API above.
	admin.Handle("/", ui.Handler("/"))
	s.admin = admin

	for _, d := range cfg.Devices {
		if err := s.adminPortTaken(d); err != nil {
			return nil, err
		}
		dev, err := s.build(ctx, d)
		if err != nil {
			return nil, err
		}
		s.devices = append(s.devices, dev)
		s.log.Info("device ready", "device", d.Name, "listen", d.Listen, "tuner", dev.mon.Snapshot().BaseURL, "tunerID", strings.ToUpper(d.ID))
	}

	if len(cfg.Settings.Viewers) == 0 {
		s.log.Warn("no viewers configured: nobody can use the tuners until you add one")
	}
	return s, nil
}

// build finds one tuner and prepares everything to serve it, without listening.
func (s *Server) build(ctx context.Context, d access.Device) (*device, error) {
	base, err := s.cfg.Locate(ctx, d.Tuner, d.ID)
	if err != nil {
		return nil, fmt.Errorf("device %q: %w", d.Name, err)
	}
	log := s.cfg.Log.With("device", d.Name)
	mon := tuner.NewMonitor(tuner.Config{
		Name: d.Name, BaseURL: base, ID: d.ID, StreamPort: d.StreamPort,
		Interval: s.cfg.Poll, Find: s.cfg.Find, OnChange: func() { s.hub.Notify(); s.announceFirmware(); s.guide.Kick() },
		OnTransition: func(online bool) { s.notify.TunerState(d.Name, s.title(d.Name), online) },
		OnMoved:      func(_, to string) { s.saveTunerAddress(d.Name, to) },
		Streams:      func() []string { return s.policy.OpenChannels(d.Name) },
		Past:         s.pastEvents[d.Name],
		OnEvent:      s.keepEvent(d.Name),
		Logger:       log,
	})
	s.policy.AddDevice(d.Name, mon, mon)
	s.guide.Watch(d.Name, mon.DeviceAuth)

	name := d.Name
	mux := http.NewServeMux()
	tunerAPI := proxy.New(proxy.Config{
		Upstream: mon, Policy: s.policy.Device(name),
		Advertise: d.Advertise, DeviceID: d.ReportID, Observer: mon, Diag: s.diag, OnTrouble: s.checkOnTrouble(name, mon), Logger: log,
	})
	// The admin pages have a port of their own, so a tuner's port is only the tuner.
	mux.Handle("/", tunerAPI)

	log.Debug("device built", "listen", d.Listen, "tuner", base)
	return &device{cfg: d, mon: mon, handler: mux}, nil
}

// errAddressUnchanged stops an edit that has nothing to save.
var errAddressUnchanged = errors.New("address already saved")

// saveTunerAddress keeps the address a tuner was found at, so that the next start looks there first instead of
// at an address it left. A tuner given on the command line has no settings file to keep it in.
func (s *Server) saveTunerAddress(name, base string) {
	addr := tunerAddress(base)
	log := s.log.With("device", name)
	_, err := s.store.Edit(func(set *access.Settings) error {
		for i := range set.Devices {
			if set.Devices[i].Name == name && set.Devices[i].Tuner != addr {
				set.Devices[i].Tuner = addr
				return nil
			}
		}
		return errAddressUnchanged
	})
	switch {
	case err == nil:
		log.Info("saved the tuner's new address", "tuner", addr)
		s.devMu.Lock()
		for _, d := range s.devices {
			if d.cfg.Name == name {
				d.cfg.Tuner = addr
			}
		}
		s.devMu.Unlock()
		s.hub.Notify()
	case errors.Is(err, errAddressUnchanged), errors.Is(err, access.ErrNoFile):
		// Nothing saved is out of date, or there is no settings file to keep it in.
	default:
		log.Warn("couldn't save the tuner's new address", "tuner", addr, "err", err)
	}
}

// tunerAddress is a base URL as it is written in the settings: the host (and port), without "http://".
func tunerAddress(base string) string {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.Scheme != "http" || (u.Path != "" && u.Path != "/") {
		return base
	}
	return u.Host
}

// Policy is the access policy, for reloading settings.
func (s *Server) Policy() *access.Policy { return s.policy }

// Run serves every device until ctx ends or one of them fails.
func (s *Server) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer s.events.Close()

	s.fleetMu.Lock()
	// Bind everything first, so a port that's already taken fails the whole
	// start-up instead of leaving some devices running and others not.
	devs := s.deviceList()
	lns := make([]net.Listener, len(devs))
	closeAll := func() {
		for _, l := range lns {
			if l != nil {
				l.Close()
			}
		}
	}
	for i, d := range devs {
		ln, ok := s.cfg.Listeners[d.cfg.Name]
		if !ok {
			var err error
			if ln, err = net.Listen("tcp", d.cfg.Listen); err != nil {
				closeAll()
				s.fleetMu.Unlock()
				return fmt.Errorf("device %q can't use %s (is another program, or another Yagi, using that port?): %w\n  change its \"listen\" in the settings file", d.cfg.Name, d.cfg.Listen, err)
			}
		}
		lns[i] = ln
	}
	bareLn, ok := s.cfg.Listeners[adminName]
	if !ok {
		var err error
		if bareLn, err = net.Listen("tcp", s.cfg.AdminListen); err != nil {
			closeAll()
			s.fleetMu.Unlock()
			return fmt.Errorf("the admin pages can't use %s (is another program, or another Yagi, using that port?): %w\n  choose another with -admin-listen :PORT, YAGI_ADMIN_LISTEN, or \"adminListen\" in the settings file", s.cfg.AdminListen, err)
		}
	}

	s.runCtx = ctx
	s.errc = make(chan error, 8)
	s.logWhereToOpen(bareLn)
	s.logClaimLinks([]net.Listener{bareLn}) // the setup link is on the admin port, not a tuner's

	s.tailnet.Reconcile()
	go s.watchTailnet(ctx)
	go s.firmware.Run(ctx)
	go s.guide.Run(ctx)
	go s.notify.Run(ctx)
	go s.recordHistory(ctx)

	for i, d := range devs {
		s.start(d, lns[i])
	}
	s.startAdmin(bareLn)
	errc := s.errc
	s.fleetMu.Unlock()

	var first error
	select {
	case <-ctx.Done():
	case first = <-errc:
	}
	cancel()
	s.closeTailnetServing()
	s.tailnet.Close()

	s.fleetMu.Lock()
	servers := []*http.Server{}
	for _, d := range s.deviceList() {
		servers = append(servers, d.srv)
	}
	if s.adminLn != nil {
		servers = append(servers, s.adminLn.srv)
	}
	s.fleetMu.Unlock()
	shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	for _, srv := range servers {
		if srv != nil {
			_ = srv.Shutdown(shutdown)
		}
	}
	return first
}

// start begins serving a device on ln. The caller holds fleetMu.
func (s *Server) start(d *device, ln net.Listener) {
	dctx, cancel := context.WithCancel(s.runCtx)
	d.cancel = cancel
	go d.mon.Run(dctx)
	s.serve(d, ln)
}

// serve answers requests for a device on ln.
func (s *Server) serve(d *device, ln net.Listener) {
	ln = s.gate(ln)
	d.ln = ln
	// No overall timeouts: streams run for hours. Only the header read is
	// bounded, so a stalled client can't hold a connection open for free.
	d.srv = &http.Server{Handler: d.handler, ReadHeaderTimeout: 10 * time.Second}
	s.log.Info("listening", "device", d.cfg.Name, "addr", ln.Addr().String())
	go func(srv *http.Server, name string) {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			select {
			case s.errc <- fmt.Errorf("device %q: %w", name, err):
			default:
			}
		}
	}(d.srv, d.cfg.Name)
}

// logClaimLinks writes the first-run setup link to the log, once for each
// address the machine can be reached at, if there is still an admin to claim.
// logWhereToOpen says where the admin page is, in plain words, for whoever just started Yagi from a command line.
func (s *Server) logWhereToOpen(ln net.Listener) {
	port := portOfListen(ln.Addr().String())
	hosts := []string{}
	for _, a := range netinfo.Detect() {
		hosts = append(hosts, a.IP)
	}
	if len(hosts) == 0 {
		hosts = []string{"<this-machine>"}
	}
	for _, h := range hosts {
		s.log.Info("admin page", "open", "http://"+net.JoinHostPort(h, port)+"/")
	}
	s.log.Info("if a browser on another computer can't connect, the machine's firewall is the usual cause: allow port " + port + ". See " + version.DocsSite + "/troubleshooting/")
}

func (s *Server) logClaimLinks(lns []net.Listener) {
	tok := s.claim.Token()
	if tok == "" || len(lns) == 0 {
		return
	}
	port := "5004"
	if a, ok := lns[0].Addr().(*net.TCPAddr); ok {
		port = fmt.Sprint(a.Port)
	}
	var hosts []string
	for _, a := range netinfo.Detect() {
		hosts = append(hosts, a.IP)
	}
	if len(hosts) == 0 {
		hosts = []string{"<this-machine>"}
	}
	s.log.Info("FIRST RUN: nobody is an admin yet. To set up, open one of these links from the computer you want to use as the admin. It works once, only from a private network, and only until Yagi restarts. That computer's address is then what is trusted, so if it changes (DHCP), you are locked out. Reserve its address in your router, or, if you use Plex, turn on Sign in with Plex on the Settings page")
	for _, u := range claim.URLs(tok, port, hosts) {
		s.log.Info("  setup link", "url", u)
	}
}

// announceFirmware tells the owner, once per release, when a tuner has newer
// firmware available, and forgets it again once the tuner is up to date so the
// next release is announced too.
func (s *Server) announceFirmware() {
	type note struct{ device, title, version, text string }
	var notes []note

	s.mu.Lock()
	for _, d := range s.deviceList() {
		t := d.mon.Snapshot()
		if !t.Online {
			continue
		}
		st := s.firmware.StatusFor(firmware.Device{
			Model: t.Model, FirmwareName: t.Firmware.Name, Version: t.Firmware.Version, UpgradeAvailable: t.Firmware.UpgradeAvailable,
		})
		switch st.State {
		case firmware.StateUpdateAvailable:
			if s.announced[d.cfg.Name] != st.Latest {
				s.announced[d.cfg.Name] = st.Latest
				title := s.title(d.cfg.Name)
				text := fmt.Sprintf("Firmware %s is available for %s (it is on %s)", st.Latest, title, st.Current)
				if st.DeviceReported != "" {
					text += ", as reported by the tuner"
				}
				notes = append(notes, note{d.cfg.Name, title, st.Latest, text})
			}
		case firmware.StateUpToDate:
			delete(s.announced, d.cfg.Name)
		}
	}
	s.mu.Unlock()

	// Outside the lock: noting an event calls back into the monitors.
	for _, n := range notes {
		for _, d := range s.deviceList() {
			if d.cfg.Name == n.device {
				d.mon.Note("info", n.text)
			}
		}
		s.notify.FirmwareFound(n.device, n.title, n.version, n.text)
	}
}

// title is what a tuner is called to the owner: its label, or its name.
func (s *Server) title(name string) string {
	if l := s.policy.Label(name); l != "" {
		return l
	}
	return name
}

// Reload applies new settings. Everything about viewers and limits takes
// effect at once; changes to the devices themselves (their ports and tuner
// addresses) need a restart, and that is reported rather than half-applied.
func (s *Server) Reload(next access.Settings) error {
	if err := s.policy.Update(next); err != nil {
		return err
	}
	if len(next.Devices) > 0 && !equal(restartKeys(next.Devices), restartKeys(s.runningConfigs())) {
		s.log.Warn("the devices in the settings file changed; restart Yagi to apply that (everything else was reloaded)")
	}
	s.log.Info("settings reloaded", "viewers", len(next.Viewers))
	return nil
}

// ReloadFile re-reads the settings file and applies it. A bad file is
// reported and the running settings are kept.
func (s *Server) ReloadFile(path string) error {
	next, _, err := access.LoadSettings(path)
	if err != nil {
		return err
	}
	return s.Reload(next)
}

// restartKeys describes the parts of each device that can't change while
// running. Per-device limits are left out: those reload live.
func restartKeys(ds []access.Device) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = strings.Join([]string{d.Name, d.Listen, d.Tuner, strings.ToUpper(d.ID), d.StreamPort, d.Advertise, strings.ToUpper(d.ReportID)}, "|")
	}
	sort.Strings(out)
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// DefaultLocate uses the tuner's address if there is one, otherwise searches
// the network for its DeviceID.
func DefaultLocate(ctx context.Context, addr, id string) (string, error) {
	if addr != "" {
		u, err := ParseTuner(addr)
		if err != nil {
			return "", err
		}
		return u.String(), nil
	}
	if id == "" {
		return "", errors.New("needs a tuner address or a DeviceID to search for")
	}
	fctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	devs, err := discovery.Discover(fctx, 3*time.Second)
	if err != nil && len(devs) == 0 {
		return "", fmt.Errorf("searching for tuner %s: %w", id, err)
	}
	for _, d := range devs {
		if strings.EqualFold(d.ID, id) {
			return d.BaseURL, nil
		}
	}
	return "", fmt.Errorf("tuner %s not found on the network; give its address", strings.ToUpper(id))
}

// ParseTuner accepts "192.168.1.132", "host:80" or a full URL.
func ParseTuner(s string) (*url.URL, error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("bad tuner address %q", s)
	}
	return u, nil
}

// notifiedFile is where the firmware releases the owner was messaged about are
// kept, so a restart doesn't repeat them: beside the settings file, and empty when
// there isn't one.
func notifiedFile(configPath string) string {
	if configPath == "" {
		return ""
	}
	return strings.TrimSuffix(configPath, filepath.Ext(configPath)) + ".notified.json"
}

// sessionsFile is where Plex sign-ins are kept so they survive a restart: beside
// the settings file, which is where Yagi is already allowed to write. With
// no settings file (started with flags only) there is nowhere to keep them.
func sessionsFile(configPath string) string {
	if configPath == "" {
		return ""
	}
	return strings.TrimSuffix(configPath, filepath.Ext(configPath)) + ".sessions.json"
}

// troubleCheckEvery is the least time between the checks a failing tuner gets in diagnostics mode. A media server
// retries every few seconds, and a check that is already written down says it.
const troubleCheckEvery = 30 * time.Second

// checkOnTrouble is what runs when a tuner fails a request in diagnostics mode: it checks the tuner at that
// moment, in the background, and writes down what it found. The answer a few minutes later, when someone looks, is
// often not the answer that was true when it failed.
func (s *Server) checkOnTrouble(name string, mon *tuner.Monitor) func(string) {
	var last atomic.Int64
	return func(what string) {
		now := time.Now().UnixNano()
		if prev := last.Load(); now-prev < int64(troubleCheckEvery) || !last.CompareAndSwap(prev, now) {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			st := mon.Snapshot()
			target := tunercheck.Target{BaseURL: st.BaseURL, ID: st.ID}
			for _, sl := range st.Slots {
				target.Slots = append(target.Slots, tunercheck.Slot{InUse: sl.InUse, ViaProxy: sl.ViaProxy})
			}
			rep := s.tunercheck.Run(ctx, target)
			attrs := []any{"device", name, "because", what}
			for _, step := range rep.Steps {
				attrs = append(attrs, step.ID, string(step.Status)+": "+step.Detail)
			}
			for _, d := range rep.Debug {
				attrs = append(attrs, "tuner says "+strings.ToLower(d.Name), strings.Join(d.Lines, " | "))
			}
			s.log.Info("diagnostics: checked the tuner", attrs...)
		}()
	}
}
