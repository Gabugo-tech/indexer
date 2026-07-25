package transform

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/miguelnietoa/stellar-explorer/indexer/internal/store"
)

const (
	claimantAddr  = "GBKPF4URAGUGPBFKQNMDDD4IY5BRRXRK2VEBULJEMVULCCODND436NIO"
	sponsoredAddr = "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"
)

func testBalanceId(t *testing.T) xdr.ClaimableBalanceId {
	t.Helper()
	var hash xdr.Hash
	for i := range hash {
		hash[i] = 0xab
	}
	return xdr.ClaimableBalanceId{
		Type: xdr.ClaimableBalanceIdTypeClaimableBalanceIdTypeV0,
		V0:   &hash,
	}
}

func TestExtractOperationDetails_CreateClaimableBalance(t *testing.T) {
	absBefore := xdr.Int64(1700000000)
	op := decodedOperation(t, xdr.OperationTypeCreateClaimableBalance, xdr.CreateClaimableBalanceOp{
		Asset:  usdcIssuedAsset(t),
		Amount: 5000000,
		Claimants: []xdr.Claimant{
			{
				Type: xdr.ClaimantTypeClaimantTypeV0,
				V0: &xdr.ClaimantV0{
					Destination: xdr.MustAddress(claimantAddr),
					Predicate: xdr.ClaimPredicate{
						Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional,
					},
				},
			},
			{
				Type: xdr.ClaimantTypeClaimantTypeV0,
				V0: &xdr.ClaimantV0{
					Destination: xdr.MustAddress(sponsoredAddr),
					Predicate: xdr.ClaimPredicate{
						Type:      xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime,
						AbsBefore: &absBefore,
					},
				},
			},
		},
	})

	details := extractOperationDetails(op)

	want := map[string]interface{}{
		"type":   "create_claimable_balance",
		"asset":  "USDC:GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
		"amount": "5000000",
	}
	for k, v := range want {
		if got := details[k]; got != v {
			t.Errorf("details[%q] = %v, want %v", k, got, v)
		}
	}

	claimants, ok := details["claimants"].([]map[string]interface{})
	if !ok {
		t.Fatalf("details[\"claimants\"] has type %T, want []map[string]interface{}", details["claimants"])
	}
	if len(claimants) != 2 {
		t.Fatalf("len(claimants) = %d, want 2", len(claimants))
	}
	if got := claimants[0]["destination"]; got != claimantAddr {
		t.Errorf("claimants[0].destination = %v, want %v", got, claimantAddr)
	}
	pred0, ok := claimants[0]["predicate"].(map[string]interface{})
	if !ok || pred0["unconditional"] != true {
		t.Errorf("claimants[0].predicate = %v, want unconditional", claimants[0]["predicate"])
	}
	pred1, ok := claimants[1]["predicate"].(map[string]interface{})
	if !ok || pred1["abs_before"] != "1700000000" {
		t.Errorf("claimants[1].predicate = %v, want abs_before 1700000000", claimants[1]["predicate"])
	}

	storeOp := &store.Operation{}
	enrichOperation(storeOp, op, details)
	if storeOp.Amount == nil || *storeOp.Amount != "5000000" {
		t.Errorf("enriched Amount = %v, want 5000000", storeOp.Amount)
	}
	if storeOp.AssetCode == nil || *storeOp.AssetCode != "USDC" {
		t.Errorf("enriched AssetCode = %v, want USDC", storeOp.AssetCode)
	}
	if storeOp.AssetIssuer == nil || *storeOp.AssetIssuer != sponsoredAddr {
		t.Errorf("enriched AssetIssuer = %v, want %v", storeOp.AssetIssuer, sponsoredAddr)
	}
}

