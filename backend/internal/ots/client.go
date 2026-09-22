package ots

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	otspkg "git.intruders.space/public/opentimestamps/ots"
	"git.intruders.space/public/opentimestamps/varn"
)

var (
	ErrSubmissionFailed = errors.New("ots: submission to calendar failed")
	ErrInvalidResponse  = errors.New("ots: invalid calendar response")
	ErrProofNotReady    = errors.New("ots: proof not yet confirmed")
	ErrInvalidProof     = errors.New("ots: invalid or malformed proof")
)

const (
	// Public OpenTimestamps calendar pool servers. Pools forward each digest
	// to a member calendar; the returned proof names the calendar that now
	// holds it, which is the server upgrades go to.
	calendarURL1 = "https://a.pool.opentimestamps.org"
	calendarURL2 = "https://b.pool.opentimestamps.org"
)

// Client submits digests to OpenTimestamps calendars and later upgrades the
// resulting proofs from "pending at a calendar" to "confirmed in a Bitcoin
// block". Proofs are stored and returned as complete serialized .ots files.
type Client struct {
	httpClient *http.Client
	calendars  []string
}

// NewClient creates a new OTS client with the default public calendar pools.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		calendars:  []string{calendarURL1, calendarURL2},
	}
}

// NewClientWithCalendars creates an OTS client that submits to the given
// calendar (or pool) servers instead of the public defaults. Useful for tests
// and for operators running their own calendars.
func NewClientWithCalendars(calendars ...string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		calendars:  calendars,
	}
}

// Submit commits a 32-byte digest (the SHA-256 of an artifact) to the Open
// Timestamps calendars and returns a complete serialized .ots proof file. The
// proof starts life with a pending calendar attestation; it becomes bitcoin-
// confirmed once the calendar mines it (see Upgrade).
func (c *Client) Submit(ctx context.Context, hash []byte) ([]byte, error) {
	if len(hash) != sha256Size {
		return nil, fmt.Errorf("ots: digest must be 32 bytes, got %d", len(hash))
	}

	var firstErr error
	for _, cal := range c.calendars {
		seq, err := c.submitToCalendar(ctx, cal, hash)
		if err == nil {
			file := &otspkg.File{Digest: hash, Sequences: []otspkg.Sequence{seq}}
			return file.SerializeToFile(), nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil {
		firstErr = ErrSubmissionFailed
	}
	return nil, firstErr
}

func (c *Client) submitToCalendar(ctx context.Context, calendarURL string, hash []byte) (otspkg.Sequence, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, calendarURL+"/digest", bytes.NewReader(hash))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.opentimestamps.v1")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSubmissionFailed, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProofBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSubmissionFailed, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrSubmissionFailed, resp.StatusCode)
	}

	seqs, err := otspkg.ParseTimestamp(varn.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	if len(seqs) != 1 {
		return nil, fmt.Errorf("%w: expected 1 sequence, got %d", ErrInvalidResponse, len(seqs))
	}
	seq := seqs[0]
	if att := seq.GetAttestation(); att.CalendarServerURL == "" {
		return nil, fmt.Errorf("%w: response has no pending calendar attestation", ErrInvalidResponse)
	}
	return seq, nil
}

// Upgrade asks the calendar that holds a pending proof whether it has been
// mined into a Bitcoin block yet. When it has, the returned proof is the same
// .ots file with the pending sequence replaced by the bitcoin-attested one
// (still serialized as a complete .ots file). While the proof is still
// pending, ErrProofNotReady is returned.
func (c *Client) Upgrade(ctx context.Context, otsProof []byte) ([]byte, error) {
	file, err := parseProofFile(otsProof)
	if err != nil {
		return nil, err
	}

	upgraded := false
	for i, seq := range file.Sequences {
		att := seq.GetAttestation()
		if att.BitcoinBlockHeight > 0 || att.CalendarServerURL == "" {
			continue // already bitcoin-confirmed, or a sequence we cannot upgrade
		}

		commitment, _ := seq.Compute(file.Digest)
		tail, err := c.upgradeAtCalendar(ctx, att.CalendarServerURL, commitment)
		if err != nil {
			return nil, err
		}

		// Splice the calendar's upgraded tail onto the pending sequence,
		// replacing the pending attestation with the real commitment path.
		newSeq := make(otspkg.Sequence, 0, len(seq)+len(tail)-1)
		newSeq = append(newSeq, seq[:len(seq)-1]...)
		newSeq = append(newSeq, tail...)
		file.Sequences[i] = newSeq
		upgraded = true
	}

	if !upgraded {
		// Nothing left to confirm — either already fully upgraded or the file
		// only carries bitcoin attestations. The worker treats this like a
		// pending proof and will simply skip it next cycle.
		return nil, ErrProofNotReady
	}
	return file.SerializeToFile(), nil
}

func (c *Client) upgradeAtCalendar(ctx context.Context, calendarURL string, commitment []byte) (otspkg.Sequence, error) {
	url := calendarURL + "/timestamp/" + hex.EncodeToString(commitment)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.opentimestamps.v1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSubmissionFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// The calendar knows the commitment but has not mined it yet.
		return nil, ErrProofNotReady
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrSubmissionFailed, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProofBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSubmissionFailed, err)
	}

	seqs, err := otspkg.ParseTimestamp(varn.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	if len(seqs) != 1 {
		return nil, fmt.Errorf("%w: expected 1 sequence, got %d", ErrInvalidResponse, len(seqs))
	}
	return seqs[0], nil
}

const (
	sha256Size    = 32
	maxProofBytes = 1 << 20 // 1 MiB — real proofs are a few KB; cap the public endpoints
)

// parseProofFile parses a serialized .ots proof file.
func parseProofFile(otsProof []byte) (*otspkg.File, error) {
	file, err := otspkg.ParseOTSFile(varn.NewBuffer(otsProof))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidProof, err)
	}
	return file, nil
}
