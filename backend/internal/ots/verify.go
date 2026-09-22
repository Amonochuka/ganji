package ots

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"git.intruders.space/public/opentimestamps/verifyer"
)

var (
	// ErrNotBitcoinAttested means the proof does not (yet) end in a confirmed
	// Bitcoin attestation — it is still pending at a calendar.
	ErrNotBitcoinAttested = errors.New("ots: proof has no confirmed bitcoin attestation")
	// ErrDigestMismatch means the proof was created for a different digest
	// than the artifact hash being verified.
	ErrDigestMismatch = errors.New("ots: proof does not commit to the given digest")
	// errChainUnavailable wraps chain-source failures (network, node down) so
	// callers can degrade to offline verification instead of failing the page.
	errChainUnavailable = errors.New("ots: bitcoin chain source unavailable")
)

// Chain is a live Bitcoin chain source (an esplora API or a bitcoind RPC
// client) used for full block-header verification. Satisfy it with
// verifyer.NewEsploraClient or — for a local node — write a thin adapter
// around btcd's rpcclient.Client.
type Chain = verifyer.Bitcoin

// Verification is the outcome of checking an OTS proof against a digest.
type Verification struct {
	// BlockHeight is the lowest attested Bitcoin block height in the proof.
	BlockHeight int
	// Timestamp is the block-header time of the confirmed block. Only set
	// when the proof was checked against a live chain (ChainChecked).
	Timestamp time.Time
	// ChainChecked reports whether the commitment was validated against the
	// real Bitcoin chain (vs. purely offline replay of the proof's own
	// transactions and merkle paths).
	ChainChecked bool
	// ChainErr, when non-nil, means chain verification was configured but the
	// chain source could not be reached. The offline proof still verified, so
	// callers normally treat the result as valid and log the issue.
	ChainErr error
}

// VerifyProof verifies a detached OTS proof against the digest it must commit
// to, using only the proof itself (no network access):
//
//  1. parse the .ots file;
//  2. confirm the file's embedded digest equals the artifact hash;
//  3. replay the commitment operations against the embedded Bitcoin
//     transaction(s) — each bitcoin-attested sequence must reproduce a
//     well-formed transaction and a 32-byte block merkle root;
//  4. report the lowest attested block height.
//
// This proves the artifact hash was committed inside the Bitcoin transaction
// the proof embeds. Confirming that transaction actually exists in the block
// at that height requires a live chain source — see Verifier.
func VerifyProof(otsProof, hash []byte) (*Verification, error) {
	if len(hash) != sha256Size {
		return nil, fmt.Errorf("ots: digest must be 32 bytes, got %d", len(hash))
	}

	file, err := parseProofFile(otsProof)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(file.Digest, hash) {
		return nil, ErrDigestMismatch
	}

	confirmed := file.GetBitcoinAttestedSequences()
	if len(confirmed) == 0 {
		return nil, ErrNotBitcoinAttested
	}

	minHeight := 0
	for _, seq := range confirmed {
		att := seq.GetAttestation()
		if att.BitcoinBlockHeight == 0 {
			continue
		}
		root, tx := seq.Compute(hash)
		if tx == nil || len(root) != sha256Size {
			return nil, fmt.Errorf("%w: attested sequence does not embed a bitcoin transaction", ErrInvalidProof)
		}
		h := int(att.BitcoinBlockHeight)
		if minHeight == 0 || h < minHeight {
			minHeight = h
		}
	}
	if minHeight == 0 {
		return nil, ErrNotBitcoinAttested
	}

	return &Verification{BlockHeight: minHeight}, nil
}

// Verifier verifies OTS proofs and, when a chain source is configured, goes
// one step further: it fetches the actual block header for each attested
// height and checks that the proof's committed merkle root really is that
// block's merkle root. A mismatch means the proof is not anchored in Bitcoin
// (calendar bug or forgery) and verification fails. A chain source that is
// merely unreachable degrades to the offline result (ChainErr is set).
type Verifier struct {
	chain Chain
}

// NewVerifier returns a verifier that checks proofs offline only — no chain
// source, no network I/O. This is always safe; all mat passes are validated.
func NewVerifier() *Verifier {
	return &Verifier{}
}

// NewVerifierWithChain returns a verifier that additionally checks attested
// block heights against a live Bitcoin chain source.
func NewVerifierWithChain(chain Chain) *Verifier {
	return &Verifier{chain: chain}
}

// Verify checks the proof against the digest. See VerifyProof for the offline
// component and the struct docs for the chain component.
func (v *Verifier) Verify(otsProof, hash []byte) (*Verification, error) {
	res, err := VerifyProof(otsProof, hash)
	if err != nil {
		return nil, err
	}
	if v.chain == nil {
		return res, nil
	}

	height, ts, err := verifyAgainstChain(v.chain, otsProof, hash)
	if err != nil {
		if errors.Is(err, errChainUnavailable) {
			res.ChainErr = err
			return res, nil
		}
		return nil, err
	}

	res.BlockHeight = height
	res.Timestamp = ts
	res.ChainChecked = true
	return res, nil
}

// verifyAgainstChain validates every bitcoin-attested sequence against the
// live chain: the block at the attested height must have the exact merkle
// root the proof's operations compute. It returns the lowest confirmed height
// and that block's header timestamp.
func verifyAgainstChain(chain Chain, otsProof, hash []byte) (int, time.Time, error) {
	file, err := parseProofFile(otsProof)
	if err != nil {
		return 0, time.Time{}, err
	}

	var fetchErr error
	minHeight := 0
	var bestTS time.Time

	for _, seq := range file.GetBitcoinAttestedSequences() {
		att := seq.GetAttestation()
		if att.BitcoinBlockHeight == 0 {
			continue
		}

		root, _ := seq.Compute(hash)
		blockHash, err := chain.GetBlockHash(int64(att.BitcoinBlockHeight))
		if err != nil {
			if fetchErr == nil {
				fetchErr = fmt.Errorf("ots: block %d: %w", att.BitcoinBlockHeight, err)
			}
			continue
		}
		header, err := chain.GetBlockHeader(blockHash)
		if err != nil {
			if fetchErr == nil {
				fetchErr = fmt.Errorf("ots: header for block %d: %w", att.BitcoinBlockHeight, err)
			}
			continue
		}

		if !bytes.Equal(root, header.MerkleRoot[:]) {
			return 0, time.Time{}, fmt.Errorf(
				"ots: chain verification failed at block %d: proof commits to %x but the block's merkle root is %x",
				att.BitcoinBlockHeight, root, header.MerkleRoot[:])
		}

		h := int(att.BitcoinBlockHeight)
		if minHeight == 0 || h < minHeight {
			minHeight = h
			bestTS = header.Timestamp
		}
	}

	if minHeight == 0 {
		if fetchErr != nil {
			return 0, time.Time{}, fmt.Errorf("%w: %v", errChainUnavailable, fetchErr)
		}
		return 0, time.Time{}, ErrNotBitcoinAttested
	}
	return minHeight, bestTS, nil
}
