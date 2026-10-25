//go:build demo

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/johnschult/yagi/internal/access"
	"github.com/johnschult/yagi/internal/discovery"
	"github.com/johnschult/yagi/internal/hdhr/hdhrtest"
	"github.com/johnschult/yagi/internal/history"
	"github.com/johnschult/yagi/internal/lineup/linetest"
	"github.com/johnschult/yagi/internal/logbuf"
	"github.com/johnschult/yagi/internal/tailnet"
	"github.com/johnschult/yagi/internal/upgrade"
)

// TestDemo runs Yagi against a made-up tuner and guide until interrupted,
// for taking the README screenshots (scripts/screenshots/retake.sh). Not part of
// the normal tests: it only builds with -tags demo.
//
// The tuner is an HDHomeRun FLEX 4K with one stream from a viewer (Alex)
// through Yagi and one or two local devices, plus three made-up viewers.
// The address of the admin pages is written to $DEMO_DIR/addr.
//
// The channels are two with plain badges, not any network's marks, unless
// $DEMO_LOGOS/channels.json lists others: a JSON array of {"number", "name",
// "logo", "title"}, where logo is a file in that folder. retake.sh makes one from
// a real tuner's guide (fetch-logos.py) so the screenshots can show real logos
// without them being kept in the repository.
// The first channel is what Alex watches; the next two are on local devices.

// demoChannel is one channel of the made-up lineup.
type demoChannel struct {
	Number string `json:"number"`
	Name   string `json:"name"`
	Logo   string `json:"logo"`  // a file in the logos folder
	Title  string `json:"title"` // what is on now
}

func demoChannels(logos string) []demoChannel {
	chs := []demoChannel{
		{"2.1", "WSB-HD", "wsb.png", "College Football"},
		{"69.1", "WUPA-HD", "wupa.png", "College Football"},
	}
	if b, err := os.ReadFile(filepath.Join(logos, "channels.json")); err == nil {
		var read []demoChannel
		if json.Unmarshal(b, &read) == nil && len(read) > 0 {
			chs = read
		}
	}
	return chs
}

// demoLineup is the channels the made-up tuner has found: the demo's own, then some others, a few disabled and
// a few favorites, so the Channels tab has something to manage. The names are made up.
func demoLineup(chs []demoChannel) []linetest.Channel {
	var out []linetest.Channel
	for _, c := range chs {
		out = append(out, linetest.Channel{Number: c.Number, Name: c.Name, HD: true})
	}
	for i, n := range []string{"Classic TV", "Movies Plus", "Weather 24", "Kids Zone", "Shop Direct", "Retro Reruns", "Sports Two", "News Channel", "Music Videos", "Home Cooking", "Mystery Hour", "Old Westerns"} {
		c := linetest.Channel{Number: fmt.Sprintf("%d.%d", 3+i*3, 1+i%3), Name: n, HD: i%3 == 0}
		switch {
		case i%5 == 1:
			c.State = "disabled"
		case i == 2 || i == 8:
			c.State = "favorite"
		}
		out = append(out, c)
	}
	return out
}

// demoGuideChannel is what the made-up guide says about one channel of the lineup that nobody is watching.
type demoGuideChannel struct {
	name, number, network string
	langs, cats           []string
}

func (d demoGuideChannel) networkName() string {
	if d.network == "" {
		return ""
	}
	return "<display-name>" + d.network + "</display-name>"
}

// demoAbout gives each of the lineup's made-up extra channels something for the guide to say about it.
func demoAbout(chs []demoChannel) []demoGuideChannel {
	profiles := []demoGuideChannel{
		{network: "ABC", langs: []string{"en"}, cats: []string{"News", "Talk", "Series"}},
		{network: "", langs: []string{"en"}, cats: []string{"Movie", "Drama", "Action"}},
		{network: "NOAA", langs: []string{"en"}, cats: []string{"Weather", "News"}},
		{network: "", langs: []string{"en"}, cats: []string{"Children", "Animated", "Comedy"}},
		{network: "SHOP LC", langs: []string{"en"}, cats: []string{"Shop", "Shopping", "Consumer"}},
		{network: "", langs: []string{"en"}, cats: []string{"Sitcom", "Comedy", "Series"}},
		{network: "ESTRELLATV", langs: []string{"es"}, cats: []string{"Sport", "Football"}},
		{network: "", langs: []string{"en", "es"}, cats: []string{"News", "Bus./financial"}},
		{network: "", langs: []string{"en"}, cats: []string{"Music", "Entertainment"}},
		{network: "", langs: []string{"en"}, cats: []string{"Cooking", "How-to", "Travel"}},
		{network: "", langs: []string{"en"}, cats: []string{"Mystery", "Crime drama", "Law"}},
		{network: "TBN", langs: []string{"en"}, cats: []string{"Religious", "Community"}},
	}
	var out []demoGuideChannel
	for i, l := range demoLineup(chs)[len(chs):] {
		p := profiles[i%len(profiles)]
		p.name, p.number = l.Name, l.Number
		out = append(out, p)
	}
	return out
}

