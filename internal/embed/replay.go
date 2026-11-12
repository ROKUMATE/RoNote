package embed

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
)

// vectorsMagic starts a recorded vectors file.
const vectorsMagic = "RNVEC1\n"

// Vectors are embeddings recorded from one model, by the SHA-256 of their
// text, for the eval to replay without the model
// (docs/adr/0011-retrieval.md).
type Vectors struct {
	Model  string
	byHash map[[32]byte][]float32
}

// NewVectors returns an empty set for model.
func NewVectors(model string) *Vectors {
	return &Vectors{Model: model, byHash: map[[32]byte][]float32{}}
}

// Add records vec as text's embedding, rounded to half precision as the
// file keeps it.
func (v *Vectors) Add(text string, vec []float32) {
	rounded := make([]float32, len(vec))
	for i, x := range vec {
		rounded[i] = halfToFloat(floatToHalf(x))
	}
	v.byHash[sha256.Sum256([]byte(text))] = rounded
}

// Len counts the recorded vectors.
func (v *Vectors) Len() int { return len(v.byHash) }

// WriteTo writes the set: the magic, the model, then each vector as its
// text's hash and Dims half-precision numbers, little endian, in hash order.
func (v *Vectors) WriteTo(w io.Writer) (int64, error) {
	bw := bufio.NewWriter(w)
	cw := &countingWriter{w: bw}
	hashes := make([][32]byte, 0, len(v.byHash))
	for h := range v.byHash {
		hashes = append(hashes, h)
	}
	slices.SortFunc(hashes, func(a, b [32]byte) int { return slices.Compare(a[:], b[:]) })
	_, _ = cw.Write([]byte(vectorsMagic))
	_ = binary.Write(cw, binary.LittleEndian, uint16(len(v.Model))) //nolint:gosec // a model name is short
	_, _ = cw.Write([]byte(v.Model))
	_ = binary.Write(cw, binary.LittleEndian, uint32(Dims))
	_ = binary.Write(cw, binary.LittleEndian, uint32(len(hashes))) //nolint:gosec // a corpus is far smaller
	buf := make([]byte, 2*Dims)
	for _, h := range hashes {
		vec := v.byHash[h]
		if len(vec) != Dims {
			return cw.n, fmt.Errorf("vectors: a vector of %d dimensions, want %d", len(vec), Dims)
		}
		for i, x := range vec {
			binary.LittleEndian.PutUint16(buf[2*i:], floatToHalf(x))
		}
		_, _ = cw.Write(h[:])
		_, _ = cw.Write(buf)
	}
	if cw.err != nil {
		return cw.n, cw.err
	}
	return cw.n, bw.Flush()
}

// ReadVectors reads a set WriteTo wrote.
func ReadVectors(r io.Reader) (*Vectors, error) {
	br := bufio.NewReader(r)
	magic := make([]byte, len(vectorsMagic))
	if _, err := io.ReadFull(br, magic); err != nil || string(magic) != vectorsMagic {
		return nil, errors.New("vectors: not a recorded vectors file")
	}
	var nameLen uint16
	if err := binary.Read(br, binary.LittleEndian, &nameLen); err != nil {
		return nil, fmt.Errorf("vectors: %w", err)
	}
	name := make([]byte, nameLen)
	if _, err := io.ReadFull(br, name); err != nil {
		return nil, fmt.Errorf("vectors: %w", err)
	}
	var dims, count uint32
	if err := binary.Read(br, binary.LittleEndian, &dims); err != nil {
		return nil, fmt.Errorf("vectors: %w", err)
	}
	if dims != Dims {
		return nil, fmt.Errorf("vectors: %d dimensions, want %d", dims, Dims)
	}
	if err := binary.Read(br, binary.LittleEndian, &count); err != nil {
		return nil, fmt.Errorf("vectors: %w", err)
	}
	v := NewVectors(string(name))
	entry := make([]byte, 32+2*Dims)
	for range count {
		if _, err := io.ReadFull(br, entry); err != nil {
			return nil, fmt.Errorf("vectors: %w", err)
		}
		vec := make([]float32, Dims)
		for i := range vec {
			vec[i] = halfToFloat(binary.LittleEndian.Uint16(entry[32+2*i:]))
		}
		v.byHash[[32]byte(entry[:32])] = vec
	}
	return v, nil
}

type countingWriter struct {
	w   io.Writer
	n   int64
	err error
}

func (c *countingWriter) Write(p []byte) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	n, err := c.w.Write(p)
	c.n += int64(n)
	c.err = err
	return n, err
}

// Replay embeds from recorded vectors. A text with none recorded is an
// error, so the eval can't silently search with a vector it never had.
type Replay struct {
	vectors *Vectors
}

// NewReplay returns a Replay of v.
func NewReplay(v *Vectors) Replay { return Replay{vectors: v} }

// Model names the model the vectors were recorded from.
func (r Replay) Model() string { return r.vectors.Model }

// Embed returns each text's recorded vector.
func (r Replay) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		vec, ok := r.vectors.byHash[sha256.Sum256([]byte(t))]
		if !ok {
			return nil, fmt.Errorf("%w: no recorded vector for %.60q; run make eval-record", ErrRejected, t)
		}
		out[i] = vec
	}
	return out, nil
}

// floatToHalf rounds f to the nearest IEEE 754 half-precision number, ties
// to even.
func floatToHalf(f float32) uint16 {
	bits := math.Float32bits(f)
	sign := uint16(bits>>16) & 0x8000
	exp := int((bits>>23)&0xff) - 127 + 15
	mant := bits & 0x7fffff
	switch {
	case (bits>>23)&0xff == 0xff: // infinity or NaN
		if mant != 0 {
			return sign | 0x7e00
		}
		return sign | 0x7c00
	case exp >= 0x1f: // too large: infinity
		return sign | 0x7c00
	case exp <= 0: // subnormal or zero
		if exp < -10 {
			return sign
		}
		mant |= 0x800000
		shift := uint32(14 - exp) //nolint:gosec // exp is -10 to 0 here
		half := mant >> shift
		rem := mant & (1<<shift - 1)
		mid := uint32(1) << (shift - 1)
		if rem > mid || (rem == mid && half&1 == 1) {
			half++
		}
		return sign | uint16(half) //nolint:gosec // at most 0x400
	}
	half := uint32(exp)<<10 | mant>>13 //nolint:gosec // exp is 1 to 30 here
	rem := mant & 0x1fff
	if rem > 0x1000 || (rem == 0x1000 && half&1 == 1) {
		half++ // may carry into the exponent, to infinity at most
	}
	return sign | uint16(half) //nolint:gosec // at most 0x7c00
}

// halfToFloat widens a half-precision number.
func halfToFloat(h uint16) float32 {
	sign := uint32(h&0x8000) << 16
	exp := uint32(h>>10) & 0x1f
	mant := uint32(h & 0x3ff)
	switch exp {
	case 0x1f:
		return math.Float32frombits(sign | 0x7f800000 | mant<<13)
	case 0:
		if mant == 0 {
			return math.Float32frombits(sign)
		}
		f := float32(mant) / (1 << 24)
		if sign != 0 {
			return -f
		}
		return f
	}
	return math.Float32frombits(sign | (exp+127-15)<<23 | mant<<13)
}
