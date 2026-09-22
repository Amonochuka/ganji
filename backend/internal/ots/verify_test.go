package ots

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"testing"
	"time"

	otspkg "git.intruders.space/public/opentimestamps/ots"
	"git.intruders.space/public/opentimestamps/varn"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

// fixtureBlockHeight is the attested Bitcoin block height embedded in the
// testdata .ots proof (a real proof from the OpenTimestamps project).
const fixtureBlockHeight = 891686

// fixtureFileHash returns the SHA-256 of the fixture text file — the digest
// the fixture .ots proof was created for.
func fixtureFileHash(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/flatearthers-united.txt")
	if err != nil {
		t.Fatalf("read fixture file: %v", err)
	}
	sum := sha256.Sum256(data)
	return sum[:]
}

func fixtureProof(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/flatearthers-united.txt.ots")
	if err != nil {
		t.Fatalf("read fixture proof: %v", err)
	}
	return data
}

func fixtureComputedRoot(t *testing.T) ([]byte, *wire.MsgTx) {
	t.Helper()
	file, err := otspkg.ParseOTSFile(varn.NewBuffer(fixtureProof(t)))
	if err != nil {
		t.Fatalf("parse fixture proof: %v", err)
	}
	seqs := file.GetBitcoinAttestedSequences()
	if len(seqs) == 0 {
		t.Fatal("fixture proof has no bitcoin-attested sequence")
	}
	root, tx := seqs[0].Compute(fixtureFileHash(t))
	return root, tx
}

// pendingProof builds a serialized .ots file whose sole sequence ends in a
// pending calendar attestation — the shape a proof has between submission
// and the calendar mining it into a Bitcoin block.
func pendingProof(t *testing.T, digest []byte) []byte {
	t.Helper()
	file := &otspkg.File{
		Digest: digest,
		Sequences: []otspkg.Sequence{
			{
				{Attestation: &otspkg.Attestation{CalendarServerURL: "https://alice.btc.calendar.opentimestamps.org"}},
			},
		},
	}
	return file.SerializeToFile()
}

func TestVerifyProofConfirmed(t *testing.T) {
	hash := fixtureFileHash(t)
	proof := fixtureProof(t)

	res, err := VerifyProof(proof, hash)
	if err != nil {
		t.Fatalf("VerifyProof: %v", err)
	}
	if res.BlockHeight != fixtureBlockHeight {
		t.Errorf("BlockHeight = %d, want %d", res.BlockHeight, fixtureBlockHeight)
	}
	if res.ChainChecked {
		t.Error("offline verification must not report ChainChecked")
	}
	if !res.Timestamp.IsZero() {
		t.Errorf("offline verification must not set Timestamp, got %v", res.Timestamp)
	}
}

func TestVerifyProofWrongDigest(t *testing.T) {
	proof := fixtureProof(t)

	res, err := VerifyProof(proof, bytes.Repeat([]byte{0x42}, sha256.Size))
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("err = %v, want %v", err, ErrDigestMismatch)
	}
	if res != nil {
		t.Errorf("res = %v, want nil", res)
	}
}

func TestVerifyProofPending(t *testing.T) {
	hash := fixtureFileHash(t)
	proof := pendingProof(t, hash)

	_, err := VerifyProof(proof, hash)
	if !errors.Is(err, ErrNotBitcoinAttested) {
		t.Fatalf("err = %v, want %v", err, ErrNotBitcoinAttested)
	}
}

func TestVerifyProofGarbage(t *testing.T) {
	hash := fixtureFileHash(t)

	_, err := VerifyProof([]byte("this is not an ots proof"), hash)
	if !errors.Is(err, ErrInvalidProof) {
		t.Fatalf("err = %v, want %v", err, ErrInvalidProof)
	}
}

func TestVerifyProofShortHash(t *testing.T) {
	_, err := VerifyProof(fixtureProof(t), []byte("too short"))
	if err == nil {
		t.Fatal("expected error for non-SHA256 digest")
	}
}

func TestVerifierOffline(t *testing.T) {
	v := NewVerifier()

	res, err := v.Verify(fixtureProof(t), fixtureFileHash(t))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.BlockHeight != fixtureBlockHeight {
		t.Errorf("BlockHeight = %d, want %d", res.BlockHeight, fixtureBlockHeight)
	}
	if res.ChainChecked {
		t.Error("verifier without chain must not report ChainChecked")
	}
}

// fakeChain is an in-memory Chain that returns a fixed block header for every
// height. The merkle root is what full chain verification compares against.
type fakeChain struct {
	root []byte
	ts   time.Time
	err  error
}

func (f *fakeChain) GetBlockHash(height int64) (*chainhash.Hash, error) {
	if f.err != nil {
		return nil, f.err
	}
	var h chainhash.Hash
	copy(h[:], "00000000-fake-block-hash")
	return &h, nil
}

func (f *fakeChain) GetBlockHeader(hash *chainhash.Hash) (*wire.BlockHeader, error) {
	if f.err != nil {
		return nil, f.err
	}
	var root chainhash.Hash
	copy(root[:], f.root)
	return &wire.BlockHeader{MerkleRoot: root, Timestamp: f.ts}, nil
}

func TestVerifierChainConfirms(t *testing.T) {
	hash := fixtureFileHash(t)
	proof := fixtureProof(t)
	root, tx := fixtureComputedRoot(t)
	if tx == nil {
		t.Fatal("fixture sequence does not embed a bitcoin transaction")
	}

	blockTime := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	v := NewVerifierWithChain(&fakeChain{root: root, ts: blockTime})

	res, err := v.Verify(proof, hash)
	if err != nil {
		t.Fatalf("chain verification: %v", err)
	}
	if !res.ChainChecked {
		t.Error("expected ChainChecked after chain verification")
	}
	if res.BlockHeight != fixtureBlockHeight {
		t.Errorf("BlockHeight = %d, want %d", res.BlockHeight, fixtureBlockHeight)
	}
	if !res.Timestamp.Equal(blockTime) {
		t.Errorf("Timestamp = %v, want %v", res.Timestamp, blockTime)
	}
}

func TestVerifierChainMismatchFails(t *testing.T) {
	hash := fixtureFileHash(t)
	proof := fixtureProof(t)

	// Wrong merkle root: the block at the attested height does not contain
	// the committed transaction — the proof is not anchored in Bitcoin.
	v := NewVerifierWithChain(&fakeChain{root: bytes.Repeat([]byte{0xAB}, sha256.Size)})

	if _, err := v.Verify(proof, hash); err == nil {
		t.Fatal("expected verification failure on merkle root mismatch")
	}
}

func TestVerifierChainUnavailableDegrades(t *testing.T) {
	hash := fixtureFileHash(t)
	proof := fixtureProof(t)

	v := NewVerifierWithChain(&fakeChain{err: errors.New("bitcoin node unreachable")})

	res, err := v.Verify(proof, hash)
	if err != nil {
		t.Fatalf("expected graceful degrade, got error: %v", err)
	}
	if res.BlockHeight != fixtureBlockHeight {
		t.Errorf("BlockHeight = %d, want %d", res.BlockHeight, fixtureBlockHeight)
	}
	if res.ChainChecked {
		t.Error("degraded result must not report ChainChecked")
	}
	if !errors.Is(res.ChainErr, errChainUnavailable) {
		t.Errorf("ChainErr = %v, want wrapped %v", res.ChainErr, errChainUnavailable)
	}
}
