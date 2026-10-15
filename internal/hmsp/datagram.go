// Package hmsp decodes HMSP, the one-way binary protocol the MetricsStream
// game plugin uses to push metrics and gameplay events to a collector (see
// PROTOCOL.md of the plugin). The package only reads the protocol: datagram
// framing (HMAC, fragments, DEFLATE) and the record layer.
package hmsp

import (
	"bytes"
	"compress/flate"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"time"
)

// Wire constants.
const (
	Magic        = 0x484D // "HM"
	Version      = 1
	headerSize   = 20
	fragmentSize = 8
	tagSize      = 8

	flagDeflate  = 0x01
	flagFragment = 0x02
	flagHMAC     = 0x04

	fragmentTimeout = 30 * time.Second
	// maxPending bounds the fragmented messages kept while waiting for their
	// missing parts, and maxPayload the inflated size of a message.
	maxPending = 64
	maxPayload = 16 << 20
)

// Header is the fixed part of a datagram.
type Header struct {
	Flags    uint8
	ServerID uint32
	Sequence uint32
	TimeMs   int64
}

// Message is a complete, decompressed payload ready for record parsing.
type Message struct {
	Header  Header
	Payload []byte
}

// DecoderStats counts what the decoder saw.
type DecoderStats struct {
	Datagrams uint64 `json:"datagrams"`
	Lost      uint64 `json:"lost"`     // gaps in the sequence numbers
	Rejected  uint64 `json:"rejected"` // bad magic, version, tag or content
}

type pendingMessage struct {
	header  Header
	count   int
	parts   map[int][]byte
	started time.Time
}

// Decoder turns datagrams into messages. It is safe for concurrent use.
type Decoder struct {
	key []byte

	mu      sync.Mutex
	pending map[[2]uint32]*pendingMessage
	lastSeq map[uint32]uint32
	stats   DecoderStats
	nowFunc func() time.Time
}

// NewDecoder returns a decoder. A non-empty secret makes it reject every
// datagram that does not carry a valid HMAC tag.
func NewDecoder(secret string) *Decoder {
	d := &Decoder{
		pending: map[[2]uint32]*pendingMessage{},
		lastSeq: map[uint32]uint32{},
		nowFunc: time.Now,
	}
	if secret != "" {
		d.key = []byte(secret)
	}
	return d
}

// Stats returns the counters so far.
func (d *Decoder) Stats() DecoderStats {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stats
}

// Decode processes one datagram. It returns nil (and no error) while a
// fragmented message is still incomplete.
func (d *Decoder) Decode(datagram []byte) (*Message, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	msg, err := d.decode(datagram)
	if err != nil {
		d.stats.Rejected++
	}
	return msg, err
}

func (d *Decoder) decode(datagram []byte) (*Message, error) {
	if len(datagram) < headerSize {
		return nil, fmt.Errorf("hmsp: datagram too short")
	}
	if binary.BigEndian.Uint16(datagram) != Magic || datagram[2] != Version {
		return nil, fmt.Errorf("hmsp: not an HMSP v%d datagram", Version)
	}
	h := Header{
		Flags:    datagram[3],
		ServerID: binary.BigEndian.Uint32(datagram[4:]),
		Sequence: binary.BigEndian.Uint32(datagram[8:]),
		TimeMs:   int64(binary.BigEndian.Uint64(datagram[12:])),
	}
	body, err := d.verify(datagram, h.Flags)
	if err != nil {
		return nil, err
	}
	d.stats.Datagrams++
	d.trackLoss(h)

	payload := body[headerSize:]
	if h.Flags&flagFragment != 0 {
		if len(payload) < fragmentSize {
			return nil, fmt.Errorf("hmsp: truncated fragment header")
		}
		id := binary.BigEndian.Uint32(payload)
		index := int(binary.BigEndian.Uint16(payload[4:]))
		count := int(binary.BigEndian.Uint16(payload[6:]))
		if count == 0 || index >= count {
			return nil, fmt.Errorf("hmsp: bad fragment %d/%d", index, count)
		}
		payload = d.reassemble(h, id, index, count, payload[fragmentSize:])
		if payload == nil {
			return nil, nil
		}
	}
	if h.Flags&flagDeflate != 0 {
		if payload, err = inflate(payload); err != nil {
			return nil, err
		}
	}
	return &Message{Header: h, Payload: payload}, nil
}

// verify strips the HMAC tag and checks it when a secret is configured.
func (d *Decoder) verify(datagram []byte, flags uint8) ([]byte, error) {
	tagged := flags&flagHMAC != 0
	if d.key == nil {
		if tagged {
			if len(datagram) < headerSize+tagSize {
				return nil, fmt.Errorf("hmsp: datagram too short")
			}
			return datagram[:len(datagram)-tagSize], nil
		}
		return datagram, nil
	}
	if !tagged || len(datagram) < headerSize+tagSize {
		return nil, fmt.Errorf("hmsp: missing authentication tag")
	}
	body, tag := datagram[:len(datagram)-tagSize], datagram[len(datagram)-tagSize:]
	mac := hmac.New(sha256.New, d.key)
	mac.Write(body)
	if !hmac.Equal(tag, mac.Sum(nil)[:tagSize]) {
		return nil, fmt.Errorf("hmsp: invalid authentication tag")
	}
	return body, nil
}

func (d *Decoder) trackLoss(h Header) {
	if prev, ok := d.lastSeq[h.ServerID]; ok {
		if gap := h.Sequence - prev - 1; gap < 1<<31 {
			d.stats.Lost += uint64(gap)
		}
	}
	d.lastSeq[h.ServerID] = h.Sequence
}

func (d *Decoder) reassemble(h Header, id uint32, index, count int, chunk []byte) []byte {
	now := d.nowFunc()
	for k, p := range d.pending {
		if now.Sub(p.started) > fragmentTimeout {
			delete(d.pending, k)
		}
	}
	key := [2]uint32{h.ServerID, id}
	p, ok := d.pending[key]
	if !ok || p.count != count {
		if len(d.pending) >= maxPending {
			return nil
		}
		p = &pendingMessage{header: h, count: count, parts: map[int][]byte{}, started: now}
		d.pending[key] = p
	}
	p.parts[index] = append([]byte(nil), chunk...)
	if len(p.parts) < p.count {
		return nil
	}
	delete(d.pending, key)
	var buf bytes.Buffer
	for i := 0; i < p.count; i++ {
		buf.Write(p.parts[i])
	}
	return buf.Bytes()
}

// inflate decompresses a raw DEFLATE payload (no zlib header).
func inflate(data []byte) ([]byte, error) {
	r := flate.NewReader(bytes.NewReader(data))
	defer r.Close()
	out, err := io.ReadAll(io.LimitReader(r, maxPayload+1))
	if err != nil {
		return nil, fmt.Errorf("hmsp: inflate: %w", err)
	}
	if len(out) > maxPayload {
		return nil, fmt.Errorf("hmsp: payload too large")
	}
	return out, nil
}
