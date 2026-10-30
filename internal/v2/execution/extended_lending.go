package execution

import (
	"math"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/lending"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func (e Engine) executeLending(t tx.Transaction, op tx.Operation) (Result, error) {
	switch op.Kind {
	case tx.OpLoanOffer:
		return e.executeLoanOffer(t, op.Payload)
	case tx.OpLoanCancel:
		return e.executeLoanCancel(t, op.Payload)
	case tx.OpLoanAccept:
		return e.executeLoanAccept(t, op.Payload)
	case tx.OpLoanRepay:
		return e.executeLoanRepay(t, op.Payload)
	case tx.OpLoanDefault:
		return e.executeLoanDefault(t, op.Payload)
	default:
		return Result{}, ErrUnsupportedOperation
	}
}

func (e Engine) executeLoanOffer(t tx.Transaction, payload []byte) (Result, error) {
	terms, err := lending.ParseTerms(payload)
	if err != nil || terms.Lender != t.Sender {
		return Result{}, lending.ErrLendingState
	}
	if e.Height > 0 && terms.MaturityHeight <= e.Height {
		return Result{}, lending.ErrLendingState
	}
	if err := e.requireLendingSameShard(terms); err != nil {
		return Result{}, err
	}
	if err := requireLendingChangeOnly(t); err != nil {
		return Result{}, err
	}
	created, outbound, consumed, err := e.accountNativeValue(t, nil, 0, terms.Principal)
	if err != nil {
		return Result{}, err
	}
	raw, err := terms.MarshalBinary()
	if err != nil {
		return Result{}, err
	}
	txID := t.ID()
	created = append(created, object.Object{
		ID:      types.ObjectIDForShard(txID, lending.OfferObjectIndex, t.ShardID),
		Version: 1,
		Owner:   terms.Lender,
		Kind:    object.KindLoanOffer,
		Data:    raw,
	})
	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: txID}, nil
}

func (e Engine) executeLoanCancel(t tx.Transaction, payload []byte) (Result, error) {
	ref, err := lending.ParseRef(payload)
	if err != nil {
		return Result{}, err
	}
	offerWitness, ok := witnessByID(t, ref.ObjectID)
	if !ok || offerWitness.Object.Kind != object.KindLoanOffer || offerWitness.Object.Owner != t.Sender {
		return Result{}, lending.ErrLendingState
	}
	terms, err := lending.ParseTerms(offerWitness.Object.Data)
	if err != nil || terms.Lender != t.Sender {
		return Result{}, lending.ErrLendingState
	}
	if err := requireLendingChangeOnly(t); err != nil {
		return Result{}, err
	}
	excluded := map[types.ObjectID]bool{offerWitness.Object.ID: true}
	created, outbound, consumed, err := e.accountNativeValue(t, excluded, terms.Principal, terms.Principal)
	if err != nil {
		return Result{}, err
	}
	payout, err := e.lendingPayout(t, terms.Lender, terms.Principal, lending.PayoutObjectIndex)
	if err != nil {
		return Result{}, err
	}
	created = append(created, payout)
	consumed = append(consumed, offerWitness.Object.ID)
	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: t.ID()}, nil
}

func (e Engine) executeLoanAccept(t tx.Transaction, payload []byte) (Result, error) {
	if e.Height == 0 {
		return Result{}, lending.ErrLendingState
	}
	ref, err := lending.ParseRef(payload)
	if err != nil {
		return Result{}, err
	}
	offerWitness, ok := witnessByID(t, ref.ObjectID)
	if !ok || offerWitness.Object.Kind != object.KindLoanOffer {
		return Result{}, lending.ErrLendingState
	}
	terms, err := lending.ParseTerms(offerWitness.Object.Data)
	if err != nil || terms.Borrower != t.Sender || offerWitness.Object.Owner != terms.Lender || e.Height >= terms.MaturityHeight {
		return Result{}, lending.ErrLendingState
	}
	if err := e.requireLendingSameShard(terms); err != nil {
		return Result{}, err
	}
	if err := requireLendingChangeOnly(t); err != nil {
		return Result{}, err
	}
	locked, err := addLendingValue(terms.Principal, terms.Collateral)
	if err != nil {
		return Result{}, err
	}
	excluded := map[types.ObjectID]bool{offerWitness.Object.ID: true}
	created, outbound, consumed, err := e.accountNativeValue(t, excluded, terms.Principal, locked)
	if err != nil {
		return Result{}, err
	}
	principal, err := e.lendingPayout(t, terms.Borrower, terms.Principal, lending.PayoutObjectIndex)
	if err != nil {
		return Result{}, err
	}
	loan := lending.Loan{OfferObject: offerWitness.Object.ID, Terms: terms, AcceptedHeight: e.Height}
	raw, err := loan.MarshalBinary()
	if err != nil {
		return Result{}, err
	}
	created = append(created,
		principal,
		object.Object{
			ID:      types.ObjectIDForShard(t.ID(), lending.LoanObjectIndex, t.ShardID),
			Version: 1,
			Owner:   terms.Borrower,
			Kind:    object.KindLoan,
			Data:    raw,
		},
	)
	consumed = append(consumed, offerWitness.Object.ID)
	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: t.ID()}, nil
}

