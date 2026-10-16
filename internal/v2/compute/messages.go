package compute

import (
	"bytes"
	"errors"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var ErrComputeMessage = errors.New("invalid compute cross-shard message")

type AssignmentMessage struct {
	JobID    types.JobID
	JobOwner types.AccountID
	JobShard uint32
	OfferID  types.Hash
	Offer    Offer
	Job      Job
}

func (m AssignmentMessage) Validate() error {
	if types.IsZero32([32]byte(m.JobID)) || types.IsZero32([32]byte(m.JobOwner)) || types.IsZero32([32]byte(m.OfferID)) || m.Job.Owner != m.JobOwner {
		return ErrComputeMessage
	}
	if err := m.Offer.Validate(); err != nil {
		return ErrComputeMessage
	}
	if err := m.Job.Validate(); err != nil || !offerMatchesJob(m.Offer, m.Job) || m.Offer.Collateral < m.Job.CollateralRequired {
		return ErrComputeMessage
	}
	return nil
}

func (m AssignmentMessage) MarshalBinary() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	offer, _ := m.Offer.MarshalBinary()
	job, _ := m.Job.MarshalBinary()
	var w codec.Writer
	w.Fixed(m.JobID[:])
	w.Fixed(m.JobOwner[:])
	w.U32(m.JobShard)
	w.Fixed(m.OfferID[:])
	w.Bytes(offer)
	w.Bytes(job)
	return w.BytesCopy(), nil
}

func ParseAssignmentMessage(data []byte) (AssignmentMessage, error) {
	r := codec.NewReader(data)
	jobIDRaw, err := r.Fixed(32)
	if err != nil {
		return AssignmentMessage{}, ErrComputeMessage
	}
	owner, err := readAccount(r)
	if err != nil {
		return AssignmentMessage{}, ErrComputeMessage
	}
	shard, err := r.U32()
	if err != nil {
		return AssignmentMessage{}, ErrComputeMessage
	}
	offerID, err := readHash(r)
	if err != nil {
		return AssignmentMessage{}, ErrComputeMessage
	}
	offerRaw, err := r.Bytes(1 << 20)
	if err != nil {
		return AssignmentMessage{}, ErrComputeMessage
	}
	offer, err := ParseOffer(offerRaw)
	if err != nil {
		return AssignmentMessage{}, ErrComputeMessage
	}
	jobRaw, err := r.Bytes(1 << 20)
	if err != nil {
		return AssignmentMessage{}, ErrComputeMessage
	}
	job, err := ParseJob(jobRaw)
	if err != nil || r.Done() != nil {
		return AssignmentMessage{}, ErrComputeMessage
	}
	var jobID types.JobID
	copy(jobID[:], jobIDRaw)
	message := AssignmentMessage{JobID: jobID, JobOwner: owner, JobShard: shard, OfferID: offerID, Offer: offer, Job: job}
	if err := message.Validate(); err != nil {
		return AssignmentMessage{}, err
	}
	return message, nil
}

func (m AssignmentMessage) Output() (object.OutputSpec, error) {
	raw, err := m.MarshalBinary()
	if err != nil {
		return object.OutputSpec{}, err
	}
	return object.OutputSpec{Owner: m.JobOwner, Kind: object.KindComputeAssignment, Data: raw}, nil
}

func (m AssignmentMessage) ValidateForRecord(record OnChainJob) error {
	if record.ID != m.JobID || record.Job.Owner != m.JobOwner {
		return ErrComputeMessage
	}
	a, err := record.Job.MarshalBinary()
	if err != nil {
		return err
	}
	b, err := m.Job.MarshalBinary()
	if err != nil || !bytes.Equal(a, b) {
		return ErrComputeMessage
	}
	return m.Validate()
}

type ResultMessage struct {
	JobID    types.JobID
	JobOwner types.AccountID
	JobShard uint32
	Result   Result
}

func (m ResultMessage) Validate() error {
	if types.IsZero32([32]byte(m.JobID)) || types.IsZero32([32]byte(m.JobOwner)) || m.Result.JobID != m.JobID {
		return ErrComputeMessage
	}
	return m.Result.Validate()
}

