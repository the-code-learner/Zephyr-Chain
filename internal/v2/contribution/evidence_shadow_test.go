package contribution

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/citizen"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/devicecredential"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/economics"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestShadowEvidenceEnvelopeKeepsEconomicAndCitizenEvidenceSeparate(t *testing.T) {
	network := testNetwork(1)
	zeci := testZECI(7, 1)
	citizenEvidence := []citizen.VerifiedDeviceChallengeEvidence{
		testCitizenEvidence(network, 1),
		testCitizenEvidence(network, 2),
	}

	envelope, err := BuildShadowEvidenceEnvelope(network, &zeci, citizenEvidence)
	if err != nil {
		t.Fatal(err)
	}
	if !envelope.Shadow || !envelope.EconomicEvidenceAvailable || !envelope.CitizenKeyControlAvailable {
		t.Fatalf("unexpected evidence availability: %+v", envelope)
	}
	if envelope.EconomicEpoch != zeci.Epoch || envelope.CitizenKeyControlCount != 2 {
		t.Fatalf("unexpected evidence metadata: %+v", envelope)
	}
	zeciHash, err := zeci.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if envelope.EconomicEvidenceHash != zeciHash {
		t.Fatal("economic evidence commitment did not preserve exact ZECI hash")
	}
	if types.IsZero32([32]byte(envelope.CitizenKeyControlRoot)) {
		t.Fatal("Citizen key-control root was not committed")
	}
	if hash, err := envelope.Hash(); err != nil || types.IsZero32([32]byte(hash)) {
		t.Fatalf("invalid envelope hash: %v %x", err, hash)
	}
}

func TestShadowEvidenceEnvelopeCanonicalAcrossCitizenOrder(t *testing.T) {
	network := testNetwork(1)
	zeci := testZECI(7, 1)
	first := testCitizenEvidence(network, 1)
	second := testCitizenEvidence(network, 2)

	a, err := BuildShadowEvidenceEnvelope(network, &zeci, []citizen.VerifiedDeviceChallengeEvidence{first, second})
	if err != nil {
		t.Fatal(err)
	}
	b, err := BuildShadowEvidenceEnvelope(network, &zeci, []citizen.VerifiedDeviceChallengeEvidence{second, first})
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("Citizen input ordering changed canonical envelope: a=%+v b=%+v", a, b)
	}
}

