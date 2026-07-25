package transform

// Hermetic unit tests for the account administration & data operation family:
// set_options, account_merge, bump_sequence, manage_data, inflation.
//
// No live network or fixture files are used: each test builds an xdr.Operation
// in memory, calls extractOperationDetails / enrichOperation directly, and
// asserts the expected fields.

import (
	"encoding/json"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/miguelnietoa/stellar-explorer/indexer/internal/store"
)

const (
	acctA = "GBKPF4URAGUGPBFKQNMDDD4IY5BRRXRK2VEBULJEMVULCCODND436NIO"
	acctB = "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"
)

func uint32ptr(v uint32) *xdr.Uint32 { u := xdr.Uint32(v); return &u }

func accountIDptr(addr string) *xdr.AccountId {
	id := xdr.MustAddress(addr)
	return &id
}

func string32ptr(s string) *xdr.String32 { v := xdr.String32(s); return &v }

func assertDetail(t *testing.T, details map[string]interface{}, key string, want interface{}) {
	t.Helper()
	got, ok := details[key]
	if !ok {
		t.Errorf("details[%q] is missing (details: %v)", key, details)
		return
	}
	wantJSON, _ := json.Marshal(want)
	gotJSON, _ := json.Marshal(got)
	if string(wantJSON) != string(gotJSON) {
		t.Errorf("details[%q]: want %s, got %s", key, wantJSON, gotJSON)
	}
}

func assertDetailAbsent(t *testing.T, details map[string]interface{}, key string) {
	t.Helper()
	if _, ok := details[key]; ok {
		t.Errorf("details[%q] should be absent but is present (value=%v)", key, details[key])
	}
}

func newStoreOp() store.Operation {
	return store.Operation{}
}

// set_options

func TestExtractOperationDetails_SetOptions_Full(t *testing.T) {
	accountID := xdr.MustAddress(acctB)
	ed25519 := accountID.MustEd25519()
	signerKey := xdr.SignerKey{
		Type:    xdr.SignerKeyTypeSignerKeyTypeEd25519,
		Ed25519: &ed25519,
	}
	op := xdr.Operation{
		Body: xdr.OperationBody{
			Type: xdr.OperationTypeSetOptions,
			SetOptionsOp: &xdr.SetOptionsOp{
				InflationDest: accountIDptr(acctA),
				SetFlags:      uint32ptr(3),
				ClearFlags:    uint32ptr(1),
				MasterWeight:  uint32ptr(10),
				LowThreshold:  uint32ptr(1),
				MedThreshold:  uint32ptr(2),
				HighThreshold: uint32ptr(3),
				HomeDomain:    string32ptr("example.com"),
				Signer: &xdr.Signer{
					Key:    signerKey,
					Weight: 5,
				},
			},
		},
	}

	details := extractOperationDetails(op)

	assertDetail(t, details, "type", "set_options")
	assertDetail(t, details, "inflation_dest", acctA)
	assertDetail(t, details, "set_flags", uint32(3))
	assertDetail(t, details, "clear_flags", uint32(1))
	assertDetail(t, details, "master_weight", uint32(10))
	assertDetail(t, details, "low_threshold", uint32(1))
	assertDetail(t, details, "med_threshold", uint32(2))
	assertDetail(t, details, "high_threshold", uint32(3))
	assertDetail(t, details, "home_domain", "example.com")
	assertDetail(t, details, "signer_key", acctB)
	assertDetail(t, details, "signer_weight", uint32(5))
}

func TestExtractOperationDetails_SetOptions_Sparse(t *testing.T) {
	op := xdr.Operation{
		Body: xdr.OperationBody{
			Type: xdr.OperationTypeSetOptions,
			SetOptionsOp: &xdr.SetOptionsOp{
				HomeDomain: string32ptr("stellar.org"),
			},
		},
	}

	details := extractOperationDetails(op)

	assertDetail(t, details, "home_domain", "stellar.org")
	assertDetailAbsent(t, details, "inflation_dest")
	assertDetailAbsent(t, details, "set_flags")
	assertDetailAbsent(t, details, "clear_flags")
	assertDetailAbsent(t, details, "master_weight")
	assertDetailAbsent(t, details, "signer_key")
}

func TestEnrichOperation_SetOptions_NoPromotedColumns(t *testing.T) {
	op := xdr.Operation{
		Body: xdr.OperationBody{
			Type:         xdr.OperationTypeSetOptions,
			SetOptionsOp: &xdr.SetOptionsOp{MasterWeight: uint32ptr(1)},
		},
	}
	details := extractOperationDetails(op)

	storeOp := newStoreOp()
	enrichOperation(&storeOp, op, details)

	if storeOp.Destination != nil {
		t.Errorf("set_options: Destination should be nil, got %v", storeOp.Destination)
	}
	if storeOp.Amount != nil {
		t.Errorf("set_options: Amount should be nil, got %v", storeOp.Amount)
	}
}

// account_merge

func TestExtractOperationDetails_AccountMerge(t *testing.T) {
	dest, err := xdr.AddressToMuxedAccount(acctA)
	if err != nil {
		t.Fatalf("failed to build MuxedAccount: %v", err)
	}
	op := xdr.Operation{
		Body: xdr.OperationBody{
			Type:        xdr.OperationTypeAccountMerge,
			Destination: &dest,
		},
	}

	details := extractOperationDetails(op)

	assertDetail(t, details, "type", "account_merge")
	assertDetail(t, details, "destination", acctA)
}