func TestExtractOperationDetails_ClaimClaimableBalance(t *testing.T) {
	op := decodedOperation(t, xdr.OperationTypeClaimClaimableBalance, xdr.ClaimClaimableBalanceOp{
		BalanceId: testBalanceId(t),
	})

	details := extractOperationDetails(op)

	if got := details["type"]; got != "claim_claimable_balance" {
		t.Errorf("details[\"type\"] = %v, want claim_claimable_balance", got)
	}
	wantId := "00000000" + strings.Repeat("ab", 32)
	if got := details["balance_id"]; got != wantId {
		t.Errorf("details[\"balance_id\"] = %v, want %v", got, wantId)
	}

	storeOp := &store.Operation{}
	enrichOperation(storeOp, op, details)
	if storeOp.Amount != nil {
		t.Errorf("enriched Amount = %v, want nil (no promoted columns for this type)", storeOp.Amount)
	}
}

func TestExtractOperationDetails_BeginSponsoringFutureReserves(t *testing.T) {
	op := decodedOperation(t, xdr.OperationTypeBeginSponsoringFutureReserves, xdr.BeginSponsoringFutureReservesOp{
		SponsoredId: xdr.MustAddress(sponsoredAddr),
	})

	details := extractOperationDetails(op)

	if got := details["type"]; got != "begin_sponsoring_future_reserves" {
		t.Errorf("details[\"type\"] = %v, want begin_sponsoring_future_reserves", got)
	}
	if got := details["sponsored_id"]; got != sponsoredAddr {
		t.Errorf("details[\"sponsored_id\"] = %v, want %v", got, sponsoredAddr)
	}
}

func TestExtractOperationDetails_EndSponsoringFutureReserves(t *testing.T) {
	op := decodedOperation(t, xdr.OperationTypeEndSponsoringFutureReserves, nil)

	details := extractOperationDetails(op)

	if got := details["type"]; got != "end_sponsoring_future_reserves" {
		t.Errorf("details[\"type\"] = %v, want end_sponsoring_future_reserves", got)
	}
	if len(details) != 1 {
		t.Errorf("details has %d entries (%v), want only the type entry", len(details), details)
	}
}

func TestExtractOperationDetails_RevokeSponsorship_Account(t *testing.T) {
	op := decodedOperation(t, xdr.OperationTypeRevokeSponsorship, xdr.RevokeSponsorshipOp{
		Type: xdr.RevokeSponsorshipTypeRevokeSponsorshipLedgerEntry,
		LedgerKey: &xdr.LedgerKey{
			Type: xdr.LedgerEntryTypeAccount,
			Account: &xdr.LedgerKeyAccount{
				AccountId: xdr.MustAddress(sponsoredAddr),
			},
		},
	})

	details := extractOperationDetails(op)

	want := map[string]interface{}{
		"type":             "revoke_sponsorship",
		"sponsorship_type": "account",
		"account_id":       sponsoredAddr,
	}
	for k, v := range want {
		if got := details[k]; got != v {
			t.Errorf("details[%q] = %v, want %v", k, got, v)
		}
	}
}

func TestExtractOperationDetails_RevokeSponsorship_ClaimableBalance(t *testing.T) {
	op := decodedOperation(t, xdr.OperationTypeRevokeSponsorship, xdr.RevokeSponsorshipOp{
		Type: xdr.RevokeSponsorshipTypeRevokeSponsorshipLedgerEntry,
		LedgerKey: &xdr.LedgerKey{
			Type: xdr.LedgerEntryTypeClaimableBalance,
			ClaimableBalance: &xdr.LedgerKeyClaimableBalance{
				BalanceId: testBalanceId(t),
			},
		},
	})

	details := extractOperationDetails(op)

	want := map[string]interface{}{
		"type":                 "revoke_sponsorship",
		"sponsorship_type":     "claimable_balance",
		"claimable_balance_id": "00000000" + strings.Repeat("ab", 32),
	}
	for k, v := range want {
		if got := details[k]; got != v {
			t.Errorf("details[%q] = %v, want %v", k, got, v)
		}
	}
}

