package codec

import (
	"bytes"
	"testing"
)

func TestCanonicalRoundTrip(t *testing.T) {
	var w Writer
	w.U8(7)
	w.U16(0x1234)
	w.U32(42)
	w.U64(99)
	w.Bool(true)
	w.String("zephyr")
	w.Bytes([]byte{1, 2, 3})

	r := NewReader(w.BytesCopy())
	if v, _ := r.U8(); v != 7 {
		t.Fatalf("u8=%d", v)
	}
	if v, _ := r.U16(); v != 0x1234 {
		t.Fatalf("u16=%d", v)
	}
	if v, _ := r.U32(); v != 42 {
		t.Fatalf("u32=%d", v)
	}
	if v, _ := r.U64(); v != 99 {
		t.Fatalf("u64=%d", v)
	}
	if v, _ := r.Bool(); !v {
		t.Fatal("bool=false")
	}
	if v, _ := r.String(64); v != "zephyr" {
		t.Fatalf("string=%q", v)
	}
	if v, _ := r.Bytes(64); !bytes.Equal(v, []byte{1, 2, 3}) {
		t.Fatalf("bytes=%v", v)
	}
	if err := r.Done(); err != nil {
		t.Fatal(err)
	}
}

func TestDomainHashSeparatesDomains(t *testing.T) {
	a := DomainHash("a", []byte("same"))
	b := DomainHash("b", []byte("same"))
	if a == b {
		t.Fatal("domain-separated hashes collided")
	}
}
