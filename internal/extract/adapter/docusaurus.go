package adapter

// docusaurus covers Docusaurus docs pages. Its admonitions carry the generic
// alert class and title heading, so the generic callout rule handles them.
var docusaurus = &Adapter{
	Name:       "docusaurus",
	Version:    "1",
	Generators: []string{"Docusaurus"},
	Signature:  ".theme-doc-markdown",
	// The markdown container holds the page's title; the breadcrumbs,
	// version badge, mobile table of contents and footer sit outside it.
	Root: ".theme-doc-markdown",
	// Demo frames draw a fake browser bar ("http://localhost:3000"), and live
	// code blocks label their editor and result panes.
	Remove: `[class*="browserWindowHeader"], [class*="playgroundHeader"]`,
	Tabs: []Tabs{{
		Container: ".theme-tabs-container, .tabs-container",
		Label:     "[role=tab]",
		Panel:     "[role=tabpanel]",
	}},
	Code: []Code{{
		Block:   ".theme-code-block",
		Title:   `[class*="codeBlockTitle"]`,
		NotText: `[class*="codeLineNumber"]`,
	}},
}
