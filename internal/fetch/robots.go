package fetch

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// robotsAgent is the product token robots.txt groups are matched on.
	robotsAgent    = "ronotebot"
	robotsTTL      = time.Hour
	maxRobotsBytes = 500 << 10
)

// robotsPolicy is what one site's robots.txt says about RoNoteBot.
type robotsPolicy struct {
	disallowAll bool
	reason      string // why disallowAll is set, for the error message
	rules       []robotsRule
}

type robotsRule struct {
	allow   bool
	pattern string
}

// allows applies RFC 9309: the longest matching pattern wins, and Allow wins
// a tie. With no matching rule the path is allowed.
func (p robotsPolicy) allows(path string) bool {
	if p.disallowAll {
		return false
	}
	if path == "/robots.txt" {
		return true
	}
	best, allowed := -1, true
	for _, r := range p.rules {
		if !robotsMatch(r.pattern, path) {
			continue
		}
		if n := len(r.pattern); n > best || (n == best && r.allow) {
			best, allowed = n, r.allow
		}
	}
	return allowed
}

// parseRobots reads the groups that name RoNoteBot, or the "*" groups if none
// do. Groups for the same agent are combined.
func parseRobots(body []byte) robotsPolicy {
	type group struct {
		agents []string
		rules  []robotsRule
	}
	var groups []group
	inAgents := false
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line, _, _ := strings.Cut(scanner.Text(), "#")
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		switch key {
		case "user-agent":
			if !inAgents {
				groups = append(groups, group{})
				inAgents = true
			}
			token, _, _ := strings.Cut(strings.ToLower(value), "/")
			groups[len(groups)-1].agents = append(groups[len(groups)-1].agents, strings.TrimSpace(token))
		case "allow", "disallow":
			inAgents = false
			if len(groups) == 0 || value == "" {
				continue
			}
			groups[len(groups)-1].rules = append(groups[len(groups)-1].rules, robotsRule{allow: key == "allow", pattern: value})
		}
	}

	var ours, anyone []robotsRule
	matched := false
	for _, g := range groups {
		for _, agent := range g.agents {
			switch agent {
			case robotsAgent:
				matched = true
				ours = append(ours, g.rules...)
			case "*":
				anyone = append(anyone, g.rules...)
			}
		}
	}
	if matched {
		return robotsPolicy{rules: ours}
	}
	return robotsPolicy{rules: anyone}
}

// robotsMatch reports whether a robots.txt path pattern matches path. "*"
// matches any run of characters and a trailing "$" anchors the end.
func robotsMatch(pattern, path string) bool {
	anchored := strings.HasSuffix(pattern, "$")
	pattern = strings.TrimSuffix(pattern, "$")
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(path, parts[0]) {
		return false
	}
	rest := path[len(parts[0]):]
	for i, part := range parts[1:] {
		if anchored && i == len(parts)-2 {
			return strings.HasSuffix(rest, part)
		}
		at := strings.Index(rest, part)
		if at < 0 {
			return false
		}
		rest = rest[at+len(part):]
	}
	return !anchored || rest == ""
}

// robotsCache keeps each origin's policy for robotsTTL.
type robotsCache struct {
	mu      sync.Mutex
	entries map[string]robotsEntry
}

type robotsEntry struct {
	policy  robotsPolicy
	expires time.Time
}

func (c *robotsCache) get(origin string) (robotsPolicy, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[origin]
	if !ok || time.Now().After(e.expires) {
		return robotsPolicy{}, false
	}
	return e.policy, true
}

func (c *robotsCache) put(origin string, p robotsPolicy) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if len(c.entries) > 1024 {
		for key, e := range c.entries {
			if now.After(e.expires) {
				delete(c.entries, key)
			}
		}
	}
	c.entries[origin] = robotsEntry{policy: p, expires: now.Add(robotsTTL)}
}

// checkRobots fails with ErrDisallowedByRobots unless the site's robots.txt
// lets RoNoteBot fetch u.
func (f *Fetcher) checkRobots(ctx context.Context, u *url.URL) error {
	robotsURL := &url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/robots.txt"}
	origin := u.Scheme + "://" + u.Host
	policy, ok := f.robotsCache.get(origin)
	if !ok {
		var cacheable bool
		var err error
		if policy, cacheable, err = f.fetchRobots(ctx, robotsURL); err != nil {
			return err
		}
		if cacheable {
			f.robotsCache.put(origin, policy)
		}
	}
	if !policy.allows(u.EscapedPath() + queryPart(u)) {
		if policy.reason != "" {
			return fmt.Errorf("%w: %s", ErrDisallowedByRobots, policy.reason)
		}
		return fmt.Errorf("%w: %s", ErrDisallowedByRobots, u)
	}
	return nil
}

// fetchRobots reads a site's robots.txt. A missing file (404 or 410) allows
// everything, and any other client error disallows everything. A server
// error (5xx) gives ErrRobotsUnavailable: nothing may be fetched until
// robots.txt can be read, and the answer is not cached so the next fetch asks
// again. Transport errors are returned as they are.
func (f *Fetcher) fetchRobots(ctx context.Context, robotsURL *url.URL) (policy robotsPolicy, cacheable bool, err error) {
	if err := f.limiter.wait(ctx, robotsURL.Hostname()); err != nil {
		return robotsPolicy{}, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL.String(), http.NoBody)
	if err != nil {
		return robotsPolicy{}, false, fmt.Errorf("%w: %w", ErrInvalidURL, err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := f.robotsClient.Do(req)
	if err != nil {
		return robotsPolicy{}, false, fmt.Errorf("fetch robots.txt: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // nothing useful to do if closing a read body fails

	switch code := resp.StatusCode; {
	case code >= 200 && code <= 299:
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxRobotsBytes))
		if err != nil {
			return robotsPolicy{}, false, fmt.Errorf("read robots.txt: %w", err)
		}
		return parseRobots(body), true, nil
	case code == http.StatusNotFound || code == http.StatusGone:
		return robotsPolicy{}, true, nil
	case code >= 500:
		return robotsPolicy{}, false, fmt.Errorf("%w: %s answered %d", ErrRobotsUnavailable, robotsURL, code)
	default:
		reason := fmt.Sprintf("%s answered %d", robotsURL, code)
		return robotsPolicy{disallowAll: true, reason: reason}, true, nil
	}
}

func queryPart(u *url.URL) string {
	if u.RawQuery == "" {
		return ""
	}
	return "?" + u.RawQuery
}
