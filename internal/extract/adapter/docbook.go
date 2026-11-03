package adapter

// docbook covers HTML made by the DocBook XSL stylesheets, such as the
// PostgreSQL manual: each page is a navigation header, one division (a
// chapter, section, reference entry, ...) and a navigation footer.
var docbook = &Adapter{
	Name:       "docbook",
	Version:    "1",
	Generators: []string{"DocBook XSL"},
	Signature:  "div.navheader ~ div.navfooter",
	Root:       "div.navheader + div",
	// A division starts with a generated table of contents. postgresql.org
	// puts a correction form with its own heading after the manual page.
	Remove:   "div.toc, #docComments",
	Callouts: []Callout{{Selector: "div.note, div.tip, div.important, div.caution, div.warning", Title: ".title"}},
}
