package hmsp

import (
	"bytes"
	"compress/flate"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"testing"
)

// wire is a minimal encoder mirroring the plugin's Wire class.
type wire struct{ b []byte }

func (w *wire) u8(v int) *wire { w.b = append(w.b, byte(v)); return w }
func (w *wire) uvar(v uint64) *wire {
	for v >= 0x80 {
		w.b = append(w.b, byte(v)|0x80)
		v >>= 7
	}
	w.b = append(w.b, byte(v))
	return w
}
func (w *wire) svar(v int64) *wire { return w.uvar(uint64(v<<1) ^ uint64(v>>63)) }
func (w *wire) str(s string) *wire { w.uvar(uint64(len(s))); w.b = append(w.b, s...); return w }
func (w *wire) f32(v float32) *wire {
	w.b = binary.BigEndian.AppendUint32(w.b, math.Float32bits(v))
	return w
}
func (w *wire) uuid(hi, lo uint64) *wire {
	w.b = binary.BigEndian.AppendUint64(w.b, hi)
	w.b = binary.BigEndian.AppendUint64(w.b, lo)
	return w
}

// record frames a record: type, length, [age], body.
func record(typ int, age *uint64, body []byte) []byte {
	inner := &wire{}
	if age != nil {
		inner.uvar(*age)
	}
	inner.b = append(inner.b, body...)
	out := &wire{}
	out.u8(typ).uvar(uint64(len(inner.b)))
	return append(out.b, inner.b...)
}

func datagram(flags byte, seq uint32, timeMs uint64, extension, payload []byte, secret string) []byte {
	var b []byte
	b = binary.BigEndian.AppendUint16(b, Magic)
	b = append(b, Version, flags)
	b = binary.BigEndian.AppendUint32(b, 42)
	b = binary.BigEndian.AppendUint32(b, seq)
	b = binary.BigEndian.AppendUint64(b, timeMs)
	b = append(b, extension...)
	b = append(b, payload...)
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(b)
		b = append(b, mac.Sum(nil)[:8]...)
	}
	return b
}

func deflate(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.BestCompression)
	w.Write(data)
	w.Close()
	return buf.Bytes()
}

func chatRecord() []byte {
	age := uint64(250)
	body := (&wire{}).uuid(1, 2).str("Alice").str("héllo").u8(0).uvar(3)
	return record(0x37, &age, body.b)
}

func TestDecodeEventAndSnapshot(t *testing.T) {
	serverBody := (&wire{}).uvar(1000).uvar(2).svar(-1).uvar(1).uvar(5).u8(1).u8(1)
	payload := append(record(0x02, nil, serverBody.b), chatRecord()...)

	d := NewDecoder("")
	msg, err := d.Decode(datagram(0, 1, 10_000, nil, payload, ""))
	if err != nil || msg == nil {
		t.Fatalf("decode: %v", err)
	}
	recs, err := DecodeRecords(msg)
	if err != nil || len(recs) != 2 {
		t.Fatalf("records: %v %v", recs, err)
	}
	if recs[0].Type != "SERVER" || recs[0].Data["state"] != "RUNNING" || recs[0].Data["max_players"] != int64(-1) {
		t.Errorf("server record: %+v", recs[0])
	}
	chat := recs[1]
	if chat.Type != "PLAYER_CHAT" || chat.TimeMs != 10_000-250 || chat.Data["content"] != "héllo" ||
		chat.Data["uuid"] != "00000000-0000-0001-0000-000000000002" || chat.Data["recipients"] != uint64(3) {
		t.Errorf("chat record: %+v", chat)
	}
}

func TestUnknownRecordIsSkipped(t *testing.T) {
	payload := append(record(0x7E, nil, []byte{1, 2, 3}), chatRecord()...)
	msg, err := NewDecoder("").Decode(datagram(0, 1, 5, nil, payload, ""))
	if err != nil {
		t.Fatal(err)
	}
	recs, err := DecodeRecords(msg)
	if err != nil || len(recs) != 2 || recs[0].Kind != KindUnknown || recs[1].Type != "PLAYER_CHAT" {
		t.Fatalf("records: %+v %v", recs, err)
	}
}

func TestHMAC(t *testing.T) {
	payload := chatRecord()
	d := NewDecoder("secret")
	if msg, err := d.Decode(datagram(flagHMAC, 1, 1, nil, payload, "secret")); err != nil || msg == nil {
		t.Fatalf("valid tag rejected: %v", err)
	}
	if _, err := d.Decode(datagram(flagHMAC, 2, 1, nil, payload, "other")); err == nil {
		t.Error("forged tag accepted")
	}
	if _, err := d.Decode(datagram(0, 3, 1, nil, payload, "")); err == nil {
		t.Error("untagged datagram accepted although a secret is set")
	}
	if got := d.Stats().Rejected; got != 2 {
		t.Errorf("rejected = %d, want 2", got)
	}
	// Without a secret, a tagged datagram is accepted and its tag stripped.
	msg, err := NewDecoder("").Decode(datagram(flagHMAC, 1, 1, nil, payload, "x"))
	if err != nil || !bytes.Equal(msg.Payload, payload) {
		t.Errorf("tag not stripped: %v", err)
	}
}

