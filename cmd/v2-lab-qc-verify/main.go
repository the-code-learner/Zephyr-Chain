// Command v2-lab-qc-verify independently verifies a lab QC proof artifact.
// It holds no validator key and never trusts the proposer's "success" flag.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/genesis"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/lightapi"
)

const maxSnapshotJSON = 8 << 20

func verify(genesisPath, snapshotPath, network, root, headerHash string) error {
	g, err := genesis.LoadJSON(genesisPath)
	if err != nil {
		return err
	}
	anchor, err := g.TrustAnchor()
	if err != nil || anchor.Network.String() != network ||
		anchor.ValidatorRoot.String() != root {
		return errors.New("independent lab genesis pins disagree")
	}
	file, err := os.Open(snapshotPath)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxSnapshotJSON+1))
	if err != nil || len(data) == 0 || len(data) > maxSnapshotJSON {
		return errors.New("snapshot proof is empty or too large")
	}
	var snapshot lightapi.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	if err := snapshot.Validate(); err != nil {
		return fmt.Errorf("cryptographic QC proof validation failed: %w", err)
	}
	if snapshot.Header.Network != anchor.Network ||
		snapshot.Header.ValidatorRoot != anchor.ValidatorRoot ||
		snapshot.Header.Height != 1 ||
		v2consensus.HeaderConsensusHash(snapshot.Header).String() != headerHash ||
		len(snapshot.Certificate.Votes) < 2*len(g.Validators)/3+1 {
		return errors.New("QC proof does not match expected lab height, pins and quorum")
	}
	expected, err := g.ValidatorSet()
	if err != nil {
		return err
	}
	if expected.Network != snapshot.Validators.Network ||
		len(expected.Validators) != len(snapshot.Validators.Validators) {
		return errors.New("QC validator roster does not match genesis")
	}
	expectedRoot, err := expected.Root()
	if err != nil || expectedRoot != snapshot.Header.ValidatorRoot {
		return errors.New("QC uses a noncanonical validator roster")
	}
	return nil
}

func main() {
	g := flag.String("genesis", "", "canonical lab genesis JSON")
	s := flag.String("snapshot", "", "signed QC snapshot JSON artifact")
	n := flag.String("network-id", "", "independently pinned network ID")
	r := flag.String("validator-root", "", "independently pinned validator root")
	h := flag.String("header-hash", "", "independently expected finalized consensus header hash")
	flag.Parse()
	if flag.NArg() != 0 || *g == "" || *s == "" || *n == "" || *r == "" || *h == "" {
		fmt.Fprintln(os.Stderr, "all independent lab verifier pins required")
		os.Exit(2)
	}
	if err := verify(*g, *s, *n, *r, *h); err != nil {
		fmt.Fprintln(os.Stderr, "signed QC proof REJECTED:", err)
		os.Exit(1)
	}
	fmt.Println("PASS: independently verified signed V2 QC, genesis pins and height-one header")
}
