package node

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func witnessTestKeys() ([][]byte,[]ed25519.PrivateKey) {
	keys:=make([][]byte,4)
	privates:=make([]ed25519.PrivateKey,4)
	for i:=range keys {
		seed:=make([]byte,ed25519.SeedSize)
		seed[0]=byte(i+1)
		privates[i]=ed25519.NewKeyFromSeed(seed)
		keys[i]=append([]byte(nil),privates[i].Public().(ed25519.PublicKey)...)
	}
	return keys,privates
}

func signedWitnessTestBundle(t *testing.T,cp FinalityCheckpoint,keys [][]byte,privates []ed25519.PrivateKey,count int) []byte {
	t.Helper()
	digest,err:=WitnessCheckpointDigest(cp)
	if err!=nil{t.Fatal(err)}
	b:=SignedWitnessCheckpoint{NetworkID:hex.EncodeToString(cp.Network[:]),ValidatorRoot:hex.EncodeToString(cp.ValidatorRoot[:]),
		Height:cp.Height,HeaderHash:hex.EncodeToString(cp.HeaderHash[:])}
	for i:=0;i<count;i++{
		b.Signatures=append(b.Signatures,WitnessSignature{
			PublicKey:hex.EncodeToString(keys[i]),
			Signature:hex.EncodeToString(ed25519.Sign(privates[i],digest[:])),
		})
	}
	sort.Slice(b.Signatures,func(i,j int)bool{return b.Signatures[i].PublicKey<b.Signatures[j].PublicKey})
	out,err:=json.Marshal(b)
	if err!=nil{t.Fatal(err)}
	return out
}

func TestWitnessCheckpointSupermajorityVerificationAndRejection(t *testing.T) {
	network:=types.NetworkID(types.HashBytes("witness-test",[]byte("network")))
	root:=types.HashBytes("witness-test",[]byte("validators"))
	cp:=FinalityCheckpoint{Network:network,ValidatorRoot:root,Height:77,HeaderHash:types.Hash{4}}
	keys,privates:=witnessTestKeys()
	valid:=signedWitnessTestBundle(t,cp,keys,privates,3)
	read,err:=ParseSignedWitnessCheckpoint(valid,keys,network,root)
	if err!=nil || *read!=cp{t.Fatalf("3-of-4 independently pinned signatures failed: %+v %v",read,err)}
	bad:=func(label string,raw []byte,trust [][]byte,n types.NetworkID,r types.Hash){
		t.Helper()
		if _,err:=ParseSignedWitnessCheckpoint(raw,trust,n,r);!errors.Is(err,ErrWitnessCheckpoint){
			t.Errorf("%s was accepted: %v",label,err)
		}
	}
	bad("below supermajority",signedWitnessTestBundle(t,cp,keys,privates,2),keys,network,root)
	bad("foreign network",valid,keys,types.NetworkID{9},root)
	bad("foreign validators",valid,keys,network,types.Hash{9})
	bad("substituted trusted keys",valid,keys[1:],network,root)
	bad("duplicate trusted key",valid,[][]byte{keys[0],keys[0],keys[1],keys[2]},network,root)
	bad("too few independent witness keys",valid,keys[:2],network,root)
	bad("unknown JSON field",append(valid[:len(valid)-1],[]byte(`,"extra":true}`)...),keys,network,root)
	bad("noncanonical duplicate JSON",append([]byte("{"),valid[1:]...),keys,network,root)
 	// Build valid JSON for an altered header (the signature stays old).
	modified:=cp
	modified.HeaderHash=types.Hash{5}
	forged:=signedWitnessTestBundle(t,cp,keys,privates,3)
	var env SignedWitnessCheckpoint
	_ = json.Unmarshal(forged,&env)
	env.HeaderHash=hex.EncodeToString(modified.HeaderHash[:])
	mutated,_:=json.Marshal(env)
	bad("altered header digest",mutated,keys,network,root)
	env.HeaderHash=hex.EncodeToString(cp.HeaderHash[:])
	env.Signatures[0].Signature=hex.EncodeToString(make([]byte,ed25519.SignatureSize))
	mutated,_=json.Marshal(env)
	bad("forged individual signature",mutated,keys,network,root)
	env.Signatures=append(env.Signatures[:1],env.Signatures[0])
	mutated,_=json.Marshal(env)
	bad("replayed witness signature",mutated,keys,network,root)
	// Canonical signing bytes cannot be retargeted by changing the network or height.
	changed:=cp;changed.Height++
	d1,_:=WitnessCheckpointDigest(cp);d2,_:=WitnessCheckpointDigest(changed)
	if d1==d2{t.Fatal("witness message not height bound")}
}

func TestWitnessAndExternalHighWaterRequireTwoCertifiedAncestors(t *testing.T) {
	network:=types.NetworkID(types.HashBytes("witness-dual-anchor",[]byte("network")))
	native:=types.TokenID(types.HashBytes("witness-dual-anchor",[]byte("native")))
	key,validators,root:=schedulerValidatorSet(t,network)
	r,roots,journal,history:=testCertifiedHistoryRuntime(t,network,native,root)
	var two,three types.Hash
	for height:=uint64(1);height<=3;height++{
		candidate,err:=r.BuildCandidate(height,nil)
		if err!=nil{t.Fatal(err)}
		commitSchedulerCandidate(t,r,key,validators,candidate)
		if height==2{two=r.ParentHash}
		if height==3{three=r.ParentHash}
	}
	if err:=r.CloseCertifiedHistory();err!=nil{t.Fatal(err)}
 	anchorDir:=t.TempDir()
	anchorPath:=filepath.Join(anchorDir,"high-water")
	anchor,err:=OpenFinalityAnchorStore(anchorPath,network,root,true)
	if err!=nil{t.Fatal(err)}
	defer anchor.Close()
	if err:=anchor.advance(FinalityCheckpoint{Network:network,ValidatorRoot:root,Height:2,HeaderHash:two});err!=nil{t.Fatal(err)}
	recoverOne:=func(witnessHash types.Hash) error {
		// Close and reopen the external anchor between attempts so the
		// exclusive lock still enforces a single owner at any instant.
		res,err:=NewRuntime(network,native,root,r.States,1)
		if err!=nil{return err}
		if err:=res.EnableGlobalCommitJournal(journal);err!=nil{return err}
		if err:=res.EnableCertifiedHistory(history,roots,false);err!=nil{return err}
		defer res.CloseCertifiedHistory()
		if err:=res.EnableFinalityAnchorStore(anchor);err!=nil{return err}
		return res.RecoverCertifiedHistoryWithCheckpoint(validators,map[uint32]worldstate.Backend{0:worldstate.NewMemory()},&FinalityCheckpoint{
			Network:network,ValidatorRoot:root,Height:3,HeaderHash:witnessHash,
		})
	}
	if err:=recoverOne(types.Hash{9});!errors.Is(err,ErrFinalityCheckpoint){t.Fatalf("forged witness ancestor accepted: %v",err)}
	if err:=recoverOne(three);err!=nil{t.Fatalf("distinct heights 2 and 3 should both be verified: %v",err)}
	mark,err:=anchor.Current()
	if err!=nil||mark.Height!=3||mark.HeaderHash!=three{t.Fatalf("externally held mark failed to catch up: %+v %v",mark,err)}
}