func TestExtractOperationDetails_CreateClaimableBalance_CompositePredicates(t *testing.T) {
	absBefore := xdr.Int64(1700000000)
	relBefore := xdr.Int64(86400)
	unconditional := xdr.ClaimPredicate{Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional}
	notUnconditional := &unconditional
	var nilPredicate *xdr.ClaimPredicate

	claimant := func(pred xdr.ClaimPredicate) xdr.Claimant {
		return xdr.Claimant{
			Type: xdr.ClaimantTypeClaimantTypeV0,
			V0: &xdr.ClaimantV0{
				Destination: xdr.MustAddress(claimantAddr),
				Predicate:   pred,
			},
		}
	}

	op := decodedOperation(t, xdr.OperationTypeCreateClaimableBalance, xdr.CreateClaimableBalanceOp{
		Asset:  nativeXDRAsset(),
		Amount: 1,
		Claimants: []xdr.Claimant{
			claimant(xdr.ClaimPredicate{
				Type: xdr.ClaimPredicateTypeClaimPredicateAnd,
				AndPredicates: &[]xdr.ClaimPredicate{
					{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime, AbsBefore: &absBefore},
					{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeRelativeTime, RelBefore: &relBefore},
				},
			}),
			claimant(xdr.ClaimPredicate{
				Type: xdr.ClaimPredicateTypeClaimPredicateOr,
				OrPredicates: &[]xdr.ClaimPredicate{
					{Type: xdr.ClaimPredicateTypeClaimPredicateUnconditional},
					{Type: xdr.ClaimPredicateTypeClaimPredicateBeforeAbsoluteTime, AbsBefore: &absBefore},
				},
			}),
			claimant(xdr.ClaimPredicate{
				Type:         xdr.ClaimPredicateTypeClaimPredicateNot,
				NotPredicate: &notUnconditional,
			}),
			claimant(xdr.ClaimPredicate{
				Type:         xdr.ClaimPredicateTypeClaimPredicateNot,
				NotPredicate: &nilPredicate,
			}),
		},
	})

	details := extractOperationDetails(op)

	claimants, ok := details["claimants"].([]map[string]interface{})
	if !ok || len(claimants) != 4 {
		t.Fatalf("claimants = %v, want 4 entries", details["claimants"])
	}
	predicate := func(i int) map[string]interface{} {
		t.Helper()
		p, ok := claimants[i]["predicate"].(map[string]interface{})
		if !ok {
			t.Fatalf("claimants[%d].predicate has type %T, want map", i, claimants[i]["predicate"])
		}
		return p
	}

	and, ok := predicate(0)["and"].([]map[string]interface{})
	if !ok || len(and) != 2 {
		t.Fatalf("predicate(0) = %v, want and with 2 sub-predicates", predicate(0))
	}
	if and[0]["abs_before"] != "1700000000" {
		t.Errorf("and[0] = %v, want abs_before 1700000000", and[0])
	}
	if and[1]["rel_before"] != "86400" {
		t.Errorf("and[1] = %v, want rel_before 86400", and[1])
	}

	or, ok := predicate(1)["or"].([]map[string]interface{})
	if !ok || len(or) != 2 {
		t.Fatalf("predicate(1) = %v, want or with 2 sub-predicates", predicate(1))
	}
	if or[0]["unconditional"] != true {
		t.Errorf("or[0] = %v, want unconditional", or[0])
	}
	if or[1]["abs_before"] != "1700000000" {
		t.Errorf("or[1] = %v, want abs_before 1700000000", or[1])
	}

	not, ok := predicate(2)["not"].(map[string]interface{})
	if !ok || not["unconditional"] != true {
		t.Errorf("predicate(2) = %v, want not(unconditional)", predicate(2))
	}

	nullNot, present := predicate(3)["not"]
	if !present || nullNot != nil {
		t.Errorf("predicate(3) = %v, want not with a null arm", predicate(3))
	}
}

