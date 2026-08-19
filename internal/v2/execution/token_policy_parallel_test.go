package execution

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestTokenDefinitionPolicyWitnessIsSharedReadForTransfers(t *testing.T) {
	var root types.Hash
	var definitionID, coinA, coinB types.ObjectID
	root[0] = 1
	definitionID[0] = 2
	coinA[0] = 3
	coinB[0] = 4
	definition := object.Object{ID: definitionID, Version: 1, Kind: object.KindTokenDefinition}
	first := tx.Transaction{
		StateRoot: root,
		Inputs: []tx.InputRef{{ObjectID: coinA}, {ObjectID: definitionID}},
		Operations: []tx.Operation{{Kind: tx.OpTransfer}},
		Witnesses: []tx.Witness{{Object: object.Object{ID: coinA, Kind: object.KindCoin}}, {Object: definition}},
	}
	first.Salt[0] = 1
	second := tx.Transaction{
		StateRoot: root,
		Inputs: []tx.InputRef{{ObjectID: coinB}, {ObjectID: definitionID}},
		Operations: []tx.Operation{{Kind: tx.OpTransfer}},
		Witnesses: []tx.Witness{{Object: object.Object{ID: coinB, Kind: object.KindCoin}}, {Object: definition}},
	}
	second.Salt[0] = 2
	if err := validateIndependentBatch([]tx.Transaction{first, second}); err != nil {
		t.Fatalf("shared read-only token definition should not serialize transfers: %v", err)
	}
}

func TestTokenDefinitionWriteConflictsWithTransferRead(t *testing.T) {
	var root types.Hash
	var definitionID, coinA types.ObjectID
	root[0] = 1
	definitionID[0] = 2
	coinA[0] = 3
	definition := object.Object{ID: definitionID, Version: 1, Kind: object.KindTokenDefinition}
	transfer := tx.Transaction{
		StateRoot: root,
		Inputs: []tx.InputRef{{ObjectID: coinA}, {ObjectID: definitionID}},
		Operations: []tx.Operation{{Kind: tx.OpTransfer}},
		Witnesses: []tx.Witness{{Object: object.Object{ID: coinA, Kind: object.KindCoin}}, {Object: definition}},
	}
	transfer.Salt[0] = 1
	mint := tx.Transaction{
		StateRoot: root,
		Inputs: []tx.InputRef{{ObjectID: definitionID}},
		Operations: []tx.Operation{{Kind: tx.OpMintToken}},
		Witnesses: []tx.Witness{{Object: definition}},
	}
	mint.Salt[0] = 2
	if err := validateIndependentBatch([]tx.Transaction{transfer, mint}); err != ErrBatchConflict {
		t.Fatalf("token definition write must conflict with transfer read, got %v", err)
	}
}
