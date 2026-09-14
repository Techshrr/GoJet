package main

import (
	"context"
	"database/sql"
	"strings"

	authn "github.com/Techshrr/GoJet/internal/auth"
	workspacecore "github.com/Techshrr/GoJet/internal/workspace"
)

func init() {
	if err := authn.ConfigureRegistrationTxHook(provisionRegistrationWorkspace); err != nil {
		panic(err)
	}
}

func provisionRegistrationWorkspace(ctx context.Context, tx *sql.Tx, user authn.User, correlationID string) error {
	name := strings.TrimSpace(user.DisplayName)
	if name == "" {
		name = user.EmailNormalized
	}
	_, err := workspacecore.ProvisionInitialWorkspaceTx(ctx, tx, workspacecore.Principal{
		UserID:      user.ID,
		Email:       user.EmailNormalized,
		DisplayName: user.DisplayName,
	}, name, correlationID)
	return err
}
