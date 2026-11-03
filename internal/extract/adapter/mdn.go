package adapter

// mdn covers MDN Web Docs, which runs its own stack, so it is found by host.
// The generic rules handle its content; this drops the feedback footer and
// survey, makes notecards callouts, and reads code blocks' language from
// class="brush: js".
var mdn = &Adapter{
	Name:     "mdn",
	Version:  "1",
	Hosts:    []string{"developer.mozilla.org"},
	Root:     "main#content",
	Remove:   ".article-footer, mdn-survey",
	Callouts: []Callout{{Selector: "div.notecard"}},
	// An example's header only repeats its language.
	Code: []Code{{Block: "div.code-example", LangAfter: "brush:"}},
}
