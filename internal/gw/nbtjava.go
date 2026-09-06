package gw

import (
	"bytes"
	"encoding/binary"
	"math"
)

// A reader for Java's network NBT (big-endian, nameless root compound, as
// block entities travel in chunks): enough to walk any tag and hand back
// compounds as maps — strings, numbers (Go types by tag), lists, byte /
// int / long arrays.

const (
	tagEnd = iota
	tagByte
	tagShort
	tagInt
	tagLong
	tagFloat
	tagDouble
	tagByteArray
	tagString
	tagList
	tagCompound
	tagIntArray
	tagLongArray
)

// readJavaNBT reads one nameless root compound (a lone TAG_End is the
// empty entry the world writes for block entities with no data).
func readJavaNBT(r *bytes.Reader) (map[string]any, bool) {
	t, err := r.ReadByte()
	if err != nil {
		return nil, false
	}
	if t == tagEnd {
		return map[string]any{}, true
	}
	if t != tagCompound {
		return nil, false
	}
	v, ok := readJavaTag(r, tagCompound, 0)
	m, isMap := v.(map[string]any)
	return m, ok && isMap
}

func readJavaString(r *bytes.Reader) (string, bool) {
	var l [2]byte
	if _, err := r.Read(l[:]); err != nil {
		return "", false
	}
	n := int(binary.BigEndian.Uint16(l[:]))
	buf := make([]byte, n)
	if k, err := r.Read(buf); err != nil || k != n {
		return "", false
	}
	return string(buf), true
}

func readJavaTag(r *bytes.Reader, t byte, depth int) (any, bool) {
	if depth > 32 {
		return nil, false
	}
	switch t {
	case tagByte:
		b, err := r.ReadByte()
		return b, err == nil
	case tagShort:
		var b [2]byte
		_, err := r.Read(b[:])
		return int16(binary.BigEndian.Uint16(b[:])), err == nil
	case tagInt:
		var b [4]byte
		_, err := r.Read(b[:])
		return int32(binary.BigEndian.Uint32(b[:])), err == nil
	case tagLong:
		var b [8]byte
		_, err := r.Read(b[:])
		return int64(binary.BigEndian.Uint64(b[:])), err == nil
	case tagFloat:
		var b [4]byte
		_, err := r.Read(b[:])
		return math.Float32frombits(binary.BigEndian.Uint32(b[:])), err == nil
	case tagDouble:
		var b [8]byte
		_, err := r.Read(b[:])
		return math.Float64frombits(binary.BigEndian.Uint64(b[:])), err == nil
	case tagString:
		return readJavaString(r)
	case tagByteArray, tagIntArray, tagLongArray:
		var b [4]byte
		if _, err := r.Read(b[:]); err != nil {
			return nil, false
		}
		n := int(int32(binary.BigEndian.Uint32(b[:])))
		if n < 0 || n > 1<<24 {
			return nil, false
		}
		size := map[byte]int{tagByteArray: 1, tagIntArray: 4, tagLongArray: 8}[t]
		buf := make([]byte, n*size)
		if k, err := r.Read(buf); n > 0 && (err != nil || k != len(buf)) {
			return nil, false
		}
		return buf, true
	case tagList:
		et, err := r.ReadByte()
		if err != nil {
			return nil, false
		}
		var b [4]byte
		if _, err := r.Read(b[:]); err != nil {
			return nil, false
		}
		n := int(int32(binary.BigEndian.Uint32(b[:])))
		if n < 0 || n > 1<<16 {
			return nil, false
		}
		list := make([]any, 0, n)
		for i := 0; i < n; i++ {
			v, ok := readJavaTag(r, et, depth+1)
			if !ok {
				return nil, false
			}
			list = append(list, v)
		}
		return list, true
	case tagCompound:
		m := map[string]any{}
		for {
			ct, err := r.ReadByte()
			if err != nil {
				return nil, false
			}
			if ct == tagEnd {
				return m, true
			}
			name, ok := readJavaString(r)
			if !ok {
				return nil, false
			}
			v, ok := readJavaTag(r, ct, depth+1)
			if !ok {
				return nil, false
			}
			m[name] = v
		}
	}
	return nil, false
}
