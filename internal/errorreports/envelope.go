package errorreports

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Limits applied to what is kept of an event.
const (
	maxRawBytes   = 128 << 10
	maxTitleRunes = 300
	maxFrames     = 200
	groupFrames   = 5
)

var errBadEnvelope = errors.New("malformed envelope")

// Item is one entry of a Sentry envelope.
type item struct {
	Type    string
	Payload []byte
}

// parseEnvelope splits a Sentry envelope: a JSON header line followed by
// items, each a JSON header line and a payload whose size is either given
// by the header's "length" or runs to the end of the line.
// See https://develop.sentry.dev/sdk/data-model/envelopes/.
func parseEnvelope(body []byte) (header map[string]any, items []item, err error) {
	line, rest := cutLine(body)
	if err := json.Unmarshal(line, &header); err != nil || header == nil {
		return nil, nil, errBadEnvelope
	}
	for len(rest) > 0 {
		line, rest = cutLine(rest)
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var h struct {
			Type   string       `json:"type"`
			Length *json.Number `json:"length"`
		}
		if err := json.Unmarshal(line, &h); err != nil {
			return nil, nil, errBadEnvelope
		}
		var payload []byte
		if h.Length != nil {
			n, err := strconv.Atoi(h.Length.String())
			if err != nil || n < 0 || n > len(rest) {
				return nil, nil, errBadEnvelope
			}
			payload, rest = rest[:n], rest[n:]
			rest = bytes.TrimPrefix(rest, []byte("\n"))
		} else {
			payload, rest = cutLine(rest)
		}
		items = append(items, item{Type: h.Type, Payload: payload})
	}
	return header, items, nil
}

func cutLine(b []byte) (line, rest []byte) {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		return b[:i], b[i+1:]
	}
	return b, nil
}

// replaceHeader returns the envelope with a new header line, the items
// being kept byte for byte.
func replaceHeader(body []byte, header map[string]any) ([]byte, error) {
	line, err := json.Marshal(header)
	if err != nil {
		return nil, err
	}
	_, rest := cutLine(body)
	out := make([]byte, 0, len(line)+1+len(rest))
	out = append(out, line...)
	out = append(out, '\n')
	return append(out, rest...), nil
}

// Frame is a stack frame, as reported by the SDK.
type Frame struct {
	Module   string `json:"module,omitempty"`
	Function string `json:"function,omitempty"`
	Filename string `json:"filename,omitempty"`
	Line     int    `json:"line,omitempty"`
	InApp    bool   `json:"inApp,omitempty"`
}

// Exception is one exception of a chain.
type Exception struct {
	Type   string  `json:"type"`
	Value  string  `json:"value,omitempty"`
	Module string  `json:"module,omitempty"`
	Frames []Frame `json:"frames,omitempty"`
}

