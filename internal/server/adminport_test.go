package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johnschult/yagi/internal/access"
)

// startSplitFleet is startFleet with the admin pages on a port of their own (-admin-listen). base is the admin
// port; the tuners' ports are their own.
func startSplitFleet(t *testing.T, devices []access.Device, tuners ...*fakeTuner) *fleet {
	t.Helper()
	f := &fleet{logs: &lockedBuf{}}
	s := access.DefaultSettings()
	off := false
	s.FirmwareCheck, s.Guide = &off, &off
	s.Devices = devices
	port := freePort(t)
	f.path = filepath.Join(t.TempDir(), "yagi.json")
	if err := access.SaveSettings(f.path, s); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srv, err := New(ctx, Config{
		Find: noFind, Settings: s, Devices: devices, Poll: 50 * time.Millisecond, Locate: locateFrom(tuners...),
		Log: slog.New(slog.NewTextHandler(f.logs, nil)), AdminListen: "127.0.0.1:" + port, ConfigPath: f.path,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.srv = srv
	goRun(t, srv, ctx, cancel)
	f.base = "http://127.0.0.1:" + port
	f.waitUp(t)
	return f
}

func TestAdminPagesOnTheirOwnPortAreNotOnTheTunerPort(t *testing.T) {
	a := newFakeTuner(t, "10ABCC87", "antenna-box")
	tp := freePort(t)
	dev := access.Device{Name: "antenna", Listen: "127.0.0.1:" + tp, Tuner: "antenna-box", ID: "10ABCC87"}
	f := startSplitFleet(t, []access.Device{dev}, a)
	tunerBase := "http://127.0.0.1:" + tp

	if code, _ := call(t, "GET", f.base+"/api/state", nil); code != http.StatusOK {
		t.Errorf("admin port /api/state = %d, want 200", code)
	}
	for _, path := range []string{"/api/state", "/", "/auth/status", "/claim"} {
		if code, _ := call(t, "GET", tunerBase+path, nil); code == http.StatusOK {
			t.Errorf("tuner port %s = 200, want the admin pages not there", path)
		}
	}
	// The bare tuner address is the tuner's, not a redirect to the admin page.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(tunerBase + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); strings.Contains(loc, "/") {
		t.Errorf("GET / on the tuner port redirects to %q, want it left to the tuner", loc)
	}
	// And what a media server asks of a tuner is still answered there.
	if code, _ := call(t, "GET", tunerBase+"/discover.json", nil); code != http.StatusOK && code != http.StatusForbidden {
		t.Errorf("discover.json = %d on the tuner port, want the tuner answering", code)
	}
	// The admin port answers a bare address with the admin page, and has no tuner front page.
	resp, err = client.Get(f.base + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 503 { // 503: the UI isn't built in this checkout
		t.Errorf("GET / on the admin port = %d, want the admin page", resp.StatusCode)
	}
}

func TestAddingATunerWithAnAdminPortGivesItAnotherPort(t *testing.T) {
	a := newFakeTuner(t, "10ABCC87", "antenna-box")
	f := startSplitFleet(t, nil, a)
	code, body := call(t, "POST", f.base+"/api/devices", map[string]string{"tuner": "antenna-box", "listen": "127.0.0.1:" + freePort(t)})
	if code != http.StatusCreated {
		t.Fatalf("add = %d %s\n%s", code, body, f.logs.String())
	}
	var added struct{ Listen string }
	json.Unmarshal(body, &added)
	if samePort(f.base[len("http://"):], added.Listen) {
		t.Errorf("the tuner took the admin port: %q", added.Listen)
	}
	f.srv.fleetMu.Lock()
	still := f.srv.adminLn != nil
	f.srv.fleetMu.Unlock()
	if !still {
		t.Error("the admin pages let go of their port when a tuner was added")
	}
	if code, _ := call(t, "GET", f.base+"/api/state", nil); code != http.StatusOK {
		t.Errorf("admin pages = %d after adding a tuner, want 200", code)
	}
}

func TestNoTunerMayUseTheAdminPort(t *testing.T) {
	a := newFakeTuner(t, "10ABCC87", "antenna-box")
	tp := freePort(t)
	dev := access.Device{Name: "antenna", Listen: "127.0.0.1:" + tp, Tuner: "antenna-box", ID: "10ABCC87"}
	f := startSplitFleet(t, []access.Device{dev}, a)
	adminAddr := f.base[len("http://"):]

	if code, body := call(t, "PATCH", f.base+"/api/devices/antenna", map[string]string{"listen": adminAddr}); code != http.StatusConflict || !strings.Contains(string(body), "admin") {
		t.Errorf("moving a tuner onto the admin port = %d %s, want 409 saying why", code, body)
	}
	if got := f.saved(t).Devices[0].Listen; got != "127.0.0.1:"+tp {
		t.Errorf("saved listen = %q, want it unchanged", got)
	}
}

func TestAStartWithATunerOnTheAdminPortIsRefused(t *testing.T) {
	a := newFakeTuner(t, "10ABCC87", "antenna-box")
	port := freePort(t)
	dev := access.Device{Name: "antenna", Listen: "127.0.0.1:" + port, Tuner: "antenna-box", ID: "10ABCC87"}
	s := access.DefaultSettings()
	s.Devices = []access.Device{dev}
	_, err := New(context.Background(), Config{
		Find: noFind, Settings: s, Devices: []access.Device{dev}, Locate: locateFrom(a),
		Log: slog.New(slog.NewTextHandler(&lockedBuf{}, nil)), AdminListen: "127.0.0.1:" + port,
	})
	if err == nil || !strings.Contains(err.Error(), "admin") {
		t.Errorf("err = %v, want a refusal naming the admin port", err)
	}
}

func TestRemovingTheLastTunerLeavesTheAdminPagesWhereTheyAre(t *testing.T) {
	a := newFakeTuner(t, "10ABCC87", "antenna-box")
	tp := freePort(t)
	dev := access.Device{Name: "antenna", Listen: "127.0.0.1:" + tp, Tuner: "antenna-box", ID: "10ABCC87"}
	f := startSplitFleet(t, []access.Device{dev}, a)
	if code, body := call(t, "DELETE", f.base+"/api/devices/antenna", nil); code != http.StatusNoContent && code != http.StatusOK {
		t.Fatalf("remove = %d %s", code, body)
	}
	if code, _ := call(t, "GET", f.base+"/api/state", nil); code != http.StatusOK {
		t.Errorf("admin pages = %d after the last tuner went, want 200 on the same port", code)
	}
}
