package embed

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

func TestHalfPrecision(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   float32
		want float32
	}{
		{0, 0},
		{1, 1},
		{-2, -2},
		{0.5, 0.5},
		{65504, 65504},
		{1e6, float32(math.Inf(1))},
		{0.1, 0.0999755859375},
		{1.0 / (1 << 24), 1.0 / (1 << 24)},
		{1e-9, 0},
		{1 + 1.0/2048, 1},
		{1 + 3.0/2048, 1 + 2.0/1024}, // ties to even
	}
	for _, tt := range tests {
		if got := halfToFloat(floatToHalf(tt.in)); got != tt.want {
			t.Errorf("half(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
	// Every half-precision number survives the round trip.
	for h := range uint16(0x7c00) {
		if got := floatToHalf(halfToFloat(h)); got != h {
			t.Fatalf("round trip of %#04x = %#04x", h, got)
		}
	}
}

func TestVectorsFile(t *testing.T) {
	t.Parallel()
	v := NewVectors("qwen3-embedding:0.6b")
	v.Add("goroutines", FakeVector("goroutines"))
	v.Add("channels", FakeVector("channels"))
	var buf bytes.Buffer
	if _, err := v.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	if want := len(vectorsMagic) + 2 + len(v.Model) + 8 + 2*(32+2*Dims); buf.Len() != want {
		t.Errorf("file is %d bytes, want %d", buf.Len(), want)
	}

	got, err := ReadVectors(&buf)
	if err != nil {
		t.Fatal(err)
	}
	r := NewReplay(got)
	vecs, err := r.Embed(t.Context(), []string{"channels"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Model() != "qwen3-embedding:0.6b" || got.Len() != 2 {
		t.Errorf("model %q, %d vectors", r.Model(), got.Len())
	}
	want := FakeVector("channels")
	for i := range want {
		if math.Abs(float64(vecs[0][i]-want[i])) > 1e-3 {
			t.Fatalf("channels[%d] = %v, want %v", i, vecs[0][i], want[i])
		}
	}
	if _, err := r.Embed(t.Context(), []string{"never recorded"}); !errors.Is(err, ErrRejected) {
		t.Errorf("an unrecorded text = %v, want ErrRejected", err)
	}
	if _, err := ReadVectors(bytes.NewReader([]byte("nope"))); err == nil {
		t.Error("read a file that isn't one")
	}
}
