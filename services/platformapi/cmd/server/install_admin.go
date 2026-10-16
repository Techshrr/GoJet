package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/internal/links"
)

var errInstallAdmin = errors.New("initial administrator creation refused")

type initialAdminRequest struct {
	Email         string `json:"email"`
	DisplayName   string `json:"display_name"`
	Password      string `json:"password"`
	CorrelationID string `json:"correlation_id"`
}

func readInitialAdmin(input io.Reader) (initialAdminRequest, error) {
	var request initialAdminRequest
	data, err := io.ReadAll(io.LimitReader(input, 4097))
	if err != nil || len(data) > 4096 {
		return request, errInstallAdmin
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		return initialAdminRequest{}, errInstallAdmin
	}
	if strings.TrimSpace(request.DisplayName) == "" || len(request.DisplayName) > 128 || strings.ContainsAny(request.DisplayName, "\r\n\x00") {
		return initialAdminRequest{}, errInstallAdmin
	}
	return request, nil
}

// Only the local privileged Installer may call this mode. Never register an HTTP route.
// Credentials arrive on stdin, not argv; all failures are deliberately redacted.
func bootstrapInitialAdministrator(input io.Reader, output io.Writer) error {
	if os.Geteuid() != 0 || os.Getenv("GOJET_TEST_AUTH_ENABLED") == "1" {
		return errInstallAdmin
	}
	request, err := readInitialAdmin(input)
	if err != nil {
		return errInstallAdmin
	}
	dsn, redisAddr := os.Getenv("GOJET_MYSQL_DSN"), os.Getenv("GOJET_REDIS_ADDR")
	if dsn == "" || redisAddr == "" {
		return errInstallAdmin
	}
	db, err := links.OpenMySQL(dsn)
	if err != nil {
		return errInstallAdmin
	}
	defer db.Close()
	redisDB := 0
	if raw := strings.TrimSpace(os.Getenv("GOJET_REDIS_DB")); raw != "" {
		redisDB, err = strconv.Atoi(raw)
		if err != nil || redisDB < 0 {
			return errInstallAdmin
		}
	}
	redis := links.NewRedisClient(redisAddr, os.Getenv("GOJET_REDIS_PASSWORD"), redisDB)
	defer redis.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if db.PingContext(ctx) != nil || redis.Ping(ctx).Err() != nil {
		return errInstallAdmin
	}
	service, _, enabled, err := buildAdminAccessService(db, redis)
	if err != nil || !enabled {
		return errInstallAdmin
	}
	// A dedicated connection holds a database-scoped advisory lock across the
	// inherited Admin transaction, serializing concurrent installer processes.
	conn, err := db.Conn(ctx)
	if err != nil {
		return errInstallAdmin
	}
	defer conn.Close()
	var database string
	if conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database) != nil || database == "" {
		return errInstallAdmin
	}
	sum := sha256.Sum256([]byte(database))
	lock := fmt.Sprintf("gojet-admin-bootstrap-%x", sum[:16])
	var acquired int
	if conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", lock).Scan(&acquired) != nil || acquired != 1 {
		return errInstallAdmin
	}
	defer func() {
		release, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_, _ = conn.ExecContext(release, "SELECT RELEASE_LOCK(?)", lock)
	}()
	if _, err := service.BootstrapAdministrator(ctx, request.Email, request.DisplayName, request.Password,
		append([]string(nil), adminaccess.PermissionCatalog...), request.CorrelationID, time.Now()); err != nil {
		return errInstallAdmin
	}
	_, err = io.WriteString(output, "{\"created\":true}\n")
	return err
}