func TestDeflateAndFragments(t *testing.T) {
	var payload []byte
	for i := 0; i < 20; i++ {
		payload = append(payload, chatRecord()...)
	}
	compressed := deflate(t, payload)
	half := len(compressed) / 2
	ext := func(index, count int) []byte {
		b := binary.BigEndian.AppendUint32(nil, 7)
		b = binary.BigEndian.AppendUint16(b, uint16(index))
		return binary.BigEndian.AppendUint16(b, uint16(count))
	}
	flags := byte(flagDeflate | flagFragment)

	d := NewDecoder("")
	// Parts may arrive out of order.
	if msg, err := d.Decode(datagram(flags, 11, 1, ext(1, 2), compressed[half:], "")); msg != nil || err != nil {
		t.Fatalf("first part: %v %v", msg, err)
	}
	msg, err := d.Decode(datagram(flags, 10, 1, ext(0, 2), compressed[:half], ""))
	if err != nil || msg == nil {
		t.Fatalf("second part: %v", err)
	}
	recs, err := DecodeRecords(msg)
	if err != nil || len(recs) != 20 {
		t.Fatalf("got %d records, err %v", len(recs), err)
	}
	// Sequence 11 then 10: the second is a duplicate/out of order, not a loss.
	if d.Stats().Lost != 0 {
		t.Errorf("lost = %d", d.Stats().Lost)
	}
}

func TestLossDetection(t *testing.T) {
	d := NewDecoder("")
	for _, seq := range []uint32{1, 2, 6} {
		if _, err := d.Decode(datagram(0, seq, 1, nil, chatRecord(), "")); err != nil {
			t.Fatal(err)
		}
	}
	if got := d.Stats().Lost; got != 3 {
		t.Errorf("lost = %d, want 3", got)
	}
}

func TestMalformedInput(t *testing.T) {
	d := NewDecoder("")
	for _, in := range [][]byte{nil, {1, 2, 3}, bytes.Repeat([]byte{0}, 30)} {
		if _, err := d.Decode(in); err == nil {
			t.Errorf("accepted %v", in)
		}
	}
	// A record claiming more bytes than available.
	msg := &Message{Payload: []byte{0x02, 0x50, 1}}
	if _, err := DecodeRecords(msg); err == nil {
		t.Error("truncated record accepted")
	}
	// A truncated body is dropped without panicking.
	msg = &Message{Payload: record(0x02, nil, []byte{1})}
	if recs, err := DecodeRecords(msg); err == nil || len(recs) != 0 {
		t.Errorf("recs=%v err=%v", recs, err)
	}
}

func TestVarints(t *testing.T) {
	w := &wire{}
	w.uvar(300).svar(-3).svar(1 << 40)
	r := newReader(w.b)
	if r.uvar() != 300 || r.svar() != -3 || r.svar() != 1<<40 || r.err() != nil {
		t.Errorf("varint round trip failed: %v", r.err())
	}
}

func TestBSON(t *testing.T) {
	// {"a": 1 (int32), "b": "hi", "c": [true], "d": {"e": 2.5}}
	doc := []byte{}
	cstr := func(s string) []byte { return append([]byte(s), 0) }
	le32 := func(v int) []byte { return binary.LittleEndian.AppendUint32(nil, uint32(v)) }
	elem := func(kind byte, name string, val []byte) []byte {
		return append(append([]byte{kind}, cstr(name)...), val...)
	}
	wrap := func(body []byte) []byte {
		return append(append(le32(len(body)+5), body...), 0)
	}
	str := append(append(le32(3), []byte("hi")...), 0)
	arr := wrap(elem(0x08, "0", []byte{1}))
	f := binary.LittleEndian.AppendUint64(nil, math.Float64bits(2.5))
	sub := wrap(elem(0x01, "e", f))
	doc = append(doc, elem(0x10, "a", le32(1))...)
	doc = append(doc, elem(0x02, "b", str)...)
	doc = append(doc, elem(0x04, "c", arr)...)
	doc = append(doc, elem(0x03, "d", sub)...)

	got, err := decodeBSON(wrap(doc))
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != int32(1) || got["b"] != "hi" || got["c"].([]any)[0] != true || got["d"].(map[string]any)["e"] != 2.5 {
		t.Errorf("decoded %v", got)
	}
	if _, err := decodeBSON(wrap(doc)[:10]); err == nil {
		t.Error("truncated document accepted")
	}
}