func TestEnrichOperation_AccountMerge_SetsDestination(t *testing.T) {
	dest, err := xdr.AddressToMuxedAccount(acctA)
	if err != nil {
		t.Fatalf("failed to build MuxedAccount: %v", err)
	}
	op := xdr.Operation{
		Body: xdr.OperationBody{
			Type:        xdr.OperationTypeAccountMerge,
			Destination: &dest,
		},
	}
	details := extractOperationDetails(op)

	storeOp := newStoreOp()
	enrichOperation(&storeOp, op, details)

	if storeOp.Destination == nil {
		t.Fatal("account_merge: expected Destination to be set")
	}
	if *storeOp.Destination != acctA {
		t.Errorf("account_merge: Destination = %q, want %q", *storeOp.Destination, acctA)
	}
	if storeOp.DestinationMuxed != nil {
		t.Errorf("account_merge: expected nil DestinationMuxed for plain address")
	}
}

// bump_sequence

func TestExtractOperationDetails_BumpSequence(t *testing.T) {
	op := xdr.Operation{
		Body: xdr.OperationBody{
			Type: xdr.OperationTypeBumpSequence,
			BumpSequenceOp: &xdr.BumpSequenceOp{
				BumpTo: xdr.SequenceNumber(9999999),
			},
		},
	}

	details := extractOperationDetails(op)

	assertDetail(t, details, "type", "bump_sequence")
	assertDetail(t, details, "bump_to", "9999999")
}

func TestEnrichOperation_BumpSequence_NoPromotedColumns(t *testing.T) {
	op := xdr.Operation{
		Body: xdr.OperationBody{
			Type:           xdr.OperationTypeBumpSequence,
			BumpSequenceOp: &xdr.BumpSequenceOp{BumpTo: 42},
		},
	}
	details := extractOperationDetails(op)

	storeOp := newStoreOp()
	enrichOperation(&storeOp, op, details)

	if storeOp.Destination != nil {
		t.Errorf("bump_sequence: Destination should be nil, got %v", storeOp.Destination)
	}
	if storeOp.Amount != nil {
		t.Errorf("bump_sequence: Amount should be nil, got %v", storeOp.Amount)
	}
}

// manage_data

func TestExtractOperationDetails_ManageData_Set(t *testing.T) {
	val := xdr.DataValue([]byte("hello"))
	op := xdr.Operation{
		Body: xdr.OperationBody{
			Type: xdr.OperationTypeManageData,
			ManageDataOp: &xdr.ManageDataOp{
				DataName:  "my-key",
				DataValue: &val,
			},
		},
	}

	details := extractOperationDetails(op)

	assertDetail(t, details, "type", "manage_data")
	assertDetail(t, details, "name", "my-key")
	assertDetail(t, details, "value", "aGVsbG8=") // "hello" in base64
}

func TestExtractOperationDetails_ManageData_Delete(t *testing.T) {
	op := xdr.Operation{
		Body: xdr.OperationBody{
			Type: xdr.OperationTypeManageData,
			ManageDataOp: &xdr.ManageDataOp{
				DataName:  "my-key",
				DataValue: nil,
			},
		},
	}

	details := extractOperationDetails(op)

	assertDetail(t, details, "name", "my-key")
	assertDetailAbsent(t, details, "value")
}

func TestEnrichOperation_ManageData_NoPromotedColumns(t *testing.T) {
	val := xdr.DataValue([]byte("x"))
	op := xdr.Operation{
		Body: xdr.OperationBody{
			Type: xdr.OperationTypeManageData,
			ManageDataOp: &xdr.ManageDataOp{
				DataName:  "k",
				DataValue: &val,
			},
		},
	}
	details := extractOperationDetails(op)

	storeOp := newStoreOp()
	enrichOperation(&storeOp, op, details)

	if storeOp.Amount != nil {
		t.Errorf("manage_data: Amount should be nil")
	}
	if storeOp.AssetCode != nil {
		t.Errorf("manage_data: AssetCode should be nil")
	}
}

// inflation

func TestExtractOperationDetails_Inflation(t *testing.T) {
	op := xdr.Operation{
		Body: xdr.OperationBody{
			Type: xdr.OperationTypeInflation,
		},
	}

	details := extractOperationDetails(op)

	assertDetail(t, details, "type", "inflation")
	if len(details) != 1 {
		t.Errorf("inflation: expected exactly 1 detail key (type), got %d: %v", len(details), details)
	}
}

func TestEnrichOperation_Inflation_NoPromotedColumns(t *testing.T) {
	op := xdr.Operation{
		Body: xdr.OperationBody{
			Type: xdr.OperationTypeInflation,
		},
	}
	details := extractOperationDetails(op)

	storeOp := newStoreOp()
	enrichOperation(&storeOp, op, details)

	if storeOp.Destination != nil || storeOp.Amount != nil || storeOp.AssetCode != nil {
		t.Errorf("inflation: all promoted columns should be nil")
	}
}
