package adapter

// docsy covers Hugo sites on the Docsy theme, such as kubernetes.io. Alerts
// carry the generic alert class and a heading, so the generic callout rule
// handles them.
var docsy = &Adapter{
	Name:      "docsy",
	Version:   "1",
	Signature: ".td-content",
	Hosts:     []string{"kubernetes.io"},
	Root:      ".td-content",
	// The feedback widget, the last-modified line, and the row of tab
	// buttons, whose labels the tabs block takes over.
	Remove: `#pre-footer, [class*="feedback--"], .td-page-meta, .td-page-meta__lastmod, ul.nav-tabs`,
	Tabs:   []Tabs{{Container: ".tab-content", Panel: ".tab-pane"}},
	// Chroma puts line numbers in a table cell before the code, or in a span
	// at the start of each line. A code sample's header names its file.
	Code: []Code{
		{Block: ".code-sample", Pre: []string{"td.lntd:last-child pre", "pre"}, Title: ".copy-code-icon a", NotText: ".ln"},
		{Block: "div.highlight", Pre: []string{"td.lntd:last-child pre", "pre"}, NotText: ".ln"},
	},
}
