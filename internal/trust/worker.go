package trust

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const OperationsMonitorServiceID = "SVC-OPS-MONITOR"

type DestinationScanProcessor interface {
	Process(context.Context, DestinationScan, []ScanTarget) error
}

type RiskWorker struct {
	Store     *Store
	Processor DestinationScanProcessor
	WorkerID  string
	LeaseTTL  time.Duration
	RetryBase time.Duration
	Now       func() time.Time
}

func NewRiskWorker(store *Store, processor DestinationScanProcessor, workerID string) (*RiskWorker, error) {
	workerID = strings.TrimSpace(workerID)
	if store == nil || processor == nil || workerID == "" || len(workerID) > 128 {
		return nil, ErrInvalid
	}
	return &RiskWorker{
		Store:     store,
		Processor: processor,
		WorkerID:  workerID,
		LeaseTTL:  2 * time.Minute,
		RetryBase: 2 * time.Second,
		Now:       time.Now,
	}, nil
}

func (w *RiskWorker) RunOnce(ctx context.Context) (bool, error) {
	if w == nil || w.Store == nil || w.Processor == nil || strings.TrimSpace(w.WorkerID) == "" || w.Now == nil || w.LeaseTTL < time.Second || w.RetryBase < 0 {
		return false, ErrInvalid
	}
	now := w.Now().UTC().Truncate(time.Microsecond)
	leased, err := w.Store.LeaseDestinationScan(ctx, w.WorkerID, now, w.LeaseTTL)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	processErr := w.Processor.Process(ctx, leased.Scan, leased.Targets)
	state, stateErr := w.Store.GetDestinationScanState(ctx, leased.Scan.WorkspaceID, leased.Scan.ID)
	if stateErr != nil {
		return true, stateErr
	}
	if state.Status == ScanStatusCompleted {
		return true, nil
	}

	if processErr == nil {
		processErr = errors.New("processor returned without durable completed authority")
	}
	errorCode := workerErrorCode(processErr)
	delay := retryDelay(w.RetryBase, leased.Scan.Attempts)
	updated, releaseErr := w.Store.ReleaseDestinationScanForRetry(ctx, leased.Scan.WorkspaceID, leased.Scan.ID, w.WorkerID, errorCode, w.Now(), delay)
	if releaseErr != nil {
		return true, releaseErr
	}
	if updated.Status == ScanStatusFailed {
		return true, fmt.Errorf("destination risk scan %d exhausted retries: %s", leased.Scan.ID, errorCode)
	}
	return true, processErr
}

func retryDelay(base time.Duration, attempt uint32) time.Duration {
	if base <= 0 {
		return 0
	}
	shift := attempt
	if shift > 5 {
		shift = 5
	}
	delay := base * time.Duration(1<<shift)
	if delay > time.Minute {
		return time.Minute
	}
	return delay
}

func workerErrorCode(err error) string {
	if err == nil {
		return "processor-incomplete"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "processor-timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "processor-canceled"
	}
	return "processor-error"
}

type ProviderPolicyProcessor struct {
	Store             *Store
	Provider          SemanticProviderClient
	Policy            DestinationPolicy
	ActorID           string
	LocalSafetyPassed bool
}

func (p *ProviderPolicyProcessor) Process(ctx context.Context, scan DestinationScan, targets []ScanTarget) error {
	if p == nil || p.Store == nil || !p.Policy.Validate() || strings.TrimSpace(p.ActorID) == "" || p.Provider.HTTPClient == nil || len(targets) == 0 {
		return ErrInvalid
	}
	if scan.PolicyVersion != strings.TrimSpace(p.Policy.Version) {
		return ErrPolicyMismatch
	}

	existing, err := p.Store.GetProviderObservations(ctx, scan.WorkspaceID, scan.ID)
	if err != nil {
		return err
	}
	providerName := strings.TrimSpace(p.Provider.Name)
	for _, observation := range existing {
		if observation.Provider == providerName {
			_, err := p.Store.FinalizeDestinationDecision(ctx, FinalizeDestinationDecisionInput{
				WorkspaceID:       scan.WorkspaceID,
				ScanID:            scan.ID,
				Policy:            p.Policy,
				LocalSafetyPassed: p.LocalSafetyPassed,
				ActorID:           p.ActorID,
				CorrelationID:     scan.CorrelationID,
			})
			return err
		}
	}

	aggregated := ProviderObservation{
		Provider:   providerName,
		Outcome:    ProviderAllow,
		SignalCode: "aggregate-allow",
		Evidence: map[string]any{
			"target_count": len(targets),
		},
		ObservedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	counts := map[ProviderOutcome]int{}
	for _, target := range targets {
		observation, err := p.Provider.Observe(ctx, target.NormalizedURL)
		if err != nil {
			return err
		}
		counts[observation.Outcome]++
		if providerOutcomeSeverity(observation.Outcome) > providerOutcomeSeverity(aggregated.Outcome) {
			aggregated.Outcome = observation.Outcome
		}
	}
	aggregated.SignalCode = "aggregate-" + string(aggregated.Outcome)
	aggregated.Evidence["allow_count"] = counts[ProviderAllow]
	aggregated.Evidence["review_count"] = counts[ProviderReview]
	aggregated.Evidence["block_count"] = counts[ProviderBlock]
	aggregated.Evidence["unknown_count"] = counts[ProviderUnknown]
	aggregated.Evidence["unavailable_count"] = counts[ProviderUnavailable]

	if _, err := p.Store.RecordProviderObservation(ctx, RecordProviderObservationInput{
		WorkspaceID:   scan.WorkspaceID,
		ScanID:        scan.ID,
		Observation:   aggregated,
		ActorID:       p.ActorID,
		CorrelationID: scan.CorrelationID,
	}); err != nil {
		return err
	}
	_, err = p.Store.FinalizeDestinationDecision(ctx, FinalizeDestinationDecisionInput{
		WorkspaceID:       scan.WorkspaceID,
		ScanID:            scan.ID,
		Policy:            p.Policy,
		LocalSafetyPassed: p.LocalSafetyPassed,
		ActorID:           p.ActorID,
		CorrelationID:     scan.CorrelationID,
	})
	return err
}

func providerOutcomeSeverity(outcome ProviderOutcome) int {
	switch outcome {
	case ProviderBlock:
		return 5
	case ProviderReview:
		return 4
	case ProviderUnknown:
		return 3
	case ProviderUnavailable:
		return 2
	case ProviderAllow:
		return 1
	default:
		return 6
	}
}
