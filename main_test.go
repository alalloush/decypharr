package main

import (
	"net"
	"testing"
)

// pprof has no authentication, so ENABLE_PPROF alone must not expose it to
// the network; listening elsewhere takes an explicit -pprof address.
func TestPprofListensOnLoopbackByDefault(t *testing.T) {
	addr := parseFlags(nil).pprofAddr
	host, _, err := net.SplitHostPort(addr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		t.Fatalf("default pprof address %q is not loopback", addr)
	}
	if got := parseFlags([]string{"-pprof", ":6060"}).pprofAddr; got != ":6060" {
		t.Fatalf("-pprof :6060 gives %q", got)
	}
}
