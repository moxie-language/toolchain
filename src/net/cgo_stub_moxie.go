// Moxie does not use cgo for DNS. Pure Go resolver only.

package net

import "context"

// cgoAvailable is false — moxie uses the pure Go DNS resolver.
const cgoAvailable = false

func cgoLookupHost(_ context.Context, name string) ([]string, error) {
	return nil, &DNSError{Err: "cgo resolver not available", Name: name}
}

func cgoLookupPort(_ context.Context, network, service string) (int, error) {
	return 0, &DNSError{Err: "cgo resolver not available", Name: network + "/" + service}
}

func cgoLookupIP(_ context.Context, network, name string) ([]IPAddr, error) {
	return nil, &DNSError{Err: "cgo resolver not available", Name: name}
}

func cgoLookupCNAME(_ context.Context, name string) (string, error, bool) {
	return "", nil, false
}

func cgoLookupPTR(_ context.Context, addr string) ([]string, error) {
	return nil, &DNSError{Err: "cgo resolver not available", Name: addr}
}