// demoSignals is what the made-up tuner reports when a channel of the lineup is tested: strong, middling, weak, and
// one that never locks, in turn, so the results show every color.
func demoSignals(lineup []linetest.Channel) map[string]hdhrtest.Signal {
	out := map[string]hdhrtest.Signal{}
	for i, c := range lineup {
		switch i % 4 {
		case 0:
			out[c.Number] = hdhrtest.Signal{Lock: "atsc3", Strength: 92, Quality: 98, Symbol: 100}
		case 1:
			out[c.Number] = hdhrtest.Signal{Lock: "atsc3", Strength: 71, Quality: 80, Symbol: 100}
		case 2:
			out[c.Number] = hdhrtest.Signal{Lock: "atsc3", Strength: 48, Quality: 35, Symbol: 90}
		default:
			out[c.Number] = hdhrtest.Signal{Strength: 14}
		}
	}
	return out
}

// seedHistory fills the Activity page with a couple of weeks of made-up watching, written the way the
// recorder writes it, so the page has something to show before anyone has really watched.
func seedHistory(t *testing.T, path string, chs []demoChannel) {
	t.Helper()
	st := &history.Store{Path: path}
	os.Remove(path)
	titles := []string{"Evening News", "College Football", "Sesame Street", "Local Weather", "Movie of the Week", "Nightly Talk"}
	viewers := []string{"Alex", "Sam", "Jordan"}
	now := time.Now()
	n := 0
	for day := 13; day >= 0; day-- {
		base := time.Date(now.Year(), now.Month(), now.Day()-day, 0, 0, 0, 0, now.Location())
		for k := 0; k < 2+(day*7)%3; k++ {
			n++
			c := chs[n%len(chs)]
			start := base.Add(time.Duration(17+(n*3)%5)*time.Hour + time.Duration((n*17)%50)*time.Minute)
			end := start.Add(time.Duration(25+(n*37)%110) * time.Minute)
			if end.After(now.Add(-10 * time.Minute)) {
				continue
			}
			sess := history.Session{Device: "antenna", Tuner: fmt.Sprintf("tuner%d", n%4), Channel: c.Number, ChannelName: c.Name, Start: start, End: end, Titles: []string{titles[n%len(titles)]},
				Bytes:   int64(end.Sub(start).Seconds() * float64(900_000+(n*53_000)%400_000)), // about 7 to 10 Mbps
				Quality: 62 + (n*13)%38}
			if n%3 == 0 {
				sess.Kind, sess.Client = history.KindLocal, "192.168.68.115"
			} else {
				sess.Kind, sess.Viewer = history.KindViewer, viewers[n%len(viewers)]
			}
			if n%4 == 0 {
				sess.Titles = append(sess.Titles, titles[(n+2)%len(titles)])
			}
			st.Append(sess)
		}
	}
}

