package adapter

// mdbook covers mdBook books, such as The Rust Programming Language, in the
// 0.4 layout (#content) and the 0.5 one (#mdbook-content).
var mdbook = &Adapter{
	Name:      "mdbook",
	Version:   "1",
	Signature: "#mdbook-content > main, #page-wrapper #content > main",
	Root:      "#mdbook-content > main, #content > main",
	Code: []Code{{
		Block: "pre",
		// Books label a listing with a file name just before it, alone in a
		// paragraph or bare.
		TitleBefore: "span.filename, p:has(span.filename)",
		// Lines marked boring are hidden, and the copy button copies only
		// what is shown.
		NotText: ".boring",
	}},
}
