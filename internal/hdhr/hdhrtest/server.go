// Package hdhrtest is a fake HDHomeRun's control side, for tests (and the demo): it answers the discover
// and the control protocol, remembers the values it is told, collects an upgrade, and records every frame
// it is sent byte for byte, so a test can compare what a client sends with what another client sends. It
// can also misbehave on purpose.
package hdhrtest

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/johnschult/yagi/internal/hdhr"
)

// Frame is one request the server received.
type Frame struct {
	Conn    int    // which connection, counting from 1
	Type    uint16 // the frame type
	Payload []byte
	Raw     []byte // the frame as it came, CRC and all
}

// Options is how the fake tuner behaves. The zero value is a well-behaved FLEX with 20250815 firmware.
type Options struct {
	DeviceID string // 8 hex digits; "10ABCC87" if empty
	Version  string // /sys/version; "20250815" if empty
	Model    string // /sys/hwmodel; "HDFX-4K" if empty
	Tuners   int    // how many tuners; 4 if zero

	// RejectUpgrade, if set, is the error the tuner answers the end of an upgrade with.
	RejectUpgrade string
	// DropBeforeEnd closes the connection after this many upgrade chunks, before the end marker.
	DropBeforeEnd int
	// DropAfterEnd closes the connection on the end marker, with no reply, as a tuner that restarts at once.
	DropAfterEnd bool
	// NoReply never answers the end marker (and keeps the connection open).
	NoReply bool
	// RebootFor is how long the tuner is unreachable after it accepts an upgrade (it closes every connection
	// in that time). The zero value is no time at all. A very long one is a tuner that never comes back.
	RebootFor time.Duration
	// VersionAfter is what /sys/version says after the upgrade. Empty means unchanged: a tuner that took the
	// image and ignored it.
	VersionAfter string
	// ChunkDelay slows the reading of each upgrade chunk, so an upload takes long enough to watch (the
	// demo). It is not for tests that want to be quick.
	ChunkDelay time.Duration
	// RestartFor is how long the tuner is unreachable after it is told to restart (set /sys/restart self).
	// The zero value is a tuner that says yes and doesn't go anywhere. RestartNoReply closes the connection
	// instead of answering the set, as a tuner might that restarts at once.
	RestartFor     time.Duration
	RestartNoReply bool
	// Signals is what a tuned tuner reports for a channel (by the channel number it was tuned to); a channel
	// not listed gets a good signal. LockAfter is how long after tuning the lock takes to appear.
	Signals   map[string]Signal
	LockAfter time.Duration
	// Busy says which tuners are in use at the start: "locked" (somebody holds its lock), "tuned" (a channel is
	// on it) or "streaming" (it has a target).
	Busy map[int]string
	// LockShowsOwner is how a real tuner shows a lock taken over the control connection: the address that holds
	// it (this one), not the key it was taken with, which it keeps to itself and only accepts back. Empty is a
	// tuner that shows the key.
	LockShowsOwner string
	// RefuseTune answers a tune request with an error, as a tuner that wouldn't tune to that channel.
	RefuseTune bool
	// Debug is the text the tuner gives for a debug value such as "/sys/debug" or "/tuner0/debug"; one not listed
	// is unknown to it, as on older firmware.
	Debug map[string]string
	// Unknown is a value name the tuner answers with an error for, besides the ones it doesn't have.
	Unknown map[string]bool
}

// Signal is what a fake tuner reports while tuned: what it locked on to (empty is no lock) and how well.
type Signal struct {
	Lock                      string
	Strength, Quality, Symbol int
}

// Server is the fake tuner.
type Server struct {
	opts Options
	tcp  net.Listener
	udp  net.PacketConn
	wg   sync.WaitGroup

	mu       sync.Mutex
	values   map[string]string
	frames   []Frame
	conns    int
	image    []byte // the last complete upgrade image
	upgraded bool
	closed   bool
	downTill time.Time // the tuner is restarting until then
	restarts int
	tunedAt  map[int]time.Time
	keys     map[string]string // the key each lock was taken with, when the tuner shows its owner instead
	tunes    []string
}

// listen opens the TCP and UDP sockets on one port. With a port of 0 the system picks the TCP port, and the same number
// may already be taken for UDP by another test running at the same time, so that is tried again with another.
func listen(addr string) (net.Listener, net.PacketConn, error) {
	tries := 1
	if _, port, err := net.SplitHostPort(addr); err == nil && port == "0" {
		tries = 50
	}
	var err error
	for range tries {
		var tcp net.Listener
		if tcp, err = net.Listen("tcp", addr); err != nil {
			return nil, nil, err
		}
		var udp net.PacketConn
		if udp, err = net.ListenPacket("udp", tcp.Addr().String()); err == nil {
			return tcp, udp, nil
		}
		tcp.Close()
	}
	return nil, nil, err
}

