package adapter

// vitepress covers VitePress sites. Shell prompts in code are unselectable
// spans, which the generic never-text rule already leaves out.
var vitepress = &Adapter{
	Name:       "vitepress",
	Version:    "1",
	Generators: []string{"VitePress"},
	Signature:  ".vp-doc",
	Root:       ".vp-doc",
	// Custom containers (::: tip) and GitHub alerts are div.custom-block with
	// a title paragraph; ::: details is a <details> with a summary.
	Callouts: []Callout{{Selector: ".custom-block", Title: ".custom-block-title, summary"}},
	Tabs:     []Tabs{{Container: ".vp-code-group", Label: ".tabs > label", Panel: ".blocks > div"}},
	// A code block's wrapper also holds its copy button, a language label
	// and, with line numbers, a column of them beside the <pre>.
	Code: []Code{{Block: `div[class*="language-"]`}},
}
