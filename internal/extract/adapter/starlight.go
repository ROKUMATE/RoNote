package adapter

// starlight covers Astro Starlight sites. Their code blocks are Expressive
// Code frames, which the generic rules already read from the copy payload.
var starlight = &Adapter{
	Name:       "starlight",
	Version:    "1",
	Generators: []string{"Starlight"},
	Signature:  ".sl-markdown-content",
	Root:       "main",
	Require:    []string{".sl-markdown-content"},
	// Heading anchor links hold "Section titled …" for screen readers. The
	// footer after the content holds the last-updated date, the edit link
	// and pagination. Islands are interactive widgets: their labels, such as
	// "Idle" or "Pause", are controls, not text.
	Remove:   ".sl-anchor-link, .sl-container > footer, astro-island",
	Callouts: []Callout{{Selector: "aside.starlight-aside", Title: ".starlight-aside__title"}},
	Tabs:     []Tabs{{Container: "starlight-tabs", Label: "[role=tab]", Panel: "[role=tabpanel]"}},
}