// demoPoster is a made-up 16:9 picture for a program (a diagonal wash of two colors chosen from its name),
// standing in for the stills SiliconDust's guide carries.
func demoPoster(name string) []byte {
	h := 0
	for _, c := range name {
		h = h*31 + int(c)
	}
	a := color.RGBA{uint8(40 + h%160), uint8(60 + (h/7)%140), uint8(120 + (h/13)%120), 255}
	b := color.RGBA{uint8(200 - (h/3)%120), uint8(120 + (h/11)%100), uint8(60 + (h/5)%100), 255}
	img := image.NewRGBA(image.Rect(0, 0, 480, 270))
	for y := 0; y < 270; y++ {
		for x := 0; x < 480; x++ {
			t := float64(x+y) / 750
			img.Set(x, y, color.RGBA{uint8(float64(a.R)*(1-t) + float64(b.R)*t), uint8(float64(a.G)*(1-t) + float64(b.G)*t), uint8(float64(a.B)*(1-t) + float64(b.B)*t), 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// wobble varies a bit rate over time, as a real broadcast does.
func wobble(rate float64, phase float64) int {
	t := float64(time.Now().UnixNano()) / 1e9
	return int(rate * (1 + 0.09*math.Sin(t/2.3+phase) + 0.05*math.Sin(t*1.7+phase*3)))
}

// demoSystemPage is the made-up tuner's System Menu page, which the Device panel reads the DRM version, MAC
// address and subnet mask from.
const demoSystemPage = `<!DOCTYPE HTML><html><head><title>HDHomeRun System Menu</title></head><body>
<div class="B C"><div class="T">System Status</div><table>
<tr><td>Hardware Model</td><td>HDFX-4K</td></tr>
<tr><td>Firmware Version</td><td>20260326</td></tr>
<tr><td>ATSC3-DRM</td><td>SD20230420</td></tr>
<tr class="L"><td colspan="2"></td></tr>
<tr><td>Device ID</td><td>10ABCC87</td></tr>
<tr><td>MAC Address</td><td>00:18:DD:00:00:01</td></tr>
<tr><td>IP Address</td><td>192.168.68.143</td></tr>
<tr><td>Subnet Mask</td><td>255.255.252.0</td></tr>
</table></div></body></html>`

// quality is the signal quality the demo reports: steady at 100 (what the screenshots show), or, with
// DEMO_SIGNAL set, drifting and dipping now and then so the trend line on a tile has something to draw.
func quality(phase float64) int {
	if os.Getenv("DEMO_SIGNAL") == "" {
		return 100
	}
	t := float64(time.Now().UnixNano()) / 1e9
	q := 90 + 7*math.Sin(t/4+phase) + 3*math.Sin(t*1.3+phase*2) - 28*math.Max(0, math.Sin(t/17+phase)-0.8)*5
	return int(math.Max(5, math.Min(100, q)))
}

// strength is the signal strength the demo reports: steady at 100, or, with DEMO_SIGNAL=sweep, sweeping
// slowly from strong to nothing and back so every signal icon shows up.
func strength(phase float64) int {
	if os.Getenv("DEMO_SIGNAL") != "sweep" {
		return 100
	}
	t := float64(time.Now().UnixNano()) / 1e9
	return int(math.Round(50 + 50*math.Sin(t/9+phase*2)))
}

func TestDemo(t *testing.T) {
	dir := os.Getenv("DEMO_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	logos := os.Getenv("DEMO_LOGOS")
	if logos == "" {
		logos = "../../scripts/screenshots/demo"
	}
	chs := demoChannels(logos)
	lt := linetest.New(demoLineup(chs), 12*time.Second) // the lineup the Channels tab manages, and a scan that takes a while
	defer lt.Close()
	var mu sync.Mutex
	remote := false // Alex is watching the first channel through Yagi

	// DEMO_UPDATE=1 is a tuner that is a release behind, with SiliconDust's side (the changelog and the download)
	// and the tuner's control port faked, so that installing firmware can be tried: it is downloaded, sent
	// slowly enough to watch, and the tuner goes away for several seconds and comes back on the new version.
	// None of it leaves this machine.
	fwVersion, fwDown := "20260326", false
	var update struct {
		on        bool
		changelog *httptest.Server
		files     *httptest.Server
		ctrl      *hdhrtest.Server
	}
	if os.Getenv("DEMO_UPDATE") != "" {
		update.on = true
		fwVersion = "20250815"
		page, err := os.ReadFile("../firmware/testdata/changelog.html")
		if err != nil {
			t.Fatal(err)
		}
		update.changelog = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(page) }))
		defer update.changelog.Close()
		image := make([]byte, 4<<20)
		for i := range image {
			image[i] = byte(i * 7)
		}
		update.files = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/hdhomerun_dvr_atsc3_firmware_20260326.bin" {
				http.NotFound(w, r)
				return
			}
			time.Sleep(3 * time.Second) // a download takes a moment
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(image)
		}))
		defer update.files.Close()
		var err2 error
		update.ctrl, err2 = hdhrtest.New("127.0.0.1:0", hdhrtest.Options{Version: "20250815", VersionAfter: "20260326", RebootFor: 9 * time.Second, RestartFor: 8 * time.Second, ChunkDelay: time.Millisecond, Signals: demoSignals(demoLineup(chs)), LockAfter: 1500 * time.Millisecond})
		if err2 != nil {
			t.Fatal(err2)
		}
		defer update.ctrl.Close()
		go func() { // the tuner's web side goes quiet while it restarts, and comes back on the new version
			for !update.ctrl.Upgraded() {
				time.Sleep(50 * time.Millisecond)
			}
			mu.Lock()
			fwDown = true
			mu.Unlock()
			time.Sleep(10 * time.Second)
			mu.Lock()
			fwVersion, fwDown = "20260326", false
			mu.Unlock()
		}()
	}

	stream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		remote = true
		mu.Unlock()
		defer func() { mu.Lock(); remote = false; mu.Unlock() }()
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(20 * time.Millisecond):
				w.Write([]byte("TS"))
				w.(http.Flusher).Flush()
			}
		}
	}))
	defer stream.Close()
	_, sport, _ := net.SplitHostPort(strings.TrimPrefix(stream.URL, "http://"))

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		down, ver := fwDown, fwVersion
		mu.Unlock()
		if down {
			http.Error(w, "restarting", http.StatusServiceUnavailable)
			return
		}
		switch r.URL.Path {
		case "/system.html":
			io.WriteString(w, demoSystemPage)
		case "/discover.json":
			fmt.Fprintf(w, `{"DeviceAuth":"DEMO-AUTH","FriendlyName":"HDHomeRun FLEX 4K","ModelNumber":"HDFX-4K","FirmwareName":"hdhomerun_dvr_atsc3","FirmwareVersion":%q,"DeviceID":"10ABCC87","TunerCount":4,"BaseURL":"http://x","LineupURL":"http://x/lineup.json"}`, ver)
		case "/lineup_status.json", "/lineup.post":
			lt.Handler().ServeHTTP(w, r)
		case "/lineup.json":
			if r.URL.Query().Get("show") == "found" { // the channels the Tuners page manages: more than are being watched
				lt.Handler().ServeHTTP(w, r)
				return
			}
			var items []string
			for _, c := range chs {
				items = append(items, fmt.Sprintf(`{"GuideNumber":%q,"GuideName":%q,"URL":"http://127.0.0.1:%s/auto/v%s"}`, c.Number, c.Name, sport, c.Number))
			}
			fmt.Fprintf(w, "[%s]", strings.Join(items, ","))
		case "/status.json":
			mu.Lock()
			rm := remote
			mu.Unlock()
			busy := func(tuner int, c demoChannel, freq int, ip string, rate float64, phase float64) string {
				return fmt.Sprintf(`{"Resource":"tuner%d","VctNumber":%q,"VctName":%q,"Frequency":%d,"SignalStrengthPercent":%d,"SignalQualityPercent":%d,"SymbolQualityPercent":100,"NetworkRate":%d,"TargetIP":%q}`, tuner, c.Number, c.Name, freq, strength(phase), quality(phase), wobble(rate, phase), ip)
			}
			slots := []string{`{"Resource":"tuner0"}`, `{"Resource":"tuner1"}`, `{"Resource":"tuner2"}`, `{"Resource":"tuner3"}`}
			if rm {
				slots[2] = busy(2, chs[0], 599000000, "127.0.0.1", 9500000, 0)
			}
			if len(chs) > 1 { // a local device on the second channel
				slots[3] = busy(3, chs[1], 527000000, "192.168.68.115", 6700000, 1.7)
			}
			if len(chs) > 2 { // and another on the third
				slots[0] = busy(0, chs[2], 539000000, "192.168.68.120", 8100000, 3.1)
			}
			fmt.Fprintf(w, "[%s]", strings.Join(slots, ","))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	now := time.Now().UTC().Truncate(time.Minute)
	stamp := func(d time.Duration) string { return now.Add(d).Format("20060102150405") + " +0000" }
	var base string
	guide := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/xmltv":
			xml := `<?xml version="1.0" encoding="utf-8"?><tv>` + "\n"
			for i, c := range chs {
				xml += fmt.Sprintf(`<channel id="c%d"><display-name>%s</display-name><display-name>%s %s</display-name><display-name>%s</display-name><display-name>%s</display-name><lcn>%s</lcn><icon src="https://img.hdhomerun.com/channels/%s"/></channel>`+"\n", i, c.Name, c.Number, c.Name, c.Number, c.Name, c.Number, c.Logo)
			}
			// The rest of the lineup is in the guide too, saying what each carries, so the info popover has something to
			// show: its network, its language (one in Spanish, one in two) and what it mostly shows.
			for i, e := range demoAbout(chs) {
				id := fmt.Sprintf("x%d", i)
				xml += fmt.Sprintf(`<channel id="%s"><display-name>%s</display-name><display-name>%s %s</display-name><display-name>%s</display-name><display-name>%s</display-name>%s<lcn>%s</lcn></channel>`+"\n", id, e.name, e.number, e.name, e.number, e.name, e.networkName(), e.number)
				for k := 0; k < 6; k++ {
					lang := e.langs[k%len(e.langs)]
					cats := ""
					for _, c := range e.cats {
						cats += "<category>" + c + "</category>"
					}
					xml += fmt.Sprintf(`<programme start="%s" stop="%s" channel="%s"><title>Program %d</title>%s<language>%s</language></programme>`+"\n", stamp(time.Duration(k)*time.Hour), stamp(time.Duration(k+1)*time.Hour), id, k+1, cats, lang)
				}
			}
			for i, c := range chs {
				title := c.Title
				if title == "" {
					title = "Evening News"
				}
				xml += `<programme start="` + stamp(-40*time.Minute) + `" stop="` + stamp(80*time.Minute) + fmt.Sprintf(`" channel="c%d"><title>%s</title><sub-title>Week 9</sub-title><desc>The second half of a big game, with the teams trading the lead as the clock runs down. Pre-game and post-game coverage follow.</desc><category>Sports</category><category>News</category><language>en</language><icon src="https://img.hdhomerun.com/titles/DEMO%d.png" width="480" height="270"/><episode-num system="onscreen">S12E09</episode-num></programme>`, i, title, i) + "\n"
			}
			xml += `</tv>`
			io.WriteString(w, strings.ReplaceAll(xml, "https://img.hdhomerun.com", base))
		case strings.HasPrefix(r.URL.Path, "/titles/"):
			w.Header().Set("Content-Type", "image/png")
			w.Write(demoPoster(path.Base(r.URL.Path)))
		case strings.HasPrefix(r.URL.Path, "/channels/"):
			b, err := os.ReadFile(filepath.Join(logos, filepath.Base(r.URL.Path)))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Write(b)
		default:
			http.NotFound(w, r)
		}
	}))
	defer guide.Close()
	base = guide.URL

	// The usual port, so the pages show the address people will really use;
	// any free one if it is taken.
	ln, err := net.Listen("tcp", "127.0.0.1:5004")
	if err != nil {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatal(err)
	}
	// The admin pages have a port of their own: the usual one if it is free, so the pages show the address people
	// will really use, and any free one if not.
	adminLn, err := net.Listen("tcp", "127.0.0.1:9244")
	if err != nil {
		adminLn, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatal(err)
	}
	s := access.DefaultSettings()
	s.OwnerReserve = 1
	s.Admin = []string{"192.0.2.0/24"}
	s.Viewers = []access.Viewer{
		{Name: "Alex", Addresses: []string{"127.0.0.1"}, Limit: 2},
		{Name: "Sam", Addresses: []string{"100.64.0.9"}, Limit: 1},
		{Name: "Jordan", Addresses: []string{"10.8.0.0/24"}, Limit: 1},
	}
	s.Labels = map[string]string{"antenna": "Living room antenna"}
	dev := access.Device{Name: "antenna", Listen: ln.Addr().String(), Tuner: "demo", ID: "10ABCC87", StreamPort: sport}
	s.Devices = []access.Device{dev}
	s.Tailscale = &access.TailscaleSettings{Enabled: true, Hostname: "yagi"} // a made-up node that is up, so the pages show it
	cfgPath := filepath.Join(dir, "yagi.json")
	if err := access.SaveSettings(cfgPath, s); err != nil {
		t.Fatal(err)
	}
	seedHistory(t, historyFile(cfgPath), chs)
	// The log the Logs page shows: what the demo logs, and a few lines of the kind a real install has.
	demoLevel := new(slog.LevelVar)
	demoLogs := logbuf.New(2000)
	demoLog := slog.New(logbuf.Tee(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: demoLevel}), demoLogs))
	demoLog.Warn("guide fetch failed", "err", "Get \"https://api.hdhomerun.com/api/xmltv\": context deadline exceeded")
	demoLog.Error("firmware update could not start", "device", "antenna", "err", "the tuner is in use")
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	cfg := Config{
		TailscaleStart: func(tailnet.Config) tailnet.Node { return demoNode{} },
		Settings:       s, Devices: s.Devices, ConfigPath: cfgPath, Poll: time.Second, Log: demoLog, LogLevel: demoLevel, Logs: demoLogs,
		Locate:    func(context.Context, string, string) (string, error) { return api.URL, nil },
		Listeners: map[string]net.Listener{"antenna": ln, adminName: adminLn}, GuideURL: guide.URL + "/api/xmltv", GuideIconBase: guide.URL,
		AdminListen: adminLn.Addr().String(),
		// The demo's tuner is made up and goes quiet on purpose: don't go looking for a real one.
		Find: func(context.Context, string, string) (discovery.Device, error) {
			return discovery.Device{}, errors.New("demo")
		},
	}
	// The Device panel reads the tuner's control port and its System Menu page: a fake of each.
	if !update.on {
		var err error
		update.ctrl, err = hdhrtest.New("127.0.0.1:0", hdhrtest.Options{Version: "20260326", RestartFor: 8 * time.Second, Signals: demoSignals(demoLineup(chs)), LockAfter: 1500 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		defer update.ctrl.Close()
		cfg.ControlPort = update.ctrl.Addr()[strings.LastIndex(update.ctrl.Addr(), ":")+1:]
	}
	// Restarting the tuner: its web side goes quiet while its control port is down, then comes back.
	go func() {
		for done := 0; ; {
			if update.ctrl.Restarts() > done {
				done = update.ctrl.Restarts()
				mu.Lock()
				fwDown = true
				mu.Unlock()
				time.Sleep(9 * time.Second)
				mu.Lock()
				fwDown = false
				mu.Unlock()
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	if update.on {
		cfg.FirmwareURL = update.changelog.URL
		cfg.FirmwareDownloadBase = update.files.URL
		cfg.ControlPort = update.ctrl.Addr()[strings.LastIndex(update.ctrl.Addr(), ":")+1:]
		cfg.UpgradeConfig = func(u *upgrade.Config) { u.RestartWait, u.PollEvery = 4*time.Second, time.Second }
	}
	srv, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	goRun(t, srv, ctx, cancel)
	// Two people who have just added Yagi in their media server and been refused: one over Yagi's own Tailscale, which
	// says who they are, and one by a bare address. They are what the Viewers page shows to be allowed.
	for _, ask := range []struct {
		from string
		id   *access.Identity
	}{
		{"100.64.0.77:41000", &access.Identity{Login: "pal@example.com", Node: "den.tail1234.ts.net"}},
		{"192.168.1.60:41000", nil},
	} {
		r := httptest.NewRequest(http.MethodGet, "/discover.json", nil)
		r.RemoteAddr = ask.from
		if ask.id != nil {
			r = r.WithContext(access.WithIdentity(r.Context(), *ask.id))
		}
		_ = srv.policy.Device("antenna").Authorize(r)
	}
	base2 := "http://" + ln.Addr().String() // the tuner's own port
	// What the pages are opened at is the admin port, which is what $DEMO_DIR/addr holds.
	os.WriteFile(filepath.Join(dir, "addr"), []byte("http://"+adminLn.Addr().String()), 0o644)
	// Alex watches the first channel through Yagi.
	go func() {
		time.Sleep(time.Second)
		resp, err := http.Get(base2 + "/auto/v" + chs[0].Number)
		if err != nil {
			return
		}
		io.Copy(io.Discard, resp.Body)
	}()
	<-ctx.Done()
}

// demoNode is a Tailscale node that is up, with a made-up name and address, so the demo can show what the pages do with one.
type demoNode struct{}

func (demoNode) WhoIs(context.Context, string) (tailnet.Who, error) {
	return tailnet.Who{Login: "alex@example.com", Node: "alex-laptop.tail-demo.ts.net"}, nil
}

func (demoNode) Status() tailnet.NodeStatus {
	return tailnet.NodeStatus{State: tailnet.Running, DNSName: "yagi.tail-demo.ts.net", Addresses: []string{"100.101.102.103"}}
}
func (demoNode) ListenTLS(string) (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") }
func (demoNode) Listen(string) (net.Listener, error)    { return net.Listen("tcp", "127.0.0.1:0") }
func (demoNode) Close() error                           { return nil }
