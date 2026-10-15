package hmsp

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
)

const maxBSONDepth = 64

// decodeBSON reads the document of a RAW_DUMP record. It supports the types
// the server's metrics registry produces; binary values and identifiers are
// rendered as hex strings.
func decodeBSON(data []byte) (map[string]any, error) {
	doc, _, err := bsonDocument(data, 0, 0)
	return doc, err
}

func bsonDocument(data []byte, off, depth int) (map[string]any, int, error) {
	if depth > maxBSONDepth {
		return nil, 0, fmt.Errorf("bson: nesting too deep")
	}
	if off < 0 || off+5 > len(data) {
		return nil, 0, ErrTruncated
	}
	size := int(int32(binary.LittleEndian.Uint32(data[off:])))
	end := off + size
	if size < 5 || end > len(data) {
		return nil, 0, ErrTruncated
	}
	out := map[string]any{}
	pos := off + 4
	for pos < end-1 {
		kind := data[pos]
		name, next, err := bsonCString(data[:end], pos+1)
		if err != nil {
			return nil, 0, err
		}
		value, after, err := bsonValue(kind, data[:end], next, depth)
		if err != nil {
			return nil, 0, err
		}
		out[name] = value
		pos = after
	}
	return out, end, nil
}

func bsonCString(data []byte, pos int) (string, int, error) {
	for i := pos; i < len(data); i++ {
		if data[i] == 0 {
			return string(data[pos:i]), i + 1, nil
		}
	}
	return "", 0, ErrTruncated
}

func bsonValue(kind byte, data []byte, pos, depth int) (any, int, error) {
	need := func(n int) error {
		if n < 0 || pos+n > len(data) {
			return ErrTruncated
		}
		return nil
	}
	switch kind {
	case 0x01:
		if err := need(8); err != nil {
			return nil, 0, err
		}
		return finite(math.Float64frombits(binary.LittleEndian.Uint64(data[pos:]))), pos + 8, nil
	case 0x02:
		if err := need(4); err != nil {
			return nil, 0, err
		}
		n := int(int32(binary.LittleEndian.Uint32(data[pos:])))
		if n < 1 || need(4+n) != nil {
			return nil, 0, ErrTruncated
		}
		return string(data[pos+4 : pos+4+n-1]), pos + 4 + n, nil
	case 0x03:
		return bsonDocument(data, pos, depth+1)
	case 0x04:
		doc, end, err := bsonDocument(data, pos, depth+1)
		if err != nil {
			return nil, 0, err
		}
		list := make([]any, 0, len(doc))
		for i := 0; i < len(doc); i++ {
			list = append(list, doc[fmt.Sprint(i)])
		}
		return list, end, nil
	case 0x05:
		if err := need(4); err != nil {
			return nil, 0, err
		}
		n := int(int32(binary.LittleEndian.Uint32(data[pos:])))
		if n < 0 || need(5+n) != nil {
			return nil, 0, ErrTruncated
		}
		return hex.EncodeToString(data[pos+5 : pos+5+n]), pos + 5 + n, nil
	case 0x07:
		return fixedHex(data, pos, 12)
	case 0x08:
		if err := need(1); err != nil {
			return nil, 0, err
		}
		return data[pos] != 0, pos + 1, nil
	case 0x09, 0x11, 0x12:
		if err := need(8); err != nil {
			return nil, 0, err
		}
		return int64(binary.LittleEndian.Uint64(data[pos:])), pos + 8, nil
	case 0x0A, 0x7F, 0xFF:
		return nil, pos, nil
	case 0x0B:
		pattern, next, err := bsonCString(data, pos)
		if err != nil {
			return nil, 0, err
		}
		_, next, err = bsonCString(data, next)
		return pattern, next, err
	case 0x10:
		if err := need(4); err != nil {
			return nil, 0, err
		}
		return int32(binary.LittleEndian.Uint32(data[pos:])), pos + 4, nil
	case 0x13:
		return fixedHex(data, pos, 16)
	}
	return nil, 0, fmt.Errorf("bson: unsupported type 0x%02X", kind)
}

func fixedHex(data []byte, pos, n int) (any, int, error) {
	if pos+n > len(data) {
		return nil, 0, ErrTruncated
	}
	return hex.EncodeToString(data[pos : pos+n]), pos + n, nil
}