// Event is what is kept of an error event.
type Event struct {
	ID          string    `json:"id"`
	Time        time.Time `json:"time"`
	Level       string    `json:"level"`
	Logger      string    `json:"logger,omitempty"`
	Message     string    `json:"message,omitempty"`
	Release     string    `json:"release,omitempty"`
	Environment string    `json:"environment,omitempty"`
	ServerName  string    `json:"serverName,omitempty"`
	// Exceptions are ordered as Sentry does: the cause first, the exception
	// finally thrown last.
	Exceptions []Exception       `json:"exceptions,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
	// Raw is the payload as received, unless larger than maxRawBytes.
	Raw        json.RawMessage `json:"raw,omitempty"`
	RawDropped bool            `json:"rawDropped,omitempty"`
	culprit    string          // set by the SDK, if any
	print      []string        // custom fingerprint, if any
	plugin     string          // found from the frames, if any
	launch     time.Time       // start of the server process, if known
}

// occurrence is the entry of the history for this event.
func (e Event) occurrence() Occurrence {
	o := Occurrence{Time: e.Time, EventID: e.ID, Message: e.Message, Release: e.Release}
	if ex := e.primary(); ex != nil {
		o.Message = ex.Value
	}
	if r := []rune(o.Message); len(r) > maxTitleRunes {
		o.Message = string(r[:maxTitleRunes]) + "…"
	}
	if !e.launch.IsZero() {
		launch := e.launch
		o.Launch = &launch
	}
	return o
}

// rawEvent is the subset of the Sentry event payload that is read.
type rawEvent struct {
	EventID     string          `json:"event_id"`
	Timestamp   json.RawMessage `json:"timestamp"`
	Level       string          `json:"level"`
	Logger      string          `json:"logger"`
	Culprit     string          `json:"culprit"`
	Transaction string          `json:"transaction"`
	Release     string          `json:"release"`
	Environment string          `json:"environment"`
	ServerName  string          `json:"server_name"`
	Message     json.RawMessage `json:"message"`
	LogEntry    *struct {
		Formatted string `json:"formatted"`
		Message   string `json:"message"`
	} `json:"logentry"`
	Exception   json.RawMessage `json:"exception"`
	Tags        json.RawMessage `json:"tags"`
	Fingerprint []string        `json:"fingerprint"`
}

type rawException struct {
	Type       string `json:"type"`
	Value      string `json:"value"`
	Module     string `json:"module"`
	Stacktrace *struct {
		Frames []struct {
			Module   string `json:"module"`
			Function string `json:"function"`
			Filename string `json:"filename"`
			Lineno   int    `json:"lineno"`
			InApp    bool   `json:"in_app"`
		} `json:"frames"`
	} `json:"stacktrace"`
}

// parseEvent decodes an event payload. now is used when the event carries
// no usable timestamp.
func parseEvent(payload []byte, now time.Time) (Event, error) {
	var r rawEvent
	if err := json.Unmarshal(payload, &r); err != nil {
		return Event{}, fmt.Errorf("unreadable event: %w", err)
	}
	ev := Event{
		ID:          r.EventID,
		Time:        parseTimestamp(r.Timestamp, now),
		Level:       r.Level,
		Logger:      r.Logger,
		Release:     r.Release,
		Environment: r.Environment,
		ServerName:  r.ServerName,
		Exceptions:  parseExceptions(r.Exception),
		Tags:        parseTags(r.Tags),
		culprit:     firstNonEmpty(r.Culprit, r.Transaction),
	}
	markInApp(ev.Exceptions)
	if ev.Level == "" {
		ev.Level = "error"
	}
	if r.LogEntry != nil {
		ev.Message = firstNonEmpty(r.LogEntry.Formatted, r.LogEntry.Message)
	}
	if ev.Message == "" {
		ev.Message = messageText(r.Message)
	}
	if !slices.Contains(r.Fingerprint, "{{ default }}") {
		ev.print = r.Fingerprint
	}
	if len(payload) <= maxRawBytes {
		ev.Raw = json.RawMessage(slices.Clone(payload))
	} else {
		ev.RawDropped = true
	}
	return ev, nil
}

func parseTimestamp(raw json.RawMessage, now time.Time) time.Time {
	var seconds float64
	if json.Unmarshal(raw, &seconds) == nil && seconds > 0 {
		sec := int64(seconds)
		return time.Unix(sec, int64((seconds-float64(sec))*1e9)).UTC()
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999"} {
			if t, err := time.Parse(layout, text); err == nil {
				return t.UTC()
			}
		}
	}
	return now.UTC()
}

// messageText reads "message", either a string or a {formatted, message}
// object depending on the SDK.
func messageText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var obj struct {
		Formatted string `json:"formatted"`
		Message   string `json:"message"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return firstNonEmpty(obj.Formatted, obj.Message)
	}
	return ""
}

// parseExceptions reads "exception": {"values": [...]}, or the bare list
// some SDKs send.
func parseExceptions(raw json.RawMessage) []Exception {
	if len(raw) == 0 {
		return nil
	}
	var values []rawException
	var wrapped struct {
		Values []rawException `json:"values"`
	}
	if json.Unmarshal(raw, &wrapped) == nil && wrapped.Values != nil {
		values = wrapped.Values
	} else if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	out := make([]Exception, 0, len(values))
	for _, v := range values {
		e := Exception{Type: v.Type, Value: v.Value, Module: v.Module}
		if v.Stacktrace != nil {
			frames := v.Stacktrace.Frames
			if len(frames) > maxFrames { // keep the frames closest to the error
				frames = frames[len(frames)-maxFrames:]
			}
			for _, f := range frames {
				e.Frames = append(e.Frames, Frame{Module: f.Module, Function: f.Function, Filename: f.Filename, Line: f.Lineno, InApp: f.InApp})
			}
		}
		out = append(out, e)
	}
	return out
}

