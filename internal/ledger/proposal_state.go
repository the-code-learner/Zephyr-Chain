package ledger

import "github.com/zephyr-chain/zephyr-chain/internal/tx"

// ExpectedStateRoot deterministically executes a proposal body against the
// current committed state without mutating the store.
func (s *Store) ExpectedStateRoot(transactions []tx.Envelope) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return expectedStateRootFromState(s.snapshotLocked(), s.chainID, transactions)
}

func expectedStateRootFromState(state persistedState, chainID string, transactions []tx.Envelope) (string, error) {
	state = normalizeState(state)
	accounts := cloneAccounts(state.Accounts)
	for _, envelope := range transactions {
		if err := envelope.ValidateForChain(chainID); err != nil {
			return "", ErrBlockInvariant
		}
		sender := accounts[envelope.From]
		sender.Address = envelope.From
		expectedNonce, ok := nextUint64(sender.Nonce)
		if !ok || sender.Balance < envelope.Amount || expectedNonce != envelope.Nonce {
			return "", ErrBlockInvariant
		}
		sender.Balance -= envelope.Amount
		sender.Nonce = envelope.Nonce
		accounts[envelope.From] = sender

		receiver := accounts[envelope.To]
		receiver.Address = envelope.To
		receiverBalance, ok := addUint64(receiver.Balance, envelope.Amount)
		if !ok {
			return "", ErrBlockInvariant
		}
		receiver.Balance = receiverBalance
		accounts[envelope.To] = receiver
	}
	rootState := state
	rootState.Accounts = accounts
	return stateRootFromState(chainID, rootState)
}
