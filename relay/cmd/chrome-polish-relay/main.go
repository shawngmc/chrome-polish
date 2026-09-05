// Command chrome-polish-relay is the optional relay host for remote-mode
// sessions. For v1, a plain sshd with a forwarding-only account is
// sufficient (see DESIGN.md section 3b) and this binary is not required.
// This is the placeholder for the v2 custom relay: a Go SSH server issuing
// short-lived, single-use, no-shell tunnel tokens instead of durable SSH
// credentials.
package main

import (
	"flag"
	"log"
)

func main() {
	listen := flag.String("listen", ":2222", "address for the relay's SSH server to listen on")
	flag.Parse()

	log.Fatalf("chrome-polish-relay: not yet implemented (would listen on %s)", *listen)
}
