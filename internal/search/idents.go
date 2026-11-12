package search

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// identifier matches a whole identifier: words of letters, digits and
// underscores joined by dots, dashes, colons or slashes, such as
// http.ServeMux, ERR_MODULE_NOT_FOUND, __init__ or no-verify.
var identifier = regexp.MustCompile(`[\p{L}\p{N}_]+(?:[.\-:/]+[\p{L}\p{N}_]+)*`)

// maxIdents bounds the identifier tokens one chunk keeps; tsvector
// positions stop at 16383.
const maxIdents = 4000

// Idents returns the identifier tokens of code: each whole identifier, its
// parts between punctuation, and those parts' camelCase words, lowercased,
// each once, in order of first appearance. Documents and queries are split
// alike, so a query for any of the forms finds the others.
func Idents(code string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(tok string) {
		tok = strings.ToLower(tok)
		if len(tok) < 2 || seen[tok] || len(out) >= maxIdents {
			return
		}
		seen[tok] = true
		out = append(out, tok)
	}
	for _, whole := range identifier.FindAllString(code, -1) {
		add(whole)
		for _, part := range parts(whole) {
			add(part)
			for _, word := range camelWords(part) {
				add(word)
			}
		}
	}
	return out
}

// parts splits an identifier at everything but letters and digits.
func parts(whole string) []string {
	return strings.FieldsFunc(whole, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
}

// camelWords splits a word at its case changes: useEffect gives use and
// Effect, HTTPServer gives HTTP and Server. A word of one case is none.
func camelWords(word string) []string {
	runes := []rune(word)
	var words []string
	start := 0
	for i := 1; i < len(runes); i++ {
		lowerThenUpper := unicode.IsLower(runes[i-1]) && unicode.IsUpper(runes[i])
		acronymEnd := i+1 < len(runes) && unicode.IsUpper(runes[i-1]) && unicode.IsUpper(runes[i]) && unicode.IsLower(runes[i+1])
		if lowerThenUpper || acronymEnd {
			words = append(words, string(runes[start:i]))
			start = i
		}
	}
	if start == 0 {
		return nil
	}
	return append(words, string(runes[start:]))
}

// IdentVector returns tokens as a tsvector literal, weight A, for Postgres
// to read with ::tsvector. Tokens hold no quotes or backslashes, so none
// need escaping.
func IdentVector(tokens []string) string {
	var sb strings.Builder
	for i, tok := range tokens {
		if i > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "'%s':%dA", tok, min(i+1, 16383))
	}
	return sb.String()
}

// looksLikeIdentifier reports whether a word of a query is code rather than
// prose: it has punctuation or an underscore inside, a digit, or a capital
// after its first letter. Plain words are left to the English search.
func looksLikeIdentifier(word string) bool {
	for i, r := range word {
		if unicode.IsDigit(r) || r == '_' || r == '.' || r == ':' || r == '/' || (r == '-' && i > 0) ||
			(i > 0 && unicode.IsUpper(r)) {
			return true
		}
	}
	return false
}

// IdentQuery returns a tsquery that matches the identifiers in q: for each,
// the whole token, or all of its parts, or all of its camelCase words; the
// identifiers OR'ed together. It is "" when q has none.
func IdentQuery(q string) string {
	var alternatives []string
	for _, whole := range identifier.FindAllString(q, -1) {
		if !looksLikeIdentifier(whole) {
			continue
		}
		forms := []string{quoteLexeme(whole)}
		ps := parts(whole)
		if c := conjunction(ps); len(ps) > 1 && c != "" {
			forms = append(forms, c)
		}
		var words []string
		for _, p := range ps {
			if w := camelWords(p); len(w) > 0 {
				words = append(words, w...)
			} else {
				words = append(words, p)
			}
		}
		if c := conjunction(words); len(words) > len(ps) && c != "" {
			forms = append(forms, c)
		}
		alternatives = append(alternatives, "("+strings.Join(forms, " | ")+")")
	}
	return strings.Join(alternatives, " | ")
}

// conjunction is words AND'ed, leaving out one-letter words, which
// documents don't index; "" when none is left.
func conjunction(words []string) string {
	quoted := make([]string, 0, len(words))
	for _, w := range words {
		if len(w) >= 2 {
			quoted = append(quoted, quoteLexeme(w))
		}
	}
	if len(quoted) == 0 {
		return ""
	}
	return "(" + strings.Join(quoted, " & ") + ")"
}

func quoteLexeme(tok string) string {
	return "'" + strings.ToLower(tok) + "'"
}