func TestExtractOperationDetails_RevokeSponsorship_Trustline(t *testing.T) {
	usdc := usdcIssuedAsset(t)
	op := decodedOperation(t, xdr.OperationTypeRevokeSponsorship, xdr.RevokeSponsorshipOp{
		Type: xdr.RevokeSponsorshipTypeRevokeSponsorshipLedgerEntry,
		LedgerKey: &xdr.LedgerKey{
			Type: xdr.LedgerEntryTypeTrustline,
			TrustLine: &xdr.LedgerKeyTrustLine{
				AccountId: xdr.MustAddress(claimantAddr),
				Asset: xdr.TrustLineAsset{
					Type:      xdr.AssetTypeAssetTypeCreditAlphanum4,
					AlphaNum4: usdc.AlphaNum4,
				},
			},
		},
	})

	details := extractOperationDetails(op)

	want := map[string]interface{}{
		"type":                 "revoke_sponsorship",
		"sponsorship_type":     "trustline",
		"trustline_account_id": claimantAddr,
		"trustline_asset":      "USDC:GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
	}
	for k, v := range want {
		if got := details[k]; got != v {
			t.Errorf("details[%q] = %v, want %v", k, got, v)
		}
	}
}

func TestExtractOperationDetails_RevokeSponsorship_TrustlinePoolShare(t *testing.T) {
	var poolHash xdr.Hash
	for i := range poolHash {
		poolHash[i] = 0xcd
	}
	poolId := xdr.PoolId(poolHash)

	op := decodedOperation(t, xdr.OperationTypeRevokeSponsorship, xdr.RevokeSponsorshipOp{
		Type: xdr.RevokeSponsorshipTypeRevokeSponsorshipLedgerEntry,
		LedgerKey: &xdr.LedgerKey{
			Type: xdr.LedgerEntryTypeTrustline,
			TrustLine: &xdr.LedgerKeyTrustLine{
				AccountId: xdr.MustAddress(claimantAddr),
				Asset: xdr.TrustLineAsset{
					Type:            xdr.AssetTypeAssetTypePoolShare,
					LiquidityPoolId: &poolId,
				},
			},
		},
	})

	details := extractOperationDetails(op)

	wantAsset := "liquidity_pool:" + strings.Repeat("cd", 32)
	if got := details["trustline_asset"]; got != wantAsset {
		t.Errorf("details[\"trustline_asset\"] = %v, want %v", got, wantAsset)
	}
	if got := details["sponsorship_type"]; got != "trustline" {
		t.Errorf("details[\"sponsorship_type\"] = %v, want trustline", got)
	}
}

func TestExtractOperationDetails_RevokeSponsorship_Offer(t *testing.T) {
	op := decodedOperation(t, xdr.OperationTypeRevokeSponsorship, xdr.RevokeSponsorshipOp{
		Type: xdr.RevokeSponsorshipTypeRevokeSponsorshipLedgerEntry,
		LedgerKey: &xdr.LedgerKey{
			Type: xdr.LedgerEntryTypeOffer,
			Offer: &xdr.LedgerKeyOffer{
				SellerId: xdr.MustAddress(sponsoredAddr),
				OfferId:  12345,
			},
		},
	})

	details := extractOperationDetails(op)

	want := map[string]interface{}{
		"type":             "revoke_sponsorship",
		"sponsorship_type": "offer",
		"seller_id":        sponsoredAddr,
		"offer_id":         "12345",
	}
	for k, v := range want {
		if got := details[k]; got != v {
			t.Errorf("details[%q] = %v, want %v", k, got, v)
		}
	}
}

