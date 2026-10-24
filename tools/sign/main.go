// Command sign makes and uses the key that signs Haven releases.
//
//	go run ./tools/sign keygen <file>   create a new signing key (keep it secret and backed up)
//	go run ./tools/sign sign <file>     write <file>.sig using the key in $HAVEN_SIGNING_KEY
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: sign keygen <file> | sign sign <file>")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "keygen":
		seed := make([]byte, ed25519.SeedSize)
		if _, err := rand.Read(seed); err != nil {
			fail(err)
		}
		if err := os.WriteFile(os.Args[2], []byte(base64.StdEncoding.EncodeToString(seed)+"\n"), 0o600); err != nil {
			fail(err)
		}
		pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
		fmt.Println("public key:", base64.StdEncoding.EncodeToString(pub))
	case "sign":
		raw := strings.TrimSpace(os.Getenv("HAVEN_SIGNING_KEY"))
		seed, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(seed) != ed25519.SeedSize {
			fail(fmt.Errorf("HAVEN_SIGNING_KEY is missing or not a valid key"))
		}
		data, err := os.ReadFile(os.Args[2])
		if err != nil {
			fail(err)
		}
		sig := ed25519.Sign(ed25519.NewKeyFromSeed(seed), data)
		if err := os.WriteFile(os.Args[2]+".sig", []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
			fail(err)
		}
	default:
		fail(fmt.Errorf("unknown command %q", os.Args[1]))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "sign:", err)
	os.Exit(1)
}
