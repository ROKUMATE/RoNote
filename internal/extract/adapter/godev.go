package adapter

// godev covers go.dev's documentation pages, a custom site rather than a
// framework, so it is found by host. The breadcrumb sits inside the
// article.
var godev = &Adapter{
	Name:    "godev",
	Version: "1",
	Hosts:   []string{"go.dev"},
	Root:    "article.Doc",
	Remove:  ".SiteBreadcrumb",
}