// New starts a fake tuner on addr ("127.0.0.1:0" for any free port; the official tool needs 65001). It
// listens for TCP control and for the UDP discover on the same port.
func New(addr string, o Options) (*Server, error) {
	tcp, udp, err := listen(addr)
	if err != nil {
		return nil, err
	}
	if o.DeviceID == "" {
		o.DeviceID = "10ABCC87"
	}
	if o.Version == "" {
		o.Version = "20250815"
	}
	if o.Model == "" {
		o.Model = "HDFX-4K"
	}
	if o.Tuners == 0 {
		o.Tuners = 4
	}
	s := &Server{opts: o, tcp: tcp, udp: udp, values: map[string]string{
		"/sys/restart": "", "/sys/version": o.Version, "/sys/model": "hdhomerun_dvr_atsc3", "/sys/hwmodel": o.Model,
		"/sys/features": "channelmap: us-bcast us-cable us-hrc us-irc kr-bcast kr-cable\nmodulation: atsc3 8vsb qam256 qam64\nauto-modulation: auto auto6t auto6c qam\n",
	}}
	for k, v := range o.Debug {
		s.values[k] = v
	}
	s.tunedAt = map[int]time.Time{}
	s.keys = map[string]string{}
	for i := 0; i < o.Tuners; i++ {
		for _, v := range []string{"channel", "lockkey", "vchannel", "target"} {
			s.values[fmt.Sprintf("/tuner%d/%s", i, v)] = "none"
		}
		switch o.Busy[i] {
		case "locked":
			s.values[fmt.Sprintf("/tuner%d/lockkey", i)] = "1234567"
		case "tuned":
			s.values[fmt.Sprintf("/tuner%d/channel", i)] = "auto:5.1"
		case "streaming":
			s.values[fmt.Sprintf("/tuner%d/target", i)] = "udp://192.168.1.9:5000"
		}
	}
	s.wg.Add(2)
	go s.serveTCP()
	go s.serveUDP()
	return s, nil
}

// Addr is where the server is listening (TCP and UDP).
func (s *Server) Addr() string { return s.tcp.Addr().String() }

// Close stops the server.
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.tcp.Close()
	s.udp.Close()
	s.wg.Wait()
}

// Frames are the control requests received so far, in order (discover datagrams are not among them).
func (s *Server) Frames() []Frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Frame(nil), s.frames...)
}

// Forget clears the recording.
func (s *Server) Forget() {
	s.mu.Lock()
	s.frames = nil
	s.mu.Unlock()
}

// Image is the last firmware image the tuner was sent in full and told to install, or nil.
func (s *Server) Image() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.image...)
}

// Value is what the tuner holds for a name.
func (s *Server) Value(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[name]
}

// SetValue changes what the tuner holds, as if it had changed by itself (a new version after a restart).
func (s *Server) SetValue(name, value string) {
	s.mu.Lock()
	s.values[name] = value
	s.mu.Unlock()
}

func (s *Server) serveUDP() {
	defer s.wg.Done()
	buf := make([]byte, 2048)
	for {
		n, from, err := s.udp.ReadFrom(buf)
		if err != nil {
			return
		}
		typ, _, err := hdhr.Decode(buf[:n])
		if err != nil || typ != hdhr.TypeDiscoverReq {
			continue
		}
		var id uint32
		_, _ = fmt.Sscanf(s.opts.DeviceID, "%X", &id)
		p := hdhr.AppendTLV(nil, hdhr.TagDeviceType, binary.BigEndian.AppendUint32(nil, 1))
		p = hdhr.AppendTLV(p, hdhr.TagDeviceID, binary.BigEndian.AppendUint32(nil, id))
		p = hdhr.AppendTLV(p, hdhr.TagTunerCount, []byte{byte(s.opts.Tuners)})
		p = hdhr.AppendTLV(p, hdhr.TagBaseURL, []byte("http://"+s.Addr()))
		s.udp.WriteTo(hdhr.Encode(hdhr.TypeDiscoverRpy, p), from)
	}
}

func (s *Server) serveTCP() {
	defer s.wg.Done()
	for {
		c, err := s.tcp.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns++
		id := s.conns
		down := time.Now().Before(s.downTill)
		s.mu.Unlock()
		if down { // restarting: nothing answers
			c.Close()
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer c.Close()
			s.handle(c, id)
		}()
	}
}

