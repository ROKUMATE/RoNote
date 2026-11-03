package adapter

// material covers Material for MkDocs and Zensical, its successor, which
// keeps the same page structure. Admonitions carry the generic admonition
// class and title, so the generic callout rule handles them.
var material = &Adapter{
	Name:       "material",
	Version:    "1",
	Generators: []string{"mkdocs-material", "zensical"},
	Signature:  `[data-md-component="content"]`,
	Root:       "article.md-content__inner",
	// Collapsible admonitions are <details> with the admonition type as class.
	Callouts: []Callout{{Selector: "details[class]", Title: "summary"}},
	Tabs: []Tabs{{
		Container: ".tabbed-set",
		Label:     ".tabbed-labels > label",
		Panel:     ".tabbed-content > .tabbed-block",
	}},
	// With line numbers, Pygments puts them in a table cell of their own,
	// next to the cell with the code.
	Code: []Code{{Block: "div.highlight", Pre: []string{"td.code pre", "pre"}, Title: ".filename"}},
}
