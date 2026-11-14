package chat

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"
)

// What one turn may send and say (docs/adr/0012-chat.md).
const (
	// inputCap bounds the input tokens of every call, far below the
	// 100,000 past which Claude Haiku 5.5 costs five times as much.
	inputCap = 30_000
	// charsPerToken overcounts tokens: prose runs about 4 characters a
	// token and code about 3.
	charsPerToken = 3
	// maxTokens bounds what one call says, thinking included.
	maxTokens = 1024
	// maxToolRounds bounds the searches a turn makes beyond the first.
	maxToolRounds = 3
	// MaxQuestion bounds a question, in characters.
	MaxQuestion = 2000
	// maxPassageChars bounds the text one passage sends: a code block
	// larger than a chunk is sent cut.
	maxPassageChars = 2000
	// callOverhead covers what a call adds beyond the texts counted: the
	// blocks' JSON, a tool round's call and its thinking.
	callOverhead = 200
)

// tokens is a count, rounded up and on the high side, of the tokens text
// takes.
func tokens(text string) int {
	return (utf8.RuneCountInString(text) + charsPerToken - 1) / charsPerToken
}

// fit cuts p's texts to maxPassageChars in all, dropping the blocks past
// the cut, so citations still name the blocks it sends.
func fit(p Passage) Passage {
	room := maxPassageChars
	for i, text := range p.Texts {
		n := utf8.RuneCountInString(text)
		if n <= room {
			room -= n
			continue
		}
		if room == 0 {
			p.BlockIDs, p.Texts = p.BlockIDs[:i], p.Texts[:i]
			return p
		}
		p.BlockIDs, p.Texts = p.BlockIDs[:i+1], append(slices.Clone(p.Texts[:i]), string([]rune(text)[:room])+"…")
		return p
	}
	return p
}

// fixedTokens are the tokens of the system prompt and the tools, the same
// in every call.
func fixedTokens() int {
	schema, _ := json.Marshal(searchTool)
	return tokens(systemPrompt) + tokens(string(schema))
}

// roundTokens is the most one tool round can add: the reply that calls
// the tool, at most maxTokens with its thinking, and the passages it gets.
func roundTokens() int {
	return maxTokens + passagesPerSearch*(maxPassageChars/charsPerToken+callOverhead/passagesPerSearch) + callOverhead
}

// exchange is an earlier question and its answer, sent as plain text.
type exchange struct {
	question, answer string
}

// history returns the latest exchanges that fit in room tokens, as
// messages, the oldest dropped first.
func history(exchanges []exchange, room int) []Message {
	start := len(exchanges)
	for start > 0 {
		e := exchanges[start-1]
		cost := tokens(e.question) + tokens(e.answer) + callOverhead
		if cost > room {
			break
		}
		room -= cost
		start--
	}
	var out []Message
	for _, e := range exchanges[start:] {
		out = append(out,
			Message{Role: User, Blocks: []Block{Text{Text: e.question}}},
			Message{Role: Assistant, Blocks: []Block{Text{Text: e.answer}}},
		)
	}
	return out
}

// historyRoom is what a turn leaves for earlier exchanges: the cap less the
// fixed part, the question and its passages, and room for every tool
// round.
func historyRoom(question string, passages []Passage) int {
	return inputCap - fixedTokens() - tokens(question) - passageTokens(passages) - maxToolRounds*roundTokens() - callOverhead
}

func passageTokens(passages []Passage) int {
	n := 0
	for _, p := range passages {
		n += tokens(p.Title) + tokens(strings.Join(p.Texts, "")) + callOverhead/passagesPerSearch
	}
	return n
}

// firstCallTokens counts the tokens of a turn's first call as the cap
// does: its fixed part and every text it sends. Later calls add to what the
// API says the one before read and wrote.
func firstCallTokens(req Request) int {
	n := fixedTokens()
	for _, m := range req.Messages {
		for _, b := range m.Blocks {
			n += blockTokens(b)
		}
	}
	return n
}

func blockTokens(b Block) int {
	switch b := b.(type) {
	case Text:
		return tokens(b.Text)
	case Result:
		return tokens(b.Title) + tokens(strings.Join(b.Texts, "")) + callOverhead/passagesPerSearch
	case ToolResult:
		n := tokens(b.Text) + callOverhead/passagesPerSearch
		for _, r := range b.Results {
			n += blockTokens(r)
		}
		return n
	}
	return 0
}
