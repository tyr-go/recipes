// Command token prints a token for the service tasks, signed with the key
// of JWT_SECRET, for development and tests. In production, tokens come
// from whoever issues them for your users, signed with the same key.
//
//	TOKEN=$(go run ./cmd/token -sub alice)
//	TOKEN=$(go run ./cmd/token -sub root -role admin -ttl 15m)
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tyr-go/recipes/tasks/auth"
)

func main() {
	sub := flag.String("sub", "", "the id of the caller, such as alice")
	var roles []string
	flag.Func("role", "a role of the caller, such as admin; repeat for more", func(role string) error {
		if strings.TrimSpace(role) == "" {
			return fmt.Errorf("an empty role")
		}
		roles = append(roles, role)
		return nil
	})
	ttl := flag.Duration("ttl", time.Hour, "how long the token lasts")
	flag.Parse()

	key := []byte(os.Getenv("JWT_SECRET"))
	switch {
	case *sub == "":
		fail("-sub is required")
	case len(key) < auth.MinKeySize:
		fail(fmt.Sprintf("JWT_SECRET has %d bytes: set it to the key of the service, of %d bytes or more", len(key), auth.MinKeySize))
	case *ttl <= 0:
		fail("-ttl must be positive")
	}
	fmt.Println(auth.NewToken(key, auth.Caller{ID: *sub, Roles: roles}, *ttl))
}

// fail reports what's wrong and exits.
func fail(msg string) {
	fmt.Fprintln(os.Stderr, "token:", msg)
	os.Exit(2)
}
