package embed

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

// Fake embeds deterministically, with no model: each word of a text adds to
// the dimension its hash picks, and the vector is normalised. Texts that
// share words point the same way, so tests can search by meaning, of a sort.
type Fake struct{}

// FakeModel is the name Fake gives its vectors.
const FakeModel = "fake-words"

// Model names the fake.
func (Fake) Model() string { return FakeModel }

// Embed embeds each text by its words.
func (Fake) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = FakeVector(text)
	}
	return out, nil
}

// FakeVector is Fake's vector for text.
func FakeVector(text string) []float32 {
	v := make([]float32, Dims)
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	for _, w := range words {
		h := fnv.New32a()
		_, _ = h.Write([]byte(w))
		sum := h.Sum32()
		sign := float32(1)
		if sum&1 == 1 {
			sign = -1
		}
		v[(sum>>1)%Dims] += sign
	}
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		v[0] = 1
		return v
	}
	scale := float32(1 / math.Sqrt(norm))
	for i := range v {
		v[i] *= scale
	}
	return v
}
