package api

import (
	"net/http"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func TestProtocolWireLimitViolationsReturnRequestTooLarge(t *testing.T) {
	for _, err := range []error{tx.ErrEnvelopeTooLarge, consensus.ErrTooManyTransactions} {
		if got := statusForError(err); got != http.StatusRequestEntityTooLarge {
			t.Fatalf("wire limit %v returned HTTP %d instead of 413", err, got)
		}
	}
}