func (e Engine) executeLoanRepay(t tx.Transaction, payload []byte) (Result, error) {
	ref, err := lending.ParseRef(payload)
	if err != nil {
		return Result{}, err
	}
	loanWitness, ok := witnessByID(t, ref.ObjectID)
	if !ok || loanWitness.Object.Kind != object.KindLoan || loanWitness.Object.Owner != t.Sender {
		return Result{}, lending.ErrLendingState
	}
	loan, err := lending.ParseLoan(loanWitness.Object.Data)
	if err != nil || loan.Terms.Borrower != t.Sender {
		return Result{}, lending.ErrLendingState
	}
	if err := requireLendingChangeOnly(t); err != nil {
		return Result{}, err
	}
	due, err := loan.Terms.RepaymentDue()
	if err != nil {
		return Result{}, err
	}
	settlement, err := addLendingValue(due, loan.Terms.Collateral)
	if err != nil {
		return Result{}, err
	}
	excluded := map[types.ObjectID]bool{loanWitness.Object.ID: true}
	created, outbound, consumed, err := e.accountNativeValue(t, excluded, loan.Terms.Collateral, settlement)
	if err != nil {
		return Result{}, err
	}
	lenderPayout, err := e.lendingPayout(t, loan.Terms.Lender, due, lending.PayoutObjectIndex)
	if err != nil {
		return Result{}, err
	}
	created = append(created, lenderPayout)
	if loan.Terms.Collateral > 0 {
		collateral, err := e.lendingPayout(t, loan.Terms.Borrower, loan.Terms.Collateral, lending.PayoutObjectIndex+1)
		if err != nil {
			return Result{}, err
		}
		created = append(created, collateral)
	}
	consumed = append(consumed, loanWitness.Object.ID)
	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: t.ID()}, nil
}

func (e Engine) executeLoanDefault(t tx.Transaction, payload []byte) (Result, error) {
	if e.Height == 0 {
		return Result{}, lending.ErrLendingState
	}
	ref, err := lending.ParseRef(payload)
	if err != nil {
		return Result{}, err
	}
	loanWitness, ok := witnessByID(t, ref.ObjectID)
	if !ok || loanWitness.Object.Kind != object.KindLoan {
		return Result{}, lending.ErrLendingState
	}
	loan, err := lending.ParseLoan(loanWitness.Object.Data)
	if err != nil || loan.Terms.Lender != t.Sender || e.Height <= loan.Terms.MaturityHeight {
		return Result{}, lending.ErrLendingState
	}
	if err := requireLendingChangeOnly(t); err != nil {
		return Result{}, err
	}
	excluded := map[types.ObjectID]bool{loanWitness.Object.ID: true}
	created, outbound, consumed, err := e.accountNativeValue(t, excluded, loan.Terms.Collateral, loan.Terms.Collateral)
	if err != nil {
		return Result{}, err
	}
	if loan.Terms.Collateral > 0 {
		collateral, err := e.lendingPayout(t, loan.Terms.Lender, loan.Terms.Collateral, lending.PayoutObjectIndex)
		if err != nil {
			return Result{}, err
		}
		created = append(created, collateral)
	}
	consumed = append(consumed, loanWitness.Object.ID)
	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: t.ID()}, nil
}

func (e Engine) requireLendingSameShard(terms lending.Terms) error {
	count := e.ShardCount
	if count == 0 {
		count = 1
	}
	router := sharding.Router{ShardCount: count}
	lenderShard, err := router.ShardForAccount(terms.Lender)
	if err != nil {
		return ErrShard
	}
	borrowerShard, err := router.ShardForAccount(terms.Borrower)
	if err != nil || lenderShard != borrowerShard {
		return ErrShard
	}
	return nil
}

func requireLendingChangeOnly(t tx.Transaction) error {
	for _, output := range t.Outputs {
		if output.Owner != t.Sender || output.Kind != object.KindCoin {
			return lending.ErrLendingState
		}
	}
	return nil
}

func (e Engine) lendingPayout(t tx.Transaction, owner types.AccountID, amount uint64, index uint32) (object.Object, error) {
	if amount == 0 {
		return object.Object{}, lending.ErrLendingState
	}
	count := e.ShardCount
	if count == 0 {
		count = 1
	}
	if types.AccountShard(owner, count) != t.ShardID {
		return object.Object{}, ErrShard
	}
	spec, err := object.NewCoinOutputAtHeight(owner, e.NativeToken, amount, e.Height)
	if err != nil {
		return object.Object{}, err
	}
	return object.Object{
		ID:      types.ObjectIDForShard(t.ID(), index, t.ShardID),
		Version: 1,
		Owner:   owner,
		Kind:    object.KindCoin,
		Data:    spec.Data,
	}, nil
}

func addLendingValue(a, b uint64) (uint64, error) {
	if math.MaxUint64-a < b {
		return 0, ErrOverflow
	}
	return a + b, nil
}
