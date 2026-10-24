package tunercheck

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/johnschult/yagi/internal/hdhr"
	"github.com/johnschult/yagi/internal/hdhr/hdhrtest"
)

func web(t *testing.T, id, lineup string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/discover.json":
			io.WriteString(w, `{"DeviceID":"`+id+`","TunerCount":2}`)
		case "/lineup.json":
			io.WriteString(w, lineup)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func checker(t *testing.T, o hdhrtest.Options) (*Checker, *hdhrtest.Server) {
	t.Helper()
	ctrl, err := hdhrtest.New("127.0.0.1:0", o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	return &Checker{Dial: func(ctx context.Context, _ string) (*hdhr.Conn, error) { return hdhr.Dial(ctx, ctrl.Addr()) }}, ctrl
}

func step(t *testing.T, r Report, id string) Step {
	t.Helper()
	for _, s := range r.Steps {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no %q step in %+v", id, r.Steps)
	return Step{}
}

var healthy = map[string]string{
	"/sys/debug":    "mem: free=100\nboot: ok",
	"/tuner0/debug": "dev: bps=19251200 resync=0 overflow=0\nts: bps=8602880 te=0 crc=0\nnet: bps=8608160 pps=737 err=0 stop=0",
}

func TestAHealthyTunerPassesEveryStep(t *testing.T) {
	c, _ := checker(t, hdhrtest.Options{Debug: healthy})
	srv := web(t, "10ABCC87", `[{"GuideNumber":"5.1"},{"GuideNumber":"2.1"}]`)
	r := c.Run(context.Background(), Target{BaseURL: srv.URL, ID: "10abcc87", Slots: []Slot{{}, {InUse: true, ViaProxy: true}}})
	for _, s := range r.Steps {
		if s.Status != OK {
			t.Errorf("%s: %s %s", s.ID, s.Status, s.Detail)
		}
	}
	if step(t, r, "channels").Detail != "2 channels." || !strings.Contains(step(t, r, "free").Detail, "1 of 2") {
		t.Errorf("steps = %+v", r.Steps)
	}
	if len(r.Debug) != 2 || r.Debug[1].Name != "Tuner 1" || len(r.Debug[1].Lines) != 3 {
		t.Errorf("debug = %+v", r.Debug)
	}
}

func TestATunerThatDoesNotAnswerSkipsTheRestAndSaysSo(t *testing.T) {
	srv := web(t, "10ABCC87", `[]`)
	url := srv.URL
	srv.Close()
	c, ctrl := checker(t, hdhrtest.Options{})
	r := c.Run(context.Background(), Target{BaseURL: url, ID: "10ABCC87"})
	if s := step(t, r, "reach"); s.Status != Problem || !strings.Contains(s.Detail, "moved to another address") {
		t.Errorf("reach = %+v", s)
	}
	for _, id := range []string{"identity", "channels", "free", "errors"} {
		if step(t, r, id).Status != Skipped {
			t.Errorf("%s ran", id)
		}
	}
	if len(ctrl.Frames()) != 0 {
		t.Error("asked the control port of a tuner that didn't answer")
	}
}

func TestADifferentTunerAtTheAddressIsCalledOut(t *testing.T) {
	c, _ := checker(t, hdhrtest.Options{})
	srv := web(t, "10C03B06", `[{"GuideNumber":"5.1"}]`)
	r := c.Run(context.Background(), Target{BaseURL: srv.URL, ID: "10ABCC87"})
	if s := step(t, r, "identity"); s.Status != Problem || !strings.Contains(s.Detail, "10C03B06") || !strings.Contains(s.Detail, "10ABCC87") {
		t.Errorf("identity = %+v", s)
	}
}

func TestAnEmptyChannelListIsAProblemInPlainWords(t *testing.T) {
	c, _ := checker(t, hdhrtest.Options{})
	srv := web(t, "10ABCC87", `[]`)
	r := c.Run(context.Background(), Target{BaseURL: srv.URL, ID: "10ABCC87"})
	if s := step(t, r, "channels"); s.Status != Problem || !strings.Contains(s.Detail, "blank guide") {
		t.Errorf("channels = %+v", s)
	}
}

func TestAllTunersInUseSaysWhoHasThem(t *testing.T) {
	c, _ := checker(t, hdhrtest.Options{})
	srv := web(t, "10ABCC87", `[{"GuideNumber":"5.1"}]`)
	r := c.Run(context.Background(), Target{BaseURL: srv.URL, ID: "10ABCC87", Slots: []Slot{{InUse: true, ViaProxy: true}, {InUse: true}}})
	if s := step(t, r, "free"); s.Status != Problem || !strings.Contains(s.Detail, "1 through Yagi, 1 straight from the tuner") {
		t.Errorf("free = %+v", s)
	}
}

func TestErrorCountersThatAreNotZeroAreReported(t *testing.T) {
	c, _ := checker(t, hdhrtest.Options{Debug: map[string]string{
		"/tuner1/debug": "ts: bps=1 te=12 crc=0\nnet: err=3 stop=0 pps=5",
	}})
	srv := web(t, "10ABCC87", `[{"GuideNumber":"5.1"}]`)
	r := c.Run(context.Background(), Target{BaseURL: srv.URL, ID: "10ABCC87"})
	s := step(t, r, "errors")
	if s.Status != Note || !strings.Contains(s.Detail, "Tuner 2: 12 transport errors") || !strings.Contains(s.Detail, "Tuner 2: 3 network errors") {
		t.Errorf("errors = %+v", s)
	}
	if strings.Contains(s.Detail, "bad checksums") {
		t.Error("a zero counter was reported")
	}
}

func TestATunerWithoutDebugCountersIsNotAFault(t *testing.T) {
	c, _ := checker(t, hdhrtest.Options{})
	srv := web(t, "10ABCC87", `[{"GuideNumber":"5.1"}]`)
	r := c.Run(context.Background(), Target{BaseURL: srv.URL, ID: "10ABCC87"})
	if s := step(t, r, "errors"); s.Status != Skipped {
		t.Errorf("errors = %+v", s)
	}
	if len(r.Debug) != 0 {
		t.Errorf("debug = %+v", r.Debug)
	}
}

func TestACheckOnlyReads(t *testing.T) {
	c, ctrl := checker(t, hdhrtest.Options{Debug: healthy})
	srv := web(t, "10ABCC87", `[{"GuideNumber":"5.1"}]`)
	c.Run(context.Background(), Target{BaseURL: srv.URL, ID: "10ABCC87"})
	if len(ctrl.Frames()) == 0 {
		t.Fatal("the control port was never asked")
	}
	for _, f := range ctrl.Frames() {
		tlvs, _ := hdhr.ParseTLVs(f.Payload)
		for _, tl := range tlvs {
			if tl.Tag == hdhr.TagGetSetValue {
				t.Fatal("a check carried a value")
			}
		}
	}
}

func TestLongAndNoisyDebugTextIsCutDown(t *testing.T) {
	lines := cleanLines(strings.Repeat("x", 500) + "\n\n" + strings.Repeat("line\n", 100))
	if len(lines) != maxLines || len(lines[0]) != maxLine {
		t.Errorf("%d lines, first %d long", len(lines), len(lines[0]))
	}
}