func TestExtractOperationDetails_RevokeSponsorship_Data(t *testing.T) {
	op := decodedOperation(t, xdr.OperationTypeRevokeSponsorship, xdr.RevokeSponsorshipOp{
		Type: xdr.RevokeSponsorshipTypeRevokeSponsorshipLedgerEntry,
		LedgerKey: &xdr.LedgerKey{
			Type: xdr.LedgerEntryTypeData,
			Data: &xdr.LedgerKeyData{
				AccountId: xdr.MustAddress(sponsoredAddr),
				DataName:  "config.setting",
			},
		},
	})

	details := extractOperationDetails(op)

	want := map[string]interface{}{
		"type":             "revoke_sponsorship",
		"sponsorship_type": "data",
		"data_account_id":  sponsoredAddr,
		"data_name":        "config.setting",
	}
	for k, v := range want {
		if got := details[k]; got != v {
			t.Errorf("details[%q] = %v, want %v", k, got, v)
		}
	}
}

func TestExtractOperationDetails_RevokeSponsorship_LiquidityPool(t *testing.T) {
	var poolHash xdr.Hash
	for i := range poolHash {
		poolHash[i] = 0xcd
	}

	op := decodedOperation(t, xdr.OperationTypeRevokeSponsorship, xdr.RevokeSponsorshipOp{
		Type: xdr.RevokeSponsorshipTypeRevokeSponsorshipLedgerEntry,
		LedgerKey: &xdr.LedgerKey{
			Type: xdr.LedgerEntryTypeLiquidityPool,
			LiquidityPool: &xdr.LedgerKeyLiquidityPool{
				LiquidityPoolId: xdr.PoolId(poolHash),
			},
		},
	})

	details := extractOperationDetails(op)

	want := map[string]interface{}{
		"type":              "revoke_sponsorship",
		"sponsorship_type":  "liquidity_pool",
		"liquidity_pool_id": strings.Repeat("cd", 32),
	}
	for k, v := range want {
		if got := details[k]; got != v {
			t.Errorf("details[%q] = %v, want %v", k, got, v)
		}
	}
}

func TestExtractOperationDetails_RevokeSponsorship_UnknownLedgerKey(t *testing.T) {
	var codeHash xdr.Hash
	for i := range codeHash {
		codeHash[i] = 0xef
	}

	// contract_code is not a revocable sponsorship target; the default arm
	// should record it instead of leaving sponsorship_type unset.
	op := decodedOperation(t, xdr.OperationTypeRevokeSponsorship, xdr.RevokeSponsorshipOp{
		Type: xdr.RevokeSponsorshipTypeRevokeSponsorshipLedgerEntry,
		LedgerKey: &xdr.LedgerKey{
			Type: xdr.LedgerEntryTypeContractCode,
			ContractCode: &xdr.LedgerKeyContractCode{
				Hash: codeHash,
			},
		},
	})

	details := extractOperationDetails(op)

	want := map[string]interface{}{
		"type":             "revoke_sponsorship",
		"sponsorship_type": "unknown",
		"ledger_key_type":  "LedgerEntryTypeContractCode",
	}
	for k, v := range want {
		if got := details[k]; got != v {
			t.Errorf("details[%q] = %v, want %v", k, got, v)
		}
	}
}

func TestExtractOperationDetails_RevokeSponsorship_Signer(t *testing.T) {
	op := decodedOperation(t, xdr.OperationTypeRevokeSponsorship, xdr.RevokeSponsorshipOp{
		Type: xdr.RevokeSponsorshipTypeRevokeSponsorshipSigner,
		Signer: &xdr.RevokeSponsorshipOpSigner{
			AccountId: xdr.MustAddress(sponsoredAddr),
			SignerKey: xdr.MustSigner(claimantAddr),
		},
	})

	details := extractOperationDetails(op)

	want := map[string]interface{}{
		"type":              "revoke_sponsorship",
		"sponsorship_type":  "signer",
		"signer_account_id": sponsoredAddr,
		"signer_key":        claimantAddr,
	}
	for k, v := range want {
		if got := details[k]; got != v {
			t.Errorf("details[%q] = %v, want %v", k, got, v)
		}
	}
}
