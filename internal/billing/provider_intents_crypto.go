package billing

import (
	"context"
	"regexp"
	"time"
)

const (
	cryptoSettlementAsset = "USDTTRC20"
	cryptoSettlementScale = uint8(6)
)

var cryptoProviderReferencePattern = regexp.MustCompile(`^41[0-9a-f]{40}$`)

// ResolveCryptoProviderIntentByProviderReference is the bounded read-only seam
// for P20-D016 Crypto settlement. Crypto token quote/address creation remains
// outside the generic fiat CreateProviderIntent/BindProviderReference paths.
func (s *Store) ResolveCryptoProviderIntentByProviderReference(ctx context.Context, providerReference string, now time.Time) (ProviderIntent, error) {
	if s == nil || s.db == nil || !canonicalCryptoProviderReference(providerReference) || now.IsZero() {
		return ProviderIntent{}, ErrInvalidInput
	}
	intent, err := loadProviderIntentByReference(ctx, s.db, ProviderCrypto, "provider_reference", providerReference)
	if err != nil {
		return ProviderIntent{}, err
	}
	if !providerIntentUsable(intent, now.UTC()) || intent.Provider != ProviderCrypto || intent.ProviderReference != providerReference || intent.SettlementAssetKind != SettlementAssetToken || intent.SettlementAsset != cryptoSettlementAsset || intent.SettlementScale == nil || *intent.SettlementScale != cryptoSettlementScale || intent.SettlementAmountUnits <= 0 {
		return ProviderIntent{}, ErrConflict
	}
	return intent, nil
}

func canonicalCryptoProviderReference(value string) bool {
	return cryptoProviderReferencePattern.MatchString(value)
}
