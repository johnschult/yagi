package server

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"log/slog"

	"github.com/johnschult/yagi/internal/access"
	"github.com/johnschult/yagi/internal/claim"
)

// startFirstRun runs a server from the given settings, saved to a settings file
// when withFile is set, and returns it with its log and address.
func startFirstRun(t *testing.T, s access.Settings, withFile bool) (srv *Server, logs *lockedBuf, admin, tuner string) {
	t.Helper()
	a := newFakeTuner(t, "10ABCC87", "antenna-box")
	logs = &lockedBuf{}
	off := false
	s.FirmwareCheck, s.Guide = &off, &off
	cfg := Config{
		Settings: s, Devices: oneDevice(a), Poll: 50 * time.Millisecond, Locate: locateFrom(a),
		Log: slog.New(slog.NewTextHandler(logs, nil)),
	}
	ln := ephemeral(t)
	adminLn := ephemeral(t)
	cfg.Listeners = map[string]net.Listener{"antenna": ln, adminName: adminLn}
	if withFile {
		cfg.ConfigPath = filepath.Join(t.TempDir(), "yagi.json")
		if err := access.SaveSettings(cfg.ConfigPath, s); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srv, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	goRun(t, srv, ctx, cancel)
	// Run logs the setup links before it starts serving.
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(logs.String(), "listening") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	return srv, logs, adminLn.Addr().String(), ln.Addr().String()
}

func status(t *testing.T, method, u string, body url.Values) int {
	t.Helper()
	req, err := http.NewRequest(method, u, strings.NewReader(body.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// A new install serves nobody, says how to set it up, and the link carries the
// token that the claim page is waiting for.
func TestFirstRunIsClosedAndLogsASetupLink(t *testing.T) {
	srv, logs, addr, tunerAddr := startFirstRun(t, access.DefaultSettings(), true)

	out := logs.String()
	if !strings.Contains(out, "FIRST RUN") {
		t.Fatalf("no first-run notice in the log:\n%s", out)
	}
	m := regexp.MustCompile(`url=(\S+)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no setup link in the log:\n%s", out)
	}
	tok := srv.claim.Token()
	if tok == "" || !strings.Contains(m[1], claim.Path+"?token="+tok) {
		t.Errorf("link %q doesn't carry the token", m[1])
	}

	// Nobody can use the tuner yet, not even this machine.
	if code := status(t, "GET", "http://"+tunerAddr+"/discover.json", nil); code != http.StatusForbidden {
		t.Errorf("/discover.json = %d, want 403 until a viewer is added", code)
	}
	// The claim page is reachable here but this machine is already an admin, so
	// it says so; a wrong token is a 404.
	if code := status(t, "GET", "http://"+addr+claim.Link(tok), nil); code != http.StatusBadRequest {
		t.Errorf("claim page from loopback = %d, want 400", code)
	}
	if code := status(t, "GET", "http://"+addr+claim.Link("wrong"), nil); code != http.StatusNotFound {
		t.Errorf("claim page with a wrong token = %d, want 404", code)
	}
}

func TestNoSetupLinkOnceThereIsAnAdmin(t *testing.T) {
	s := access.DefaultSettings()
	s.Admin = []string{"100.64.0.1"}
	srv, logs, _, _ := startFirstRun(t, s, true)
	if srv.claim.Token() != "" || strings.Contains(logs.String(), "FIRST RUN") {
		t.Errorf("a claimed install offered a setup link:\n%s", logs.String())
	}
}

func TestNoSetupLinkWhenSettingsCantBeSaved(t *testing.T) {
	srv, logs, _, _ := startFirstRun(t, access.DefaultSettings(), false)
	if srv.claim.Token() != "" || strings.Contains(logs.String(), "FIRST RUN") {
		t.Errorf("an install with no settings file offered a setup link:\n%s", logs.String())
	}
}
