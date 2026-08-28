package admin

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/trust"
	"github.com/redis/go-redis/v9"
)

const (
	workspaceWebhookSecretPrefix = "gwhsec_"
	workspaceWebhookLeasePrefix  = "workspace-webhook:lease:"
	workspaceWebhookMaxAttempts  = 5
	workspaceWebhookMaxBodyBytes = 256 * 1024
	WorkspaceWebhookServiceID    = trust.OperationsMonitorServiceID
)

type WorkspaceWebhookAuthority struct {
	db       *sql.DB
	redis    *redis.Client
	cipher   *SecretCipher
	resolver trust.IPResolver
	dialer   trust.ContextDialer
	client   *http.Client
}

type WorkspaceWebhookInput struct {
	Name        string   `json:"name"`
	EndpointURL string   `json:"endpoint_url"`
	Events      []string `json:"events"`
}

type WorkspaceWebhook struct {
	ID           string     `json:"id"`
	WorkspaceID  string     `json:"workspace_id"`
	Name         string     `json:"name"`
	EndpointURL  string     `json:"endpoint_url"`
	Events       []string   `json:"events"`
	SecretPrefix string     `json:"secret_prefix"`
	Status       string     `json:"status"`
	CreatedBy    string     `json:"created_by"`
	UpdatedBy    string     `json:"updated_by"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	RotatedAt    *time.Time `json:"rotated_at,omitempty"`
	DisabledAt   *time.Time `json:"disabled_at,omitempty"`
}

type WorkspaceWebhookSecret struct {
	Webhook WorkspaceWebhook `json:"webhook"`
	Secret  string           `json:"secret"`
}

type WorkspaceWebhookDelivery struct {
	ID               string     `json:"id"`
	WorkspaceID      string     `json:"workspace_id"`
	WebhookID        string     `json:"webhook_id"`
	EventID          string     `json:"event_id"`
	EventType        string     `json:"event_type"`
	BodySHA256       string     `json:"body_sha256"`
	Status           string     `json:"status"`
	Attempts         int        `json:"attempts"`
	NextAttemptAt    time.Time  `json:"next_attempt_at"`
	LastAttemptAt    *time.Time `json:"last_attempt_at,omitempty"`
	LastStatusCode   *int       `json:"last_status_code,omitempty"`
	LastErrorCode    string     `json:"last_error_code,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	DeliveredAt      *time.Time `json:"delivered_at,omitempty"`
}

type webhookWorkerDelivery struct {
	Delivery        WorkspaceWebhookDelivery
	Body            []byte
	EndpointURL     string
	SecretCipher    []byte
	SecretKeyID     string
	WebhookStatus   string
}

func NewWorkspaceWebhookAuthority(db *sql.DB, redisClient *redis.Client, cipher *SecretCipher, resolver trust.IPResolver, dialer trust.ContextDialer) (*WorkspaceWebhookAuthority, error) {
	if db == nil || redisClient == nil || cipher == nil || strings.TrimSpace(cipher.KeyID()) == "" {
		return nil, ErrInvalid
	}
	return &WorkspaceWebhookAuthority{
		db:       db,
		redis:    redisClient,
		cipher:   cipher,
		resolver: resolver,
		dialer:   dialer,
		client:   trust.NewInspectionHTTPClient(resolver, dialer),
	}, nil
}

func (a *WorkspaceWebhookAuthority) requireWorkspaceManager(ctx context.Context, workspaceID, actorID string) error {
	workspaceID, actorID = strings.TrimSpace(workspaceID), strings.TrimSpace(actorID)
	if workspaceID == "" || actorID == "" {
		return ErrInvalid
	}
	var role string
	err := a.db.QueryRowContext(ctx, `SELECT m.role FROM workspace_memberships m JOIN workspaces w ON w.id=m.workspace_id WHERE m.workspace_id=? AND m.user_id=? AND w.status='active'`, workspaceID, actorID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return err
	}
	if role != "owner" && role != "admin" {
		return ErrForbidden
	}
	return nil
}

