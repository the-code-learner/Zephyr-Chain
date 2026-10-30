//go:build linux || darwin

package node

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func testCertifiedHistoryRuntime(t *testing.T, network types.NetworkID, native types.TokenID, root types.Hash) (*Runtime, map[uint32]types.Hash, string, string) {
	t.Helper()
	diskDir := t.TempDir()
	state := worldstate.NewMemory()
	roots := map[uint32]types.Hash{0:state.Root()}
	r, err := NewRuntime(network,native,root,map[uint32]worldstate.Backend{0:state},1)
	if err != nil { t.Fatal(err) }
	journal := filepath.Join(diskDir,"global-commit.journal")
	history := filepath.Join(diskDir,"certified-history.log")
	if err := r.EnableGlobalCommitJournal(journal); err != nil { t.Fatal(err) }
	if err := r.EnableCertifiedHistory(history,roots,true); err != nil { t.Fatal(err) }
	return r,roots,journal,history
}

func TestCertifiedHistoryReplayEveryBlockAndRefuseMissingTail(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network",[]byte("certified-history")))
	key,validators,root := schedulerValidatorSet(t,network)
	r,roots,journal,history := testCertifiedHistoryRuntime(t,network,types.TokenID(types.HashBytes("token",[]byte("ZPH"))),root)
	for i:=0;i<3;i++ {
		candidate,err := r.BuildCandidate(uint64(i+1),nil)
		if err != nil { t.Fatal(err) }
		commitSchedulerCandidate(t,r,key,validators,candidate)
	}
	last := r.ParentHash
	if err := r.CloseCertifiedHistory(); err != nil { t.Fatal(err) }
	restarted,err := NewRuntime(r.Network,r.NativeToken,r.ValidatorRoot,r.States,1)
	if err != nil { t.Fatal(err) }
	if err := restarted.EnableGlobalCommitJournal(journal); err != nil { t.Fatal(err) }
	if err := restarted.EnableCertifiedHistory(history,roots,false); err != nil { t.Fatal(err) }
	defer restarted.CloseCertifiedHistory()
	if err := restarted.RecoverCertifiedHistory(validators,map[uint32]worldstate.Backend{0:worldstate.NewMemory()}); err != nil {
		t.Fatalf("QC history from genesis failed: %v",err)
	}
	if restarted.Height != 3 || restarted.ParentHash != last || restarted.RecoveryRequired() {
		t.Fatal("multiheight replay recovered wrong finalized tip")
	}
	candidate,err := restarted.BuildCandidate(4,nil)
	if err != nil { t.Fatalf("cannot build next block after recovery: %v",err) }
	commitSchedulerCandidate(t,restarted,key,validators,candidate)
	if restarted.Height != 4 { t.Fatal("recovered runtime did not append next certified block") }
}

