package fetch

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"syscall"
)

// Ranges no fetch may reach, even in dev.
var alwaysBlocked = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),          // "this network", including the unspecified 0.0.0.0
	netip.MustParsePrefix("169.254.169.254/32"), // cloud metadata
	netip.MustParsePrefix("224.0.0.0/4"),        // multicast
	netip.MustParsePrefix("240.0.0.0/4"),        // reserved, including broadcast
	netip.MustParsePrefix("::/128"),             // unspecified
	netip.MustParsePrefix("::ffff:0:0/96"),      // IPv4-mapped IPv6
	netip.MustParsePrefix("ff00::/8"),           // multicast
}

// Ranges blocked unless Options.AllowPrivateNetworks is set.
var privateBlocked = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),     // private
	netip.MustParsePrefix("100.64.0.0/10"),  // carrier-grade NAT
	netip.MustParsePrefix("127.0.0.0/8"),    // loopback
	netip.MustParsePrefix("169.254.0.0/16"), // link-local
	netip.MustParsePrefix("172.16.0.0/12"),  // private
	netip.MustParsePrefix("192.168.0.0/16"), // private
	netip.MustParsePrefix("::1/128"),        // loopback
	netip.MustParsePrefix("fc00::/7"),       // unique local
	netip.MustParsePrefix("fe80::/10"),      // link-local
	netip.MustParsePrefix("64:ff9b::/96"),   // NAT64, which maps onto IPv4
}

// addressGuard returns the check run on every address the fetcher is about
// to connect to.
func addressGuard(allowPrivate bool) func(netip.AddrPort) error {
	return func(ap netip.AddrPort) error {
		addr := ap.Addr().WithZone("")
		for _, p := range alwaysBlocked {
			if p.Contains(addr) {
				return fmt.Errorf("%w: %s is in %s", ErrBlockedAddress, addr, p)
			}
		}
		if allowPrivate {
			return nil
		}
		for _, p := range privateBlocked {
			if p.Contains(addr) {
				return fmt.Errorf("%w: %s is in %s", ErrBlockedAddress, addr, p)
			}
		}
		return nil
	}
}

// checkURL applies the rules every requested and redirected URL must meet.
func (f *Fetcher) checkURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: scheme %q is not http or https", ErrInvalidURL, u.Scheme)
	}
	if u.User != nil {
		return fmt.Errorf("%w: URLs with credentials are not fetched", ErrInvalidURL)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("%w: %q has no host", ErrInvalidURL, u)
	}
	if port := u.Port(); port != "" && port != "80" && port != "443" && !f.allowAnyPort {
		return fmt.Errorf("%w: port %s is not 80 or 443", ErrBlockedAddress, port)
	}
	return nil
}

// dial resolves addr through the fetcher's resolver and connects to each
// address in turn. The dialer's Control hook runs the guard on the address
// actually being connected, so a name that resolves differently later (DNS
// rebinding) is checked again.
func (f *Fetcher) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips := []netip.Addr{}
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = append(ips, ip)
	} else if ips, err = f.resolver.LookupNetIP(ctx, "ip", host); err != nil {
		return nil, err
	}

	dialer := net.Dialer{
		Timeout: connectTimeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return fmt.Errorf("%w: cannot parse %q", ErrBlockedAddress, address)
			}
			return f.guard(ap)
		},
	}
	var errs []error
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return nil, fmt.Errorf("no addresses for %s", host)
	}
	return nil, errors.Join(errs...)
}