// parseTags reads "tags", an object or a list of [key, value] pairs.
func parseTags(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	tags := map[string]string{}
	var obj map[string]any
	var pairs [][]any
	switch {
	case json.Unmarshal(raw, &obj) == nil:
		for k, v := range obj {
			tags[k] = scalar(v)
		}
	case json.Unmarshal(raw, &pairs) == nil:
		for _, p := range pairs {
			if len(p) == 2 {
				tags[scalar(p[0])] = scalar(p[1])
			}
		}
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
}

func scalar(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// primary is the exception finally thrown.
func (e Event) primary() *Exception {
	if len(e.Exceptions) == 0 {
		return nil
	}
	return &e.Exceptions[len(e.Exceptions)-1]
}

// Title summarises the event in one line.
func (e Event) Title() string {
	title := e.Message
	if ex := e.primary(); ex != nil {
		title = ex.Type
		if ex.Value != "" {
			title += ": " + ex.Value
		}
	}
	title, _, _ = strings.Cut(title, "\n")
	if title == "" {
		title = "Unknown error"
	}
	if r := []rune(title); len(r) > maxTitleRunes {
		title = string(r[:maxTitleRunes]) + "…"
	}
	return title
}

// Culprit is where the error happened: given by the SDK, or the innermost
// frame of the application.
func (e Event) Culprit() string {
	if e.culprit != "" {
		return e.culprit
	}
	ex := e.primary()
	if ex == nil || len(ex.Frames) == 0 {
		return e.Logger
	}
	frame := ex.Frames[len(ex.Frames)-1]
	for i := len(ex.Frames) - 1; i >= 0; i-- {
		if ex.Frames[i].InApp {
			frame = ex.Frames[i]
			break
		}
	}
	if frame.Module == "" {
		return frame.Function
	}
	return frame.Module + "." + frame.Function
}

// pluginLoggerSuffix ends the logger name of a plugin on the Hytale server
// ("MyPlugin|P"), which is how its errors are told apart: the server adds
// no tag naming the plugin.
const pluginLoggerSuffix = "|P"

// Plugin is the plugin the event is attributed to: the one logging it, or
// else the one whose code appears in the stack trace.
func (e Event) Plugin() string {
	if name, ok := strings.CutSuffix(e.Logger, pluginLoggerSuffix); ok && name != "" {
		return name
	}
	return firstNonEmpty(e.plugin, e.Tags["plugin"])
}

// platformModules are the packages of the JDK, the game and its libraries.
var platformModules = []string{
	"java.", "javax.", "jdk.", "sun.", "com.sun.", "com.hypixel.", "io.netty.",
	"com.google.", "it.unimi.", "org.slf4j.", "io.sentry.",
}

// markInApp flags the frames outside the platform as belonging to the
// application when the SDK flagged none (the server does not configure
// it): they are the frames of plugins, highlighted and used for grouping.
func markInApp(exceptions []Exception) {
	for _, ex := range exceptions {
		if slices.ContainsFunc(ex.Frames, func(f Frame) bool { return f.InApp }) {
			return
		}
	}
	for i := range exceptions {
		for j := range exceptions[i].Frames {
			f := &exceptions[i].Frames[j]
			f.InApp = f.Module != "" && !slices.ContainsFunc(platformModules, func(p string) bool { return strings.HasPrefix(f.Module, p) })
		}
	}
}

// Fingerprint groups the occurrences of the same error: the custom
// fingerprint of the SDK if any, otherwise the exception types and their
// innermost frames (line numbers left out, so that a new build does not
// split an issue), otherwise the message.
func (e Event) Fingerprint() string {
	var parts []string
	switch {
	case len(e.print) > 0:
		parts = append(parts, "custom")
		parts = append(parts, e.print...)
	case len(e.Exceptions) > 0:
		for _, ex := range e.Exceptions {
			parts = append(parts, ex.Module+"."+ex.Type)
			frames := ex.Frames
			if inApp := slices.DeleteFunc(slices.Clone(frames), func(f Frame) bool { return !f.InApp }); len(inApp) > 0 {
				frames = inApp
			}
			for _, f := range frames[max(0, len(frames)-groupFrames):] {
				parts = append(parts, f.Module+"."+f.Function)
			}
		}
	default:
		parts = append(parts, e.Logger, e.Message)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:8])
}