// recorder keeps the bytes of the frame being read, so the record holds the frame as it came.
type recorder struct {
	r io.Reader
	b bytes.Buffer
}

func (r *recorder) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.b.Write(p[:n])
	return n, err
}

func (s *Server) handle(c net.Conn, id int) {
	var chunks int
	var image []byte
	for {
		rec := &recorder{r: c}
		typ, payload, err := hdhr.ReadFrame(rec)
		if err != nil {
			return
		}
		s.mu.Lock()
		s.frames = append(s.frames, Frame{Conn: id, Type: typ, Payload: append([]byte(nil), payload...), Raw: append([]byte(nil), rec.b.Bytes()...)})
		s.mu.Unlock()

		switch typ {
		case hdhr.TypeGetSetReq:
			reply := s.getSet(payload)
			if s.restartRequested(payload) {
				if !s.opts.RestartNoReply {
					c.Write(hdhr.Encode(hdhr.TypeGetSetRpy, reply))
				}
				return
			}
			c.Write(hdhr.Encode(hdhr.TypeGetSetRpy, reply))
		case hdhr.TypeUpgradeReq:
			if len(payload) < 4 {
				return
			}
			off := binary.BigEndian.Uint32(payload)
			if off != 0xFFFFFFFF {
				if int(off) != len(image) { // a gap or a repeat: the tuner would discard the lot
					return
				}
				image = append(image, payload[4:]...)
				chunks++
				if s.opts.ChunkDelay > 0 {
					time.Sleep(s.opts.ChunkDelay)
				}
				if s.opts.DropBeforeEnd > 0 && chunks >= s.opts.DropBeforeEnd {
					return
				}
				continue
			}
			switch {
			case s.opts.DropAfterEnd:
				s.finish(image)
				return
			case s.opts.NoReply:
				s.finish(image)
				time.Sleep(time.Minute)
				return
			case s.opts.RejectUpgrade != "":
				c.Write(hdhr.Encode(hdhr.TypeUpgradeRpy, hdhr.AppendTLV(nil, hdhr.TagError, append([]byte(s.opts.RejectUpgrade), 0))))
			default:
				s.finish(image)
				c.Write(hdhr.Encode(hdhr.TypeUpgradeRpy, nil))
			}
		}
	}
}

func (s *Server) finish(image []byte) {
	s.mu.Lock()
	s.image = image
	s.upgraded = true
	if s.opts.RebootFor > 0 {
		s.downTill = time.Now().Add(s.opts.RebootFor)
	}
	if s.opts.VersionAfter != "" {
		s.values["/sys/version"] = s.opts.VersionAfter
	}
	s.mu.Unlock()
}

// Upgraded says whether an image was accepted.
func (s *Server) Upgraded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.upgraded
}

