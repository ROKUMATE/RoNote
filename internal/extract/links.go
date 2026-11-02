package extract

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/rokumate/ronote/internal/ir"
)

// linker resolves and classifies the links and image sources of one page.
type linker struct {
	page *url.URL // the page's final URL, without its fragment
	base *url.URL // <base href> resolved against page, or page
	host string   // page's host, lowercased, without a leading "www."
}

func newLinker(doc *html.Node, page *url.URL) *linker {
	p := *page
	p.Fragment, p.RawFragment = "", ""
	l := &linker{page: &p, base: &p, host: siteHost(&p)}
	base := findFirst(doc, func(n *html.Node) bool { return n.DataAtom == atom.Base && hasAttr(n, "href") })
	if base != nil {
		if u, err := p.Parse(strings.TrimSpace(attr(base, "href"))); err == nil && isHTTP(u) {
			l.base = u
		}
	}
	return l
}

// resolve makes href absolute against the page's base and keeps it only if
// it is http(s).
func (l *linker) resolve(href string) (*url.URL, bool) {
	href = strings.TrimSpace(href)
	if href == "" {
		return nil, false
	}
	u, err := l.base.Parse(href)
	if err != nil || !isHTTP(u) {
		return nil, false
	}
	return u, true
}

// link classifies href. A "#fragment" href, or one that resolves to this page
// plus a fragment, is an anchor; a page on the same host, ignoring a leading
// "www.", is same_domain; any other http(s) or mailto URL is external. Other
// schemes (javascript:, data:, ...) give no link at all.
func (l *linker) link(href string) *ir.Link {
	href = strings.TrimSpace(href)
	if frag, ok := strings.CutPrefix(href, "#"); ok {
		if frag == "" {
			return nil
		}
		if u, err := url.Parse(href); err == nil {
			frag = u.Fragment
		}
		return &ir.Link{Href: "#" + frag, Kind: ir.LinkAnchor}
	}
	u, err := l.base.Parse(href)
	switch {
	case err != nil:
		return nil
	case u.Scheme == "mailto":
		return &ir.Link{Href: u.String(), Kind: ir.LinkExternal}
	case !isHTTP(u):
		return nil
	case u.Fragment != "" && samePage(u, l.page):
		return &ir.Link{Href: "#" + u.Fragment, Kind: ir.LinkAnchor}
	case siteHost(u) == l.host:
		return &ir.Link{Href: u.String(), Kind: ir.LinkSameDomain}
	default:
		return &ir.Link{Href: u.String(), Kind: ir.LinkExternal}
	}
}

func isHTTP(u *url.URL) bool {
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// siteHost is u's host for same-domain checks: lowercased, without port, and
// without one leading "www.".
func siteHost(u *url.URL) string {
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

// samePage reports whether u, ignoring its fragment, is the page itself.
func samePage(u, page *url.URL) bool {
	path := func(u *url.URL) string {
		if p := u.EscapedPath(); p != "" {
			return p
		}
		return "/"
	}
	return strings.EqualFold(u.Host, page.Host) && path(u) == path(page) && u.RawQuery == page.RawQuery
}
