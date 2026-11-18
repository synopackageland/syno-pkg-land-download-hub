package gopeed

import (
	"net"
	"net/url"
	"strconv"
)

// BT endpoint admission uses the same address floor as App HTTP fetches.
// Upstream owns later DNS lookups, redirects and peer sockets; these checks
// cannot guarantee their final connection addresses.
func publicBTEndpoint(raw string, tracker bool) bool {
	u, err := url.Parse(raw)
	return err == nil && u.User == nil && u.Host != "" && publicHost(u.Hostname()) &&
		(u.Scheme == "http" || u.Scheme == "https" || tracker && u.Scheme == "udp")
}

func publicMagnetEndpoints(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	for _, key := range []string{"tr", "ws", "xs", "as"} {
		for _, address := range query[key] {
			if !publicBTEndpoint(address, key == "tr") {
				return false
			}
		}
	}
	for _, address := range query["x.pe"] {
		host, port, err := net.SplitHostPort(address)
		n, parseErr := strconv.Atoi(port)
		if err != nil || parseErr != nil || n < 1 || n > 65535 || !publicHost(host) {
			return false
		}
	}
	return true
}

func publicTorrentEndpoints(top bvalue) bool {
	var check func(bvalue, bool) bool
	check = func(value bvalue, tracker bool) bool {
		switch value.kind {
		case 's':
			return len(value.str) == 0 || publicBTEndpoint(string(value.str), tracker)
		case 'l':
			for _, item := range value.list {
				if !check(item, tracker) {
					return false
				}
			}
			return true
		}
		return false
	}
	for _, key := range []string{"announce", "announce-list", "url-list", "httpseeds"} {
		if value, ok := top.dict[key]; ok && !check(value, key == "announce" || key == "announce-list") {
			return false
		}
	}
	return true
}