func (s *Server) getSet(payload []byte) []byte {
	tlvs, err := hdhr.ParseTLVs(payload)
	if err != nil {
		return hdhr.AppendTLV(nil, hdhr.TagError, []byte("ERROR: bad request\x00"))
	}
	var name string
	var value *string
	var key string
	for _, t := range tlvs {
		switch t.Tag {
		case hdhr.TagGetSetName:
			name = string(bytes.TrimRight(t.Value, "\x00"))
		case hdhr.TagGetSetValue:
			v := string(bytes.TrimRight(t.Value, "\x00"))
			value = &v
		case hdhr.TagLockKey:
			if len(t.Value) == 4 {
				key = strconv.FormatUint(uint64(binary.BigEndian.Uint32(t.Value)), 10)
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.status(name); ok {
		return hdhr.AppendTLV(nil, hdhr.TagGetSetValue, append([]byte(v), 0))
	}
	cur, known := s.values[name]
	if !known || s.opts.Unknown[name] {
		return hdhr.AppendTLV(nil, hdhr.TagError, []byte("ERROR: invalid get/set\x00"))
	}
	if value != nil {
		if err := s.lockCheck(name, *value, key); err != "" {
			return hdhr.AppendTLV(nil, hdhr.TagError, append([]byte(err), 0))
		}
		if m := tuneRe.FindStringSubmatch(name); m != nil && m[2] == "vchannel" {
			if s.opts.RefuseTune {
				return hdhr.AppendTLV(nil, hdhr.TagError, []byte("ERROR: invalid channel\x00"))
			}
			n, _ := strconv.Atoi(m[1])
			s.tunedAt[n] = time.Now()
			s.tunes = append(s.tunes, fmt.Sprintf("tuner%d %s", n, *value))
			s.values[fmt.Sprintf("/tuner%d/channel", n)] = "auto:" + *value
		}
		if *value == "force" && strings.HasSuffix(name, "/lockkey") {
			*value = "none"
		}
		if strings.HasSuffix(name, "/lockkey") && s.opts.LockShowsOwner != "" {
			n := strings.TrimSuffix(strings.TrimPrefix(name, "/tuner"), "/lockkey")
			if *value == "none" {
				delete(s.keys, n)
			} else {
				s.keys[n] = *value
				*value = s.opts.LockShowsOwner
			}
		}
		s.values[name] = *value
		if m := tuneRe.FindStringSubmatch(name); m != nil && m[2] == "channel" && *value == "none" {
			n, _ := strconv.Atoi(m[1])
			s.values[fmt.Sprintf("/tuner%d/vchannel", n)] = "none"
			delete(s.tunedAt, n)
		}
		cur = *value
	}
	return hdhr.AppendTLV(nil, hdhr.TagGetSetValue, append([]byte(cur), 0))
}

// restartRequested says whether a get/set was "set /sys/restart self", and if so takes the tuner down for
// RestartFor and counts it.
func (s *Server) restartRequested(payload []byte) bool {
	tlvs, err := hdhr.ParseTLVs(payload)
	if err != nil {
		return false
	}
	var name, value string
	var hasValue bool
	for _, t := range tlvs {
		switch t.Tag {
		case hdhr.TagGetSetName:
			name = string(bytes.TrimRight(t.Value, "\x00"))
		case hdhr.TagGetSetValue:
			value, hasValue = string(bytes.TrimRight(t.Value, "\x00")), true
		}
	}
	if name != "/sys/restart" || !hasValue || value != "self" || s.opts.Unknown[name] {
		return false
	}
	s.mu.Lock()
	s.restarts++
	if s.opts.RestartFor > 0 {
		s.downTill = time.Now().Add(s.opts.RestartFor)
	}
	s.mu.Unlock()
	return true
}

// Restarts is how many times the tuner has been told to restart.
func (s *Server) Restarts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restarts
}

var tuneRe = regexp.MustCompile(`^/tuner([0-9]+)/([a-z]+)$`)

// lockCheck is a tuner's lock: what it says to a change to one of its values, or "" if the change is allowed.
// A locked tuner only takes changes that carry its key; the lock itself can be taken when free, given back
// with its key, or forced.
func (s *Server) lockCheck(name, value, key string) string {
	m := tuneRe.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	held := s.values[fmt.Sprintf("/tuner%s/lockkey", m[1])]
	if k, ok := s.keys[m[1]]; ok && held != "none" {
		held = k // what it was taken with, whatever it shows
	}
	if m[2] == "lockkey" {
		if value == "force" || held == "none" || key == held {
			return ""
		}
		return "ERROR: resource locked by 192.168.1.9"
	}
	if held != "none" && key != held {
		return "ERROR: resource locked by 192.168.1.9"
	}
	return ""
}

// status is a tuner's status line: nothing while it has no channel, and while tuned what the options say
// (after LockAfter, the lock).
func (s *Server) status(name string) (string, bool) {
	m := tuneRe.FindStringSubmatch(name)
	if m == nil || m[2] != "status" {
		return "", false
	}
	n, _ := strconv.Atoi(m[1])
	if s.values[fmt.Sprintf("/tuner%d/channel", n)] == "none" {
		return "ch=none lock=none ss=0 snq=0 seq=0 bps=0 pps=0", true
	}
	vch := s.values[fmt.Sprintf("/tuner%d/vchannel", n)]
	sig, ok := s.opts.Signals[vch]
	if !ok {
		sig = Signal{Lock: "atsc3", Strength: 80, Quality: 90, Symbol: 100}
	}
	if at, tuned := s.tunedAt[n]; tuned && time.Since(at) < s.opts.LockAfter {
		return fmt.Sprintf("ch=atsc3:575000000 lock=none ss=%d snq=0 seq=0 bps=0 pps=0", sig.Strength/2), true
	}
	lock := sig.Lock
	if lock == "" {
		lock = "none"
	}
	return fmt.Sprintf("ch=atsc3:575000000 lock=%s ss=%d snq=%d seq=%d bps=0 pps=0", lock, sig.Strength, sig.Quality, sig.Symbol), true
}

// Tunes is "tunerN channel" for every tune request accepted, in order.
func (s *Server) Tunes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.tunes...)
}

// Held says whether a tuner still has a channel or a lock on it.
func (s *Server) Held(tuner int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[fmt.Sprintf("/tuner%d/channel", tuner)] != "none" || s.values[fmt.Sprintf("/tuner%d/lockkey", tuner)] != "none"
}
