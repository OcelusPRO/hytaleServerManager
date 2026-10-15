package hmsp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// ErrTruncated is reported when a record ends before its fields do.
var ErrTruncated = errors.New("hmsp: truncated data")

// reader decodes the HMSP primitive types (big-endian, LEB128 varints). The
// first failure is sticky: later reads return zero values and err() reports it,
// so record decoders can read straight through and check once at the end.
type reader struct {
	data []byte
	pos  int
	bad  error
}

func newReader(data []byte) *reader { return &reader{data: data} }

func (r *reader) err() error { return r.bad }

func (r *reader) remaining() int { return len(r.data) - r.pos }

func (r *reader) take(n int) []byte {
	if r.bad != nil {
		return nil
	}
	if n < 0 || n > r.remaining() {
		r.bad = ErrTruncated
		return nil
	}
	out := r.data[r.pos : r.pos+n]
	r.pos += n
	return out
}

func (r *reader) u8() uint8 {
	if b := r.take(1); b != nil {
		return b[0]
	}
	return 0
}

func (r *reader) bool() bool { return r.u8() != 0 }

func (r *reader) u32() uint32 {
	if b := r.take(4); b != nil {
		return binary.BigEndian.Uint32(b)
	}
	return 0
}

func (r *reader) u64() uint64 {
	if b := r.take(8); b != nil {
		return binary.BigEndian.Uint64(b)
	}
	return 0
}

func (r *reader) uvar() uint64 {
	var v uint64
	for shift := uint(0); shift < 70; shift += 7 {
		b := r.u8()
		if r.bad != nil {
			return 0
		}
		v |= uint64(b&0x7F) << shift
		if b&0x80 == 0 {
			return v
		}
	}
	r.bad = fmt.Errorf("hmsp: varint too long")
	return 0
}

func (r *reader) svar() int64 {
	z := r.uvar()
	return int64(z>>1) ^ -int64(z&1)
}

// finite keeps NaN and infinities out of the JSON the interface receives.
func finite(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return f
}

func (r *reader) f32() float64 { return finite(float64(math.Float32frombits(r.u32()))) }

func (r *reader) f64() float64 { return finite(math.Float64frombits(r.u64())) }

func (r *reader) str() string {
	n := r.uvar()
	if n > uint64(r.remaining()) {
		r.bad = ErrTruncated
		return ""
	}
	return string(r.take(int(n)))
}

// uuid returns the canonical text form, or "" for the nil UUID (absent).
func (r *reader) uuid() string {
	b := r.take(16)
	if b == nil {
		return ""
	}
	var zero [16]byte
	if string(b) == string(zero[:]) {
		return ""
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// count reads a list length, refusing one the remaining bytes cannot hold.
func (r *reader) count() int {
	n := r.uvar()
	if n > uint64(r.remaining()) {
		r.bad = ErrTruncated
		return 0
	}
	return int(n)
}
