package codec

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

var (
	ErrUnexpectedEOF = errors.New("unexpected end of canonical payload")
	ErrTrailingData  = errors.New("trailing data in canonical payload")
	ErrLengthLimit   = errors.New("canonical field exceeds configured length limit")
)

type Writer struct {
	buf bytes.Buffer
}

func (w *Writer) U8(v uint8) { _ = w.buf.WriteByte(v) }

func (w *Writer) U16(v uint16) {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	_, _ = w.buf.Write(b[:])
}

func (w *Writer) U32(v uint32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	_, _ = w.buf.Write(b[:])
}

func (w *Writer) U64(v uint64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	_, _ = w.buf.Write(b[:])
}

func (w *Writer) Bool(v bool) {
	if v {
		w.U8(1)
		return
	}
	w.U8(0)
}

func (w *Writer) Fixed(v []byte) {
	_, _ = w.buf.Write(v)
}

func (w *Writer) Bytes(v []byte) {
	w.U32(uint32(len(v)))
	w.Fixed(v)
}

func (w *Writer) String(v string) { w.Bytes([]byte(v)) }

func (w *Writer) BytesCopy() []byte {
	out := make([]byte, w.buf.Len())
	copy(out, w.buf.Bytes())
	return out
}

type Reader struct {
	data []byte
	off  int
}

func NewReader(data []byte) *Reader {
	return &Reader{data: data}
}

func (r *Reader) take(n int) ([]byte, error) {
	if n < 0 || r.off+n > len(r.data) {
		return nil, ErrUnexpectedEOF
	}
	out := r.data[r.off : r.off+n]
	r.off += n
	return out, nil
}

func (r *Reader) U8() (uint8, error) {
	b, err := r.take(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func (r *Reader) U16() (uint16, error) {
	b, err := r.take(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b), nil
}

func (r *Reader) U32() (uint32, error) {
	b, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b), nil
}

func (r *Reader) U64() (uint64, error) {
	b, err := r.take(8)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(b), nil
}

func (r *Reader) Bool() (bool, error) {
	v, err := r.U8()
	if err != nil {
		return false, err
	}
	switch v {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("invalid canonical bool %d", v)
	}
}

func (r *Reader) Fixed(n int) ([]byte, error) { return r.take(n) }

func (r *Reader) Bytes(max uint32) ([]byte, error) {
	n, err := r.U32()
	if err != nil {
		return nil, err
	}
	if n > max {
		return nil, ErrLengthLimit
	}
	b, err := r.take(int(n))
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out, nil
}

func (r *Reader) String(max uint32) (string, error) {
	b, err := r.Bytes(max)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (r *Reader) Done() error {
	if r.off != len(r.data) {
		return ErrTrailingData
	}
	return nil
}

// DomainHash preserves the exact canonical framing used by Writer.String +
// Writer.Bytes, but streams it directly into SHA-256 instead of allocating a
// temporary buffer and then copying that buffer before hashing. Consensus bytes
// and hash outputs therefore remain bit-for-bit compatible while hot Merkle
// paths avoid two transient allocations per hash.
func DomainHash(domain string, payload []byte) [32]byte {
	h := sha256.New()
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(domain)))
	_, _ = h.Write(length[:])
	_, _ = h.Write([]byte(domain))
	binary.BigEndian.PutUint32(length[:], uint32(len(payload)))
	_, _ = h.Write(length[:])
	_, _ = h.Write(payload)
	var out [32]byte
	sum := h.Sum(out[:0])
	copy(out[:], sum)
	return out
}
