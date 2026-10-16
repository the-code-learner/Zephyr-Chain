package contracts

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestDeploymentAcceptsWASMVersionOneEnvelope(t *testing.T) {
	code := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	deployment := Deployment{
		Runtime: RuntimeWASMv1, Code: code, ABI: 1,
		UpgradeAuthority: types.AccountIDFromPublicKey([]byte("owner")),
		MaxMemoryPages:   64,
	}
	if err := deployment.Validate(); err != nil {
		t.Fatal(err)
	}
}