func normalizeWorkspaceWebhookEvents(events []string) ([]string, error) {
	if len(events) == 0 || len(events) > 32 {
		return nil, ErrInvalid
	}
	seen := make(map[string]struct{}, len(events))
	out := make([]string, 0, len(events))
	for _, raw := range events {
		event := strings.TrimSpace(raw)
		if !validWorkspaceWebhookEvent(event) {
			return nil, ErrInvalid
		}
		if _, exists := seen[event]; exists {
			continue
		}
		seen[event] = struct{}{}
		out = append(out, event)
	}
	if len(out) == 0 {
		return nil, ErrInvalid
	}
	sort.Strings(out)
	return out, nil
}

func validWorkspaceWebhookEvent(value string) bool {
	if value == "" || len(value) > 96 || strings.Contains(value, "*") {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == ':', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func (a *WorkspaceWebhookAuthority) validateInput(ctx context.Context, input WorkspaceWebhookInput) (WorkspaceWebhookInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.EndpointURL = strings.TrimSpace(input.EndpointURL)
	if input.Name == "" || len(input.Name) > 128 || input.EndpointURL == "" || len(input.EndpointURL) > 2048 {
		return WorkspaceWebhookInput{}, ErrInvalid
	}
	events, err := normalizeWorkspaceWebhookEvents(input.Events)
	if err != nil {
		return WorkspaceWebhookInput{}, err
	}
	target, err := trust.ValidateInspectionTarget(ctx, input.EndpointURL, a.resolver)
	if err != nil {
		return WorkspaceWebhookInput{}, ErrInvalid
	}
	input.EndpointURL = target.CanonicalURL
	input.Events = events
	return input, nil
}

func workspaceWebhookSecretPrefix(secret string) string {
	if len(secret) <= 14 {
		return secret
	}
	return secret[:14]
}

func workspaceWebhookSecretPurpose(workspaceID, webhookID string) string {
	return "workspace-webhook:" + workspaceID + ":" + webhookID
}

func (a *WorkspaceWebhookAuthority) Create(ctx context.Context, workspaceID, actorID string, input WorkspaceWebhookInput, correlationID string, now time.Time) (WorkspaceWebhookSecret, error) {
	if err := a.requireWorkspaceManager(ctx, workspaceID, actorID); err != nil {
		a.auditBestEffort(ctx, workspaceID, actorID, "webhook.create", "", correlationID, "denied", map[string]any{"reason": "workspace_role"}, now)
		return WorkspaceWebhookSecret{}, err
	}
	normalized, err := a.validateInput(ctx, input)
	if err != nil {
		return WorkspaceWebhookSecret{}, err
	}
	id, err := newOpaque("wh_", 18)
	if err != nil {
		return WorkspaceWebhookSecret{}, err
	}
	secret, err := newOpaque(workspaceWebhookSecretPrefix, 32)
	if err != nil {
		return WorkspaceWebhookSecret{}, err
	}
	ciphertext, err := a.cipher.Encrypt(secret, workspaceWebhookSecretPurpose(workspaceID, id))
	if err != nil {
		return WorkspaceWebhookSecret{}, err
	}
	eventsJSON, _ := json.Marshal(normalized.Events)
	_, err = a.db.ExecContext(ctx, `INSERT INTO workspace_webhooks(id,workspace_id,name,endpoint_url,events_json,secret_ciphertext,secret_key_id,secret_prefix,status,created_by,updated_by,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?, 'active',?,?,?,?)`, id, workspaceID, normalized.Name, normalized.EndpointURL, eventsJSON, ciphertext, a.cipher.KeyID(), workspaceWebhookSecretPrefix(secret), actorID, actorID, now.UTC(), now.UTC())
	if err != nil {
		return WorkspaceWebhookSecret{}, err
	}
	webhook, err := a.get(ctx, workspaceID, id)
	if err != nil {
		return WorkspaceWebhookSecret{}, err
	}
	a.auditBestEffort(ctx, workspaceID, actorID, "webhook.create", id, correlationID, "success", map[string]any{"endpoint_fingerprint": webhookEndpointFingerprint(webhook.EndpointURL), "events": webhook.Events}, now)
	return WorkspaceWebhookSecret{Webhook: webhook, Secret: secret}, nil
}

func (a *WorkspaceWebhookAuthority) List(ctx context.Context, workspaceID, actorID string) ([]WorkspaceWebhook, error) {
	if err := a.requireWorkspaceManager(ctx, workspaceID, actorID); err != nil {
		return nil, err
	}
	rows, err := a.db.QueryContext(ctx, `SELECT id,workspace_id,name,endpoint_url,events_json,secret_prefix,status,created_by,updated_by,created_at,updated_at,rotated_at,disabled_at FROM workspace_webhooks WHERE workspace_id=? ORDER BY created_at,id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkspaceWebhook{}
	for rows.Next() {
		item, scanErr := scanWorkspaceWebhook(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (a *WorkspaceWebhookAuthority) Get(ctx context.Context, workspaceID, actorID, webhookID string) (WorkspaceWebhook, error) {
	if err := a.requireWorkspaceManager(ctx, workspaceID, actorID); err != nil {
		return WorkspaceWebhook{}, err
	}
	return a.get(ctx, workspaceID, webhookID)
}

func (a *WorkspaceWebhookAuthority) get(ctx context.Context, workspaceID, webhookID string) (WorkspaceWebhook, error) {
	item, err := scanWorkspaceWebhook(a.db.QueryRowContext(ctx, `SELECT id,workspace_id,name,endpoint_url,events_json,secret_prefix,status,created_by,updated_by,created_at,updated_at,rotated_at,disabled_at FROM workspace_webhooks WHERE workspace_id=? AND id=?`, strings.TrimSpace(workspaceID), strings.TrimSpace(webhookID)))
	if errors.Is(err, sql.ErrNoRows) {
		return WorkspaceWebhook{}, ErrNotFound
	}
	return item, err
}

func (a *WorkspaceWebhookAuthority) RotateSecret(ctx context.Context, workspaceID, actorID, webhookID, correlationID string, now time.Time) (WorkspaceWebhookSecret, error) {
	if err := a.requireWorkspaceManager(ctx, workspaceID, actorID); err != nil {
		a.auditBestEffort(ctx, workspaceID, actorID, "webhook.rotate_secret", webhookID, correlationID, "denied", map[string]any{"reason": "workspace_role"}, now)
		return WorkspaceWebhookSecret{}, err
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return WorkspaceWebhookSecret{}, err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM workspace_webhooks WHERE workspace_id=? AND id=? FOR UPDATE`, workspaceID, webhookID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WorkspaceWebhookSecret{}, ErrNotFound
		}
		return WorkspaceWebhookSecret{}, err
	}
	secret, err := newOpaque(workspaceWebhookSecretPrefix, 32)
	if err != nil {
		return WorkspaceWebhookSecret{}, err
	}
	ciphertext, err := a.cipher.Encrypt(secret, workspaceWebhookSecretPurpose(workspaceID, webhookID))
	if err != nil {
		return WorkspaceWebhookSecret{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workspace_webhooks SET secret_ciphertext=?,secret_key_id=?,secret_prefix=?,updated_by=?,rotated_at=?,updated_at=? WHERE workspace_id=? AND id=?`, ciphertext, a.cipher.KeyID(), workspaceWebhookSecretPrefix(secret), actorID, now.UTC(), now.UTC(), workspaceID, webhookID); err != nil {
		return WorkspaceWebhookSecret{}, err
	}
	if err := tx.Commit(); err != nil {
		return WorkspaceWebhookSecret{}, err
	}
	webhook, err := a.get(ctx, workspaceID, webhookID)
	if err != nil {
		return WorkspaceWebhookSecret{}, err
	}
	a.auditBestEffort(ctx, workspaceID, actorID, "webhook.rotate_secret", webhookID, correlationID, "success", map[string]any{"secret_prefix": webhook.SecretPrefix, "status": status}, now)
	return WorkspaceWebhookSecret{Webhook: webhook, Secret: secret}, nil
}

func (a *WorkspaceWebhookAuthority) Enable(ctx context.Context, workspaceID, actorID, webhookID, correlationID string, now time.Time) (WorkspaceWebhook, error) {
	return a.setEnabled(ctx, workspaceID, actorID, webhookID, correlationID, true, now)
}

func (a *WorkspaceWebhookAuthority) Disable(ctx context.Context, workspaceID, actorID, webhookID, correlationID string, now time.Time) (WorkspaceWebhook, error) {
	return a.setEnabled(ctx, workspaceID, actorID, webhookID, correlationID, false, now)
}

func (a *WorkspaceWebhookAuthority) setEnabled(ctx context.Context, workspaceID, actorID, webhookID, correlationID string, enabled bool, now time.Time) (WorkspaceWebhook, error) {
	action := "webhook.disable"
	status := "disabled"
	var disabledAt any = now.UTC()
	if enabled {
		action = "webhook.enable"
		status = "active"
		disabledAt = nil
	}
	if err := a.requireWorkspaceManager(ctx, workspaceID, actorID); err != nil {
		a.auditBestEffort(ctx, workspaceID, actorID, action, webhookID, correlationID, "denied", map[string]any{"reason": "workspace_role"}, now)
		return WorkspaceWebhook{}, err
	}
	result, err := a.db.ExecContext(ctx, `UPDATE workspace_webhooks SET status=?,disabled_at=?,updated_by=?,updated_at=? WHERE workspace_id=? AND id=?`, status, disabledAt, actorID, now.UTC(), workspaceID, webhookID)
	if err != nil {
		return WorkspaceWebhook{}, err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		var exists int
		if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_webhooks WHERE workspace_id=? AND id=?`, workspaceID, webhookID).Scan(&exists); err != nil {
			return WorkspaceWebhook{}, err
		}
		if exists == 0 {
			return WorkspaceWebhook{}, ErrNotFound
		}
	}
	webhook, err := a.get(ctx, workspaceID, webhookID)
	if err != nil {
		return WorkspaceWebhook{}, err
	}
	a.auditBestEffort(ctx, workspaceID, actorID, action, webhookID, correlationID, "success", map[string]any{"status": webhook.Status}, now)
	return webhook, nil
}

func (a *WorkspaceWebhookAuthority) QueueDelivery(ctx context.Context, workspaceID, webhookID, eventID, eventType string, payload json.RawMessage, correlationID string, now time.Time) (WorkspaceWebhookDelivery, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	webhookID = strings.TrimSpace(webhookID)
	eventID = strings.TrimSpace(eventID)
	eventType = strings.TrimSpace(eventType)
	if workspaceID == "" || webhookID == "" || eventID == "" || len(eventID) > 128 || !validWorkspaceWebhookEvent(eventType) || len(payload) == 0 || len(payload) > workspaceWebhookMaxBodyBytes || !json.Valid(payload) {
		return WorkspaceWebhookDelivery{}, ErrInvalid
	}
	webhook, err := a.get(ctx, workspaceID, webhookID)
	if err != nil {
		return WorkspaceWebhookDelivery{}, err
	}
	if webhook.Status != "active" {
		return WorkspaceWebhookDelivery{}, ErrConflict
	}
	allowed := false
	for _, subscribed := range webhook.Events {
		if subscribed == eventType {
			allowed = true
			break
		}
	}
	if !allowed {
		return WorkspaceWebhookDelivery{}, ErrForbidden
	}
	id, err := newOpaque("whd_", 18)
	if err != nil {
		return WorkspaceWebhookDelivery{}, err
	}
	if strings.TrimSpace(correlationID) == "" {
		correlationID = fmt.Sprintf("p17-webhook-event-%d", now.UTC().UnixNano())
	}
	envelope := struct {
		ID          string          `json:"id"`
		Type        string          `json:"type"`
		WorkspaceID string          `json:"workspace_id"`
		CreatedAt   string          `json:"created_at"`
		Data        json.RawMessage `json:"data"`
	}{ID: eventID, Type: eventType, WorkspaceID: workspaceID, CreatedAt: now.UTC().Format(time.RFC3339Nano), Data: payload}
	body, err := json.Marshal(envelope)
	if err != nil || len(body) > workspaceWebhookMaxBodyBytes {
		return WorkspaceWebhookDelivery{}, ErrInvalid
	}
	digest := sha256.Sum256(body)
	_, err = a.db.ExecContext(ctx, `INSERT INTO workspace_webhook_deliveries(id,workspace_id,webhook_id,event_id,event_type,body,body_sha256,status,attempts,next_attempt_at,last_error_code,request_correlation_id,created_at,updated_at) VALUES (?,?,?,?,?,?,?,'retrying',0,?,'',?,?,?) ON DUPLICATE KEY UPDATE id=id`, id, workspaceID, webhookID, eventID, eventType, body, digest[:], now.UTC(), correlationID, now.UTC(), now.UTC())
	if err != nil {
		return WorkspaceWebhookDelivery{}, err
	}
	return a.getDeliveryByEvent(ctx, workspaceID, webhookID, eventID)
}

func (a *WorkspaceWebhookAuthority) ListDeliveries(ctx context.Context, workspaceID, actorID, webhookID string) ([]WorkspaceWebhookDelivery, error) {
	if err := a.requireWorkspaceManager(ctx, workspaceID, actorID); err != nil {
		return nil, err
	}
	if _, err := a.get(ctx, workspaceID, webhookID); err != nil {
		return nil, err
	}
	rows, err := a.db.QueryContext(ctx, `SELECT id,workspace_id,webhook_id,event_id,event_type,body_sha256,status,attempts,next_attempt_at,last_attempt_at,last_status_code,last_error_code,created_at,updated_at,delivered_at FROM workspace_webhook_deliveries WHERE workspace_id=? AND webhook_id=? ORDER BY created_at DESC,id DESC LIMIT 100`, workspaceID, webhookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkspaceWebhookDelivery{}
	for rows.Next() {
		item, scanErr := scanWorkspaceWebhookDelivery(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (a *WorkspaceWebhookAuthority) RetryDelivery(ctx context.Context, workspaceID, actorID, webhookID, deliveryID, correlationID string, now time.Time) (WorkspaceWebhookDelivery, error) {
	if err := a.requireWorkspaceManager(ctx, workspaceID, actorID); err != nil {
		a.auditBestEffort(ctx, workspaceID, actorID, "webhook.delivery.retry", deliveryID, correlationID, "denied", map[string]any{"reason": "workspace_role"}, now)
		return WorkspaceWebhookDelivery{}, err
	}
	webhook, err := a.get(ctx, workspaceID, webhookID)
	if err != nil {
		return WorkspaceWebhookDelivery{}, err
	}
	if webhook.Status != "active" {
		return WorkspaceWebhookDelivery{}, ErrConflict
	}
	result, err := a.db.ExecContext(ctx, `UPDATE workspace_webhook_deliveries SET status='retrying',next_attempt_at=?,last_error_code='',updated_at=? WHERE workspace_id=? AND webhook_id=? AND id=? AND status='failed'`, now.UTC(), now.UTC(), workspaceID, webhookID, deliveryID)
	if err != nil {
		return WorkspaceWebhookDelivery{}, err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		item, getErr := a.getDelivery(ctx, workspaceID, webhookID, deliveryID)
		if getErr != nil {
			return WorkspaceWebhookDelivery{}, getErr
		}
		if item.Status != "failed" {
			return WorkspaceWebhookDelivery{}, ErrConflict
		}
	}
	item, err := a.getDelivery(ctx, workspaceID, webhookID, deliveryID)
	if err != nil {
		return WorkspaceWebhookDelivery{}, err
	}
	a.auditBestEffort(ctx, workspaceID, actorID, "webhook.delivery.retry", deliveryID, correlationID, "success", map[string]any{"webhook_id": webhookID, "attempts": item.Attempts}, now)
	return item, nil
}

func (a *WorkspaceWebhookAuthority) RunDeliveryOnce(ctx context.Context, now time.Time) (bool, error) {
	row, err := a.nextDueDelivery(ctx, now)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	leaseToken, err := newOpaque("whlease_", 18)
	if err != nil {
		return false, err
	}
	leaseKey := workspaceWebhookLeasePrefix + row.Delivery.ID
	leased, err := a.redis.SetNX(ctx, leaseKey, leaseToken, 2*time.Minute).Result()
	if err != nil {
		return false, err
	}
	if !leased {
		return false, nil
	}
	defer func() {
		_, _ = a.redis.Eval(context.Background(), `if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) else return 0 end`, []string{leaseKey}, leaseToken).Result()
	}()

	row, err = a.loadWorkerDelivery(ctx, row.Delivery.ID)
	if err != nil {
		return true, err
	}
	if row.WebhookStatus != "active" || row.Delivery.Status != "retrying" || row.Delivery.NextAttemptAt.After(now.UTC()) {
		return false, nil
	}
	secret, err := a.cipher.Decrypt(row.SecretCipher, row.SecretKeyID, workspaceWebhookSecretPurpose(row.Delivery.WorkspaceID, row.Delivery.WebhookID))
	if err != nil {
		markErr := a.markDeliveryFailure(ctx, row, "secret_unavailable", 0, true, now)
		if markErr != nil {
			return true, markErr
		}
		return true, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, row.EndpointURL, bytes.NewReader(row.Body))
	if err != nil {
		_ = a.markDeliveryFailure(ctx, row, "invalid_endpoint", 0, true, now)
		return true, err
	}
	timestamp := now.UTC().Truncate(time.Second)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "GoJet-OperationsMonitor/1.0")
	request.Header.Set("X-GoJet-Delivery", row.Delivery.ID)
	request.Header.Set("X-GoJet-Idempotency-Key", row.Delivery.ID)
	request.Header.Set("X-GoJet-Event", row.Delivery.EventType)
	request.Header.Set("X-GoJet-Timestamp", fmt.Sprintf("%d", timestamp.Unix()))
	request.Header.Set("X-GoJet-Signature", SignWorkspaceWebhookDelivery(secret, row.Delivery.ID, timestamp, row.Body))

	resp, deliveryErr := a.client.Do(request)
	if deliveryErr != nil {
		permanent := errors.Is(deliveryErr, trust.ErrUnsafeInspectionTarget) || errors.Is(deliveryErr, trust.ErrUnsafeInspectionAddress)
		code := "delivery_unavailable"
		if permanent {
			code = "unsafe_destination"
		} else if errors.Is(deliveryErr, trust.ErrInspectionResolution) {
			code = "dns_unavailable"
		}
		if err := a.markDeliveryFailure(ctx, row, code, 0, permanent, now); err != nil {
			return true, err
		}
		return true, deliveryErr
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := a.markDeliverySuccess(ctx, row, resp.StatusCode, now); err != nil {
			return true, err
		}
		return true, nil
	}
	if err := a.markDeliveryFailure(ctx, row, fmt.Sprintf("http_%d", resp.StatusCode), resp.StatusCode, false, now); err != nil {
		return true, err
	}
	return true, fmt.Errorf("webhook delivery returned HTTP %d", resp.StatusCode)
}

func SignWorkspaceWebhookDelivery(secret, deliveryID string, timestamp time.Time, body []byte) string {
	message := deliveryID + "\n" + fmt.Sprintf("%d", timestamp.UTC().Unix()) + "\n" + string(body)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func VerifyWorkspaceWebhookDeliverySignature(secret, deliveryID string, timestamp time.Time, body []byte, signature string) bool {
	expected := SignWorkspaceWebhookDelivery(secret, deliveryID, timestamp, body)
	return hmac.Equal([]byte(expected), []byte(strings.TrimSpace(signature)))
}

func (a *WorkspaceWebhookAuthority) nextDueDelivery(ctx context.Context, now time.Time) (webhookWorkerDelivery, error) {
	var id string
	err := a.db.QueryRowContext(ctx, `SELECT d.id FROM workspace_webhook_deliveries d JOIN workspace_webhooks w ON w.id=d.webhook_id AND w.workspace_id=d.workspace_id WHERE d.status='retrying' AND d.next_attempt_at<=? AND w.status='active' ORDER BY d.next_attempt_at,d.id LIMIT 1`, now.UTC()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return webhookWorkerDelivery{}, ErrNotFound
	}
	if err != nil {
		return webhookWorkerDelivery{}, err
	}
	return a.loadWorkerDelivery(ctx, id)
}

func (a *WorkspaceWebhookAuthority) loadWorkerDelivery(ctx context.Context, deliveryID string) (webhookWorkerDelivery, error) {
	var row webhookWorkerDelivery
	var bodyHash []byte
	var lastAttempt, delivered sql.NullTime
	var lastStatus sql.NullInt64
	err := a.db.QueryRowContext(ctx, `SELECT d.id,d.workspace_id,d.webhook_id,d.event_id,d.event_type,d.body,d.body_sha256,d.status,d.attempts,d.next_attempt_at,d.last_attempt_at,d.last_status_code,d.last_error_code,d.created_at,d.updated_at,d.delivered_at,w.endpoint_url,w.secret_ciphertext,w.secret_key_id,w.status FROM workspace_webhook_deliveries d JOIN workspace_webhooks w ON w.id=d.webhook_id AND w.workspace_id=d.workspace_id WHERE d.id=?`, deliveryID).Scan(&row.Delivery.ID, &row.Delivery.WorkspaceID, &row.Delivery.WebhookID, &row.Delivery.EventID, &row.Delivery.EventType, &row.Body, &bodyHash, &row.Delivery.Status, &row.Delivery.Attempts, &row.Delivery.NextAttemptAt, &lastAttempt, &lastStatus, &row.Delivery.LastErrorCode, &row.Delivery.CreatedAt, &row.Delivery.UpdatedAt, &delivered, &row.EndpointURL, &row.SecretCipher, &row.SecretKeyID, &row.WebhookStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return webhookWorkerDelivery{}, ErrNotFound
	}
	if err != nil {
		return webhookWorkerDelivery{}, err
	}
	row.Delivery.BodySHA256 = hex.EncodeToString(bodyHash)
	if lastAttempt.Valid {
		value := lastAttempt.Time.UTC()
		row.Delivery.LastAttemptAt = &value
	}
	if lastStatus.Valid {
		value := int(lastStatus.Int64)
		row.Delivery.LastStatusCode = &value
	}
	if delivered.Valid {
		value := delivered.Time.UTC()
		row.Delivery.DeliveredAt = &value
	}
	return row, nil
}

func (a *WorkspaceWebhookAuthority) markDeliverySuccess(ctx context.Context, row webhookWorkerDelivery, statusCode int, now time.Time) error {
	attempts := row.Delivery.Attempts + 1
	result, err := a.db.ExecContext(ctx, `UPDATE workspace_webhook_deliveries SET status='delivered',attempts=?,last_attempt_at=?,last_status_code=?,last_error_code='',delivered_at=?,updated_at=? WHERE id=? AND status='retrying'`, attempts, now.UTC(), statusCode, now.UTC(), now.UTC(), row.Delivery.ID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	a.auditBestEffort(ctx, row.Delivery.WorkspaceID, "operationsmonitor", "webhook.delivery.delivered", row.Delivery.ID, "operationsmonitor-"+row.Delivery.ID, "success", map[string]any{"webhook_id": row.Delivery.WebhookID, "event_type": row.Delivery.EventType, "attempts": attempts, "status_code": statusCode}, now)
	return nil
}

func (a *WorkspaceWebhookAuthority) markDeliveryFailure(ctx context.Context, row webhookWorkerDelivery, errorCode string, statusCode int, permanent bool, now time.Time) error {
	attempts := row.Delivery.Attempts + 1
	status := "retrying"
	nextAttempt := now.UTC().Add(workspaceWebhookRetryDelay(attempts))
	if permanent || attempts >= workspaceWebhookMaxAttempts {
		status = "failed"
		nextAttempt = now.UTC()
	}
	var statusValue any
	if statusCode > 0 {
		statusValue = statusCode
	}
	result, err := a.db.ExecContext(ctx, `UPDATE workspace_webhook_deliveries SET status=?,attempts=?,next_attempt_at=?,last_attempt_at=?,last_status_code=?,last_error_code=?,updated_at=? WHERE id=? AND status='retrying'`, status, attempts, nextAttempt, now.UTC(), statusValue, errorCode, now.UTC(), row.Delivery.ID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	resultName := "failed"
	if status == "retrying" {
		resultName = "conflict"
	}
	a.auditBestEffort(ctx, row.Delivery.WorkspaceID, "operationsmonitor", "webhook.delivery."+status, row.Delivery.ID, "operationsmonitor-"+row.Delivery.ID, resultName, map[string]any{"webhook_id": row.Delivery.WebhookID, "event_type": row.Delivery.EventType, "attempts": attempts, "error_code": errorCode}, now)
	return nil
}

func workspaceWebhookRetryDelay(attempt int) time.Duration {
	switch attempt {
	case 1:
		return time.Minute
	case 2:
		return 5 * time.Minute
	case 3:
		return 15 * time.Minute
	default:
		return time.Hour
	}
}

func (a *WorkspaceWebhookAuthority) getDeliveryByEvent(ctx context.Context, workspaceID, webhookID, eventID string) (WorkspaceWebhookDelivery, error) {
	return scanWorkspaceWebhookDelivery(a.db.QueryRowContext(ctx, `SELECT id,workspace_id,webhook_id,event_id,event_type,body_sha256,status,attempts,next_attempt_at,last_attempt_at,last_status_code,last_error_code,created_at,updated_at,delivered_at FROM workspace_webhook_deliveries WHERE workspace_id=? AND webhook_id=? AND event_id=?`, workspaceID, webhookID, eventID))
}

func (a *WorkspaceWebhookAuthority) getDelivery(ctx context.Context, workspaceID, webhookID, deliveryID string) (WorkspaceWebhookDelivery, error) {
	item, err := scanWorkspaceWebhookDelivery(a.db.QueryRowContext(ctx, `SELECT id,workspace_id,webhook_id,event_id,event_type,body_sha256,status,attempts,next_attempt_at,last_attempt_at,last_status_code,last_error_code,created_at,updated_at,delivered_at FROM workspace_webhook_deliveries WHERE workspace_id=? AND webhook_id=? AND id=?`, workspaceID, webhookID, deliveryID))
	if errors.Is(err, sql.ErrNoRows) {
		return WorkspaceWebhookDelivery{}, ErrNotFound
	}
	return item, err
}

type workspaceWebhookRowScanner interface{ Scan(dest ...any) error }

func scanWorkspaceWebhook(row workspaceWebhookRowScanner) (WorkspaceWebhook, error) {
	var item WorkspaceWebhook
	var events []byte
	var rotated, disabled sql.NullTime
	if err := row.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.EndpointURL, &events, &item.SecretPrefix, &item.Status, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt, &rotated, &disabled); err != nil {
		return WorkspaceWebhook{}, err
	}
	if err := json.Unmarshal(events, &item.Events); err != nil {
		return WorkspaceWebhook{}, err
	}
	if rotated.Valid {
		value := rotated.Time.UTC()
		item.RotatedAt = &value
	}
	if disabled.Valid {
		value := disabled.Time.UTC()
		item.DisabledAt = &value
	}
	return item, nil
}

func scanWorkspaceWebhookDelivery(row workspaceWebhookRowScanner) (WorkspaceWebhookDelivery, error) {
	var item WorkspaceWebhookDelivery
	var hash []byte
	var lastAttempt, delivered sql.NullTime
	var lastStatus sql.NullInt64
	if err := row.Scan(&item.ID, &item.WorkspaceID, &item.WebhookID, &item.EventID, &item.EventType, &hash, &item.Status, &item.Attempts, &item.NextAttemptAt, &lastAttempt, &lastStatus, &item.LastErrorCode, &item.CreatedAt, &item.UpdatedAt, &delivered); err != nil {
		return WorkspaceWebhookDelivery{}, err
	}
	item.BodySHA256 = hex.EncodeToString(hash)
	if lastAttempt.Valid {
		value := lastAttempt.Time.UTC()
		item.LastAttemptAt = &value
	}
	if lastStatus.Valid {
		value := int(lastStatus.Int64)
		item.LastStatusCode = &value
	}
	if delivered.Valid {
		value := delivered.Time.UTC()
		item.DeliveredAt = &value
	}
	return item, nil
}

func webhookEndpointFingerprint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return "invalid"
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	digest := sha256.Sum256([]byte(host))
	return hex.EncodeToString(digest[:8])
}

func (a *WorkspaceWebhookAuthority) auditBestEffort(ctx context.Context, workspaceID, actorID, action, resourceID, correlationID, result string, metadata map[string]any, now time.Time) {
	workspaceID, actorID = strings.TrimSpace(workspaceID), strings.TrimSpace(actorID)
	if workspaceID == "" || actorID == "" {
		return
	}
	if strings.TrimSpace(correlationID) == "" {
		correlationID = fmt.Sprintf("p17-webhook-%d", now.UTC().UnixNano())
	}
	raw, _ := json.Marshal(metadata)
	_, _ = a.db.ExecContext(ctx, `INSERT INTO workspace_audit_events(workspace_id,actor_id,action,resource_type,resource_id,reason,request_correlation_id,result,metadata_json,created_at) SELECT ?,?,?,?,?,NULL,?,?,?,? FROM workspaces WHERE id=?`, workspaceID, actorID, action, "webhook", resourceID, correlationID, result, raw, now.UTC(), workspaceID)
}