func TestCertifiedHistoryRejectsForgedIntermediateSignatureWithValidHashChain(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network",[]byte("certified-history")))
	key,validators,root := schedulerValidatorSet(t,network)
	r,roots,journal,history := testCertifiedHistoryRuntime(t,network,types.TokenID(types.HashBytes("token",[]byte("ZPH"))),root)
	for i:=0;i<2;i++ {
		candidate,err := r.BuildCandidate(uint64(i+1),nil)
		if err != nil { t.Fatal(err) }
		commitSchedulerCandidate(t,r,key,validators,candidate)
	}
	if err := r.CloseCertifiedHistory(); err != nil { t.Fatal(err) }
	raw,err := os.ReadFile(history)
	if err != nil { t.Fatal(err) }
	headerLen := 4+32+32+32+4+32
	var rebuilt bytes.Buffer
	rebuilt.Write(raw[:headerLen])
	pos := headerLen
	var previous types.Hash
	for recordIndex:=0;recordIndex<2;recordIndex++ {
		if pos+4>len(raw) { t.Fatal("test missing history record") }
		size:=int(binary.BigEndian.Uint32(raw[pos:pos+4]))
		pos+=4
		intent,err := parseGlobalCommitIntent(raw[pos:pos+size])
		if err != nil { t.Fatal(err) }
		pos+=size+32
		if recordIndex==0 { intent.Certificate.Votes[0].Signature[0]^=0x01 }
		payload,err := intent.MarshalBinary()
		if err != nil { t.Fatal(err) }
		var chain codec.Writer
		chain.Fixed(previous[:])
		chain.Fixed(payload)
		digest:=types.Hash(codec.DomainHash("zephyr/finality-history-record/v2",chain.BytesCopy()))
		var w codec.Writer
		w.U32(uint32(len(payload)))
		w.Fixed(payload)
		w.Fixed(digest[:])
		rebuilt.Write(w.BytesCopy())
		previous=digest
	}
	if pos!=len(raw) { t.Fatal("test fixture contains unexpected records") }
	if err := os.WriteFile(history,rebuilt.Bytes(),0600); err != nil { t.Fatal(err) }
	restarted,err:=NewRuntime(r.Network,r.NativeToken,r.ValidatorRoot,r.States,1)
	if err != nil { t.Fatal(err) }
	if err := restarted.EnableGlobalCommitJournal(journal); err != nil { t.Fatal(err) }
	if err := restarted.EnableCertifiedHistory(history,roots,false); err != nil { t.Fatal(err) }
	defer restarted.CloseCertifiedHistory()
	if err := restarted.RecoverCertifiedHistory(validators,map[uint32]worldstate.Backend{0:worldstate.NewMemory()}); !errors.Is(err,ErrCertifiedHistory) {
		t.Fatalf("checksum-valid forged QC accepted: %v",err)
	}
}

func TestCertifiedHistoryReplaysChangedObjectsAndChecksCompleteState(t *testing.T) {
	network:=types.NetworkID(types.HashBytes("network",[]byte("history-changes")))
	native:=types.TokenID(types.HashBytes("token",[]byte("ZPH")))
	key,validators,root:=schedulerValidatorSet(t,network)
	old:=systemObjectForShard(0,1,"history-old")
	state:=worldstate.NewMemory()
	_,err:=state.Apply(nil,[]object.Object{old})
	if err!=nil { t.Fatal(err) }
	genesisMemory:=worldstate.NewMemory()
	if _,err:=genesisMemory.Apply(nil,[]object.Object{old}); err!=nil { t.Fatal(err) }
	roots:=map[uint32]types.Hash{0:state.Root()}
	r,err:=NewRuntime(network,native,root,map[uint32]worldstate.Backend{0:state},1)
	if err!=nil { t.Fatal(err) }
	dir:=t.TempDir()
	journal,hist:=filepath.Join(dir,"journal"),filepath.Join(dir,"history")
	if err:=r.EnableGlobalCommitJournal(journal);err!=nil{t.Fatal(err)}
	if err:=r.EnableCertifiedHistory(hist,roots,true);err!=nil{t.Fatal(err)}
	newObj:=systemObjectForShard(0,2,"history-new")
	candidate:=manualCandidateForDeltas(t,r,map[uint32]shardDelta{0:{Consumed:[]types.ObjectID{old.ID},Created:[]object.Object{newObj}}})
	commitSchedulerCandidate(t,r,key,validators,candidate)
	if err:=r.CloseCertifiedHistory();err!=nil{t.Fatal(err)}
	restarted,err:=NewRuntime(network,native,root,map[uint32]worldstate.Backend{0:state},1)
	if err!=nil{t.Fatal(err)}
	if err:=restarted.EnableGlobalCommitJournal(journal);err!=nil{t.Fatal(err)}
	if err:=restarted.EnableCertifiedHistory(hist,roots,false);err!=nil{t.Fatal(err)}
	defer restarted.CloseCertifiedHistory()
	if err:=restarted.RecoverCertifiedHistory(validators,map[uint32]worldstate.Backend{0:genesisMemory});err!=nil{
		t.Fatalf("certified object delta was not replayable: %v",err)
	}
	if restarted.Height!=1||restarted.ParentHash!=r.ParentHash{t.Fatal("wrong anchored final state")}
}