func (m ResultMessage) MarshalBinary() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	result, _ := m.Result.MarshalBinary()
	var w codec.Writer
	w.Fixed(m.JobID[:])
	w.Fixed(m.JobOwner[:])
	w.U32(m.JobShard)
	w.Bytes(result)
	return w.BytesCopy(), nil
}

func ParseResultMessage(data []byte) (ResultMessage, error) {
	r := codec.NewReader(data)
	jobRaw, err := r.Fixed(32)
	if err != nil {
		return ResultMessage{}, ErrComputeMessage
	}
	owner, err := readAccount(r)
	if err != nil {
		return ResultMessage{}, ErrComputeMessage
	}
	shard, err := r.U32()
	if err != nil {
		return ResultMessage{}, ErrComputeMessage
	}
	resultRaw, err := r.Bytes(2048)
	if err != nil {
		return ResultMessage{}, ErrComputeMessage
	}
	result, err := ParseResult(resultRaw)
	if err != nil || r.Done() != nil {
		return ResultMessage{}, ErrComputeMessage
	}
	var jobID types.JobID
	copy(jobID[:], jobRaw)
	message := ResultMessage{JobID: jobID, JobOwner: owner, JobShard: shard, Result: result}
	if err := message.Validate(); err != nil {
		return ResultMessage{}, err
	}
	return message, nil
}

func (m ResultMessage) Output() (object.OutputSpec, error) {
	raw, err := m.MarshalBinary()
	if err != nil {
		return object.OutputSpec{}, err
	}
	return object.OutputSpec{Owner: m.JobOwner, Kind: object.KindComputeResult, Data: raw}, nil
}

type IngestRef struct {
	JobObject     types.ObjectID
	MessageObject types.ObjectID
}

func (r IngestRef) MarshalBinary() ([]byte, error) {
	if types.IsZero32([32]byte(r.JobObject)) || types.IsZero32([32]byte(r.MessageObject)) || r.JobObject == r.MessageObject {
		return nil, ErrComputeMessage
	}
	var w codec.Writer
	w.Fixed(r.JobObject[:])
	w.Fixed(r.MessageObject[:])
	return w.BytesCopy(), nil
}

func ParseIngestRef(data []byte) (IngestRef, error) {
	if len(data) != 64 {
		return IngestRef{}, ErrComputeMessage
	}
	var out IngestRef
	copy(out.JobObject[:], data[:32])
	copy(out.MessageObject[:], data[32:])
	if _, err := out.MarshalBinary(); err != nil {
		return IngestRef{}, err
	}
	return out, nil
}

type JobRef struct{ JobObject types.ObjectID }

func (r JobRef) MarshalBinary() ([]byte, error) {
	if types.IsZero32([32]byte(r.JobObject)) {
		return nil, ErrComputeMessage
	}
	return append([]byte(nil), r.JobObject[:]...), nil
}

func ParseJobRef(data []byte) (JobRef, error) {
	if len(data) != 32 {
		return JobRef{}, ErrComputeMessage
	}
	var out JobRef
	copy(out.JobObject[:], data)
	if _, err := out.MarshalBinary(); err != nil {
		return JobRef{}, err
	}
	return out, nil
}

type SettlementReceipt struct {
	JobID       types.JobID
	ResultRoot  types.Hash
	Payments    map[types.AccountID]uint64
	Refund      uint64
	Slashed     map[types.AccountID]uint64
	SlashReward uint64
	Expired     bool
}

func (r SettlementReceipt) MarshalBinary() []byte {
	var w codec.Writer
	w.Fixed(r.JobID[:])
	w.Fixed(r.ResultRoot[:])
	writeAccountAmounts(&w, r.Payments)
	w.U64(r.Refund)
	writeAccountAmounts(&w, r.Slashed)
	w.U64(r.SlashReward)
	w.Bool(r.Expired)
	return w.BytesCopy()
}

func writeAccountAmounts(w *codec.Writer, values map[types.AccountID]uint64) {
	ids := make([]types.AccountID, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	w.U32(uint32(len(ids)))
	for _, id := range ids {
		w.Fixed(id[:])
		w.U64(values[id])
	}
}
