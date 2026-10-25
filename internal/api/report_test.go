package api

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/johnschult/yagi/internal/hdhr"
	"github.com/johnschult/yagi/internal/logbuf"
	"github.com/johnschult/yagi/internal/tunercheck"
)

func TestReportGathersTheTunerTheCheckAndTheLog(t *testing.T) {
	e, ctrl := infoEnv(t)
	e.api.Check = &tunercheck.Checker{Dial: func(ctx context.Context, host string) (*hdhr.Conn, error) { return hdhr.Dial(ctx, ctrl.Addr()) }}
	e.api.Logs = logbuf.New(50)
	lg := slog.New(logbuf.Tee(slog.NewTextHandler(discard{}, nil), e.api.Logs))
	lg.Info("diagnostics: stream", "path", "/auto/v5.1", "status", 200)
	lg.Debug("too chatty to include")

	code, v := e.call(t, "POST", "/api/report", "")
	if code != 200 {
		t.Fatalf("%d %v", code, v)
	}
	text := v.(map[string]any)["text"].(string)
	for _, want := range []string{"Yagi report", "== Settings ==", "== Tuner antenna ==", "Check now, just run:", "[ok] The tuner answers", "== Log ==", "diagnostics: stream", "path=/auto/v5.1"} {
		if !strings.Contains(text, want) {
			t.Errorf("report is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "too chatty") {
		t.Error("a debug line got into the report")
	}
	if strings.Contains(strings.ToLower(text), "deviceauth") {
		t.Error("the report mentions DeviceAuth")
	}
}

func TestReportWorksWithoutACheckerOrALog(t *testing.T) {
	e, _ := infoEnv(t)
	code, v := e.call(t, "POST", "/api/report", "")
	if code != 200 {
		t.Fatalf("%d %v", code, v)
	}
	text := v.(map[string]any)["text"].(string)
	if !strings.Contains(text, "There is no log.") || strings.Contains(text, "Check now") {
		t.Errorf("got:\n%s", text)
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
