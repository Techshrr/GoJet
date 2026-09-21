// This bounded probe calls the production reconciler against the real CI
// database. It does not insert events or deliveries and never calls a sender.
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/internal/links"
)

func run() error {
	db, err := links.OpenMySQL(os.Getenv("GOJET_MYSQL_DSN"))
	if err != nil {
		return err
	}
	defer db.Close()
	r := links.NewRedisClient(os.Getenv("GOJET_REDIS_ADDR"), os.Getenv("GOJET_REDIS_PASSWORD"), 0)
	defer r.Close()
	key, err := hex.DecodeString(os.Getenv("GOJET_ADMIN_TOTP_KEY_HEX"))
	if err != nil {
		return err
	}
	cipher, err := admin.NewSecretCipher(os.Getenv("GOJET_ADMIN_TOTP_KEY_ID"), key)
	if err != nil {
		return err
	}
	a, err := admin.NewWorkspaceWebhookAuthority(db, r, cipher, nil, nil)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	count, err := a.QueueLinkEvents(ctx, 100)
	if err != nil {
		return err
	}
	fmt.Println(count)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "production webhook event reconciliation failed")
		os.Exit(1)
	}
}
