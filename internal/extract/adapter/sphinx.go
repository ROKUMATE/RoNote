package adapter

// sphinx covers Sphinx sites: the basic and Alabaster themes (docs.python.org,
// many Read the Docs projects) put the content in div.body, the Read the Docs
// theme in an articleBody, and Furo in an article. Admonitions carry the
// generic admonition class and title, and ¶ heading links are dropped by the
// generic permalink rule.
var sphinx = &Adapter{
	Name:      "sphinx",
	Version:   "1",
	Signature: `script[src*="documentation_options.js"], script#documentation_options`,
	Root:      `div.body, div[itemprop="articleBody"], article[role="main"]`,
	// A caption's own ¶ link is not a heading's, so it is named here.
	Remove: "a.headerlink",
	// With line numbers, Pygments puts them in a table cell of their own,
	// next to the cell with the code.
	Code: []Code{
		{Block: "div.literal-block-wrapper", Pre: []string{"td.code pre", "pre"}, Title: ".code-block-caption .caption-text"},
		{Block: `div[class^="highlight-"], div[class*=" highlight-"]`, Pre: []string{"td.code pre", "pre"}},
	},
}