func TestShadowEvidenceEnvelopeKeepsMissingClassesExplicit(t *testing.T) {
	network := testNetwork(1)
	zeci := testZECI(7, 1)

	economicOnly, err := BuildShadowEvidenceEnvelope(network, &zeci, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !economicOnly.EconomicEvidenceAvailable || economicOnly.CitizenKeyControlAvailable ||
		economicOnly.CitizenKeyControlCount != 0 || !types.IsZero32([32]byte(economicOnly.CitizenKeyControlRoot)) {
		t.Fatalf("missing Citizen evidence was invented: %+v", economicOnly)
	}

	citizenOnly, err := BuildShadowEvidenceEnvelope(
		network,
		nil,
		[]citizen.VerifiedDeviceChallengeEvidence{testCitizenEvidence(network, 1)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if citizenOnly.EconomicEvidenceAvailable || citizenOnly.EconomicEpoch != 0 ||
		!types.IsZero32([32]byte(citizenOnly.EconomicEvidenceHash)) ||
		!citizenOnly.CitizenKeyControlAvailable {
		t.Fatalf("missing economic evidence was invented: %+v", citizenOnly)
	}

	if _, err := BuildShadowEvidenceEnvelope(network, nil, nil); err == nil {
		t.Fatal("empty evidence manifest was accepted")
	}
}

func TestShadowEvidenceEnvelopeRejectsDuplicateOrCrossNetworkCitizenEvidence(t *testing.T) {
	network := testNetwork(1)
	duplicate := testCitizenEvidence(network, 1)
	if _, err := BuildShadowEvidenceEnvelope(
		network,
		nil,
		[]citizen.VerifiedDeviceChallengeEvidence{duplicate, duplicate},
	); err == nil {
		t.Fatal("duplicate Citizen challenge evidence was accepted")
	}

	foreign := testCitizenEvidence(testNetwork(2), 2)
	if _, err := BuildShadowEvidenceEnvelope(
		network,
		nil,
		[]citizen.VerifiedDeviceChallengeEvidence{foreign},
	); err == nil {
		t.Fatal("cross-network Citizen challenge evidence was accepted")
	}
}

func TestCitizenKeyControlRootBindsVerifiedFactsWithoutEconomicReclassification(t *testing.T) {
	network := testNetwork(1)
	base := testCitizenEvidence(network, 1)

	first, err := BuildShadowEvidenceEnvelope(
		network,
		nil,
		[]citizen.VerifiedDeviceChallengeEvidence{base},
	)
	if err != nil {
		t.Fatal(err)
	}

	changed := base
	changed.ContextHash = types.HashBytes("contribution/context", []byte("different"))
	second, err := BuildShadowEvidenceEnvelope(
		network,
		nil,
		[]citizen.VerifiedDeviceChallengeEvidence{changed},
	)
	if err != nil {
		t.Fatal(err)
	}
	if first.CitizenKeyControlRoot == second.CitizenKeyControlRoot {
		t.Fatal("Citizen key-control root did not bind context")
	}
	if first.EconomicEvidenceAvailable || second.EconomicEvidenceAvailable {
		t.Fatal("Citizen key-control evidence was reclassified as economic evidence")
	}
}

func TestShadowEvidenceRejectsMalformedSourceEvidence(t *testing.T) {
	network := testNetwork(1)

	badZECI := testZECI(7, 1)
	badZECI.Shadow = false
	if _, err := BuildShadowEvidenceEnvelope(network, &badZECI, nil); err == nil {
		t.Fatal("non-shadow ZECI source was accepted")
	}

	badCitizen := testCitizenEvidence(network, 1)
	badCitizen.RequiredCapabilities = 1 << 31
	if _, err := BuildShadowEvidenceEnvelope(
		network,
		nil,
		[]citizen.VerifiedDeviceChallengeEvidence{badCitizen},
	); err == nil {
		t.Fatal("unsupported Citizen capability evidence was accepted")
	}
}

func testNetwork(seed byte) types.NetworkID {
	var network types.NetworkID
	network[0] = seed
	return network
}

func testZECI(epoch uint64, seed byte) economics.ZECIShadowTelemetry {
	return economics.ZECIShadowTelemetry{
		Version:                  economics.ZECIShadowVersion,
		Epoch:                    epoch,
		Shadow:                   true,
		ShardCount:               2,
		FinalizedOperations:      20,
		ResourceUsed:             50,
		ResourceCapacity:         100,
		ResourceUtilizationBps:   5_000,
		AgeWeightedVelocityBps:   200,
		VerifiedWorkObservations: 2,
		VerifiedWorkUnits:        16,
		VerifiedWorkPaidZPH:      24,
		ComputeSupplyReliable:    true,
		ComputeFulfilled:         16,
		EvidenceHash:             types.Hash{seed},
	}
}

func testCitizenEvidence(network types.NetworkID, seed byte) citizen.VerifiedDeviceChallengeEvidence {
	return citizen.VerifiedDeviceChallengeEvidence{
		Network:              network,
		CredentialObject:     types.ObjectID{seed},
		CredentialSequence:   uint64(seed),
		CredentialHeight:     100 + uint64(seed),
		CredentialStateRoot:  types.Hash{seed + 10},
		Owner:                types.AccountID{seed + 20},
		RequiredCapabilities: devicecredential.CapabilityVerifyHeaders,
		ChallengeHash:        types.Hash{seed + 30},
		ContextHash:          types.Hash{seed + 40},
	}
}
