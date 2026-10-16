package execution

import (
	"github.com/zephyr-chain/zephyr-chain/internal/v2/devicecredential"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func (e Engine) executeDeviceCredential(t tx.Transaction, op tx.Operation) (Result, error) {
	switch op.Kind {
	case tx.OpDeviceCredentialRegister:
		return e.executeDeviceCredentialRegister(t, op.Payload)
	case tx.OpDeviceCredentialUpdate:
		return e.executeDeviceCredentialUpdate(t, op.Payload)
	case tx.OpDeviceCredentialRevoke:
		return e.executeDeviceCredentialRevoke(t, op.Payload)
	default:
		return Result{}, ErrUnsupportedOperation
	}
}

func (e Engine) executeDeviceCredentialRegister(t tx.Transaction, payload []byte) (Result, error) {
	if e.Height == 0 || !e.deviceOwnerOnShard(t.Sender, t.ShardID) {
		return Result{}, devicecredential.ErrCredential
	}
	spec, err := devicecredential.ParseSpec(payload)
	if err != nil || (spec.ExpiresHeight != 0 && spec.ExpiresHeight <= e.Height) {
		return Result{}, devicecredential.ErrCredential
	}
	created, consumed, err := e.feeOnlyOutputs(t)
	if err != nil {
		return Result{}, err
	}
	credential := devicecredential.Credential{
		Owner:           t.Sender,
		DevicePublicKey: append([]byte(nil), spec.DevicePublicKey...),
		Capabilities:    spec.Capabilities,
		IssuedHeight:    e.Height,
		ExpiresHeight:   spec.ExpiresHeight,
		Sequence:        1,
	}
	raw, err := credential.MarshalBinary()
	if err != nil {
		return Result{}, err
	}
	created = append(created, object.Object{
		ID:      types.ObjectIDForShard(t.ID(), devicecredential.ObjectIndex, t.ShardID),
		Version: 1,
		Owner:   t.Sender,
		Kind:    object.KindDeviceCredential,
		Data:    raw,
	})
	return Result{Consumed: consumed, Created: created, TxID: t.ID()}, nil
}

func (e Engine) executeDeviceCredentialUpdate(t tx.Transaction, payload []byte) (Result, error) {
	if e.Height == 0 || !e.deviceOwnerOnShard(t.Sender, t.ShardID) {
		return Result{}, devicecredential.ErrCredential
	}
	update, err := devicecredential.ParseUpdate(payload)
	if err != nil || (update.Spec.ExpiresHeight != 0 && update.Spec.ExpiresHeight <= e.Height) {
		return Result{}, devicecredential.ErrCredential
	}
	witness, ok := witnessByID(t, update.ObjectID)
	if !ok || witness.Object.Kind != object.KindDeviceCredential || witness.Object.Owner != t.Sender {
		return Result{}, devicecredential.ErrCredential
	}
	current, err := devicecredential.ParseCredential(witness.Object.Data)
	if err != nil || current.Owner != t.Sender {
		return Result{}, devicecredential.ErrCredential
	}
	created, consumed, err := e.feeOnlyOutputsExcluding(t, witness.Object.ID, nil)
	if err != nil {
		return Result{}, err
	}
	updated := devicecredential.Credential{
		Owner:           t.Sender,
		DevicePublicKey: append([]byte(nil), update.Spec.DevicePublicKey...),
		Capabilities:    update.Spec.Capabilities,
		IssuedHeight:    e.Height,
		ExpiresHeight:   update.Spec.ExpiresHeight,
		Sequence:        current.Sequence + 1,
	}
	raw, err := updated.MarshalBinary()
	if err != nil {
		return Result{}, err
	}
	next := witness.Object
	next.Version++
	next.Data = raw
	created = append(created, next)
	consumed = append(consumed, witness.Object.ID)
	return Result{Consumed: consumed, Created: created, TxID: t.ID()}, nil
}

func (e Engine) executeDeviceCredentialRevoke(t tx.Transaction, payload []byte) (Result, error) {
	ref, err := devicecredential.ParseRef(payload)
	if err != nil || !e.deviceOwnerOnShard(t.Sender, t.ShardID) {
		return Result{}, devicecredential.ErrCredential
	}
	witness, ok := witnessByID(t, ref.ObjectID)
	if !ok || witness.Object.Kind != object.KindDeviceCredential || witness.Object.Owner != t.Sender {
		return Result{}, devicecredential.ErrCredential
	}
	current, err := devicecredential.ParseCredential(witness.Object.Data)
	if err != nil || current.Owner != t.Sender {
		return Result{}, devicecredential.ErrCredential
	}
	created, consumed, err := e.feeOnlyOutputsExcluding(t, witness.Object.ID, nil)
	if err != nil {
		return Result{}, err
	}
	consumed = append(consumed, witness.Object.ID)
	return Result{Consumed: consumed, Created: created, TxID: t.ID()}, nil
}

func (e Engine) deviceOwnerOnShard(owner types.AccountID, shard uint32) bool {
	count := e.ShardCount
	if count == 0 {
		count = 1
	}
	return types.AccountShard(owner, count) == shard
}
