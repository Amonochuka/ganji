package ots

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

var (
	ErrSubmissionFailed = errors.New("ots: submission to calendar failed")
	ErrInvalidResponse  = errors.New("ots: invalid calendar response")
	ErrProofNotReady    = errors.New("ots: proof not yet confirmed")
)

const (
	// Public OpenTimestamps calendar servers
	calendarURL1 = "https://a.pool.opentimestamps.org"
	calendarURL2 = "https://b.pool.opentimestamps.org"

	// Calendar API endpoints
	submitPath   = "/digest"
	upgradePath  = "/upgrade"
	proofPath    = "/proof"
)

// CalendarResponse represents the response from an OTS calendar
type CalendarResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	OTS     string `json:"ots,omitempty"` // base64 encoded .ots file
}

// Client submits hashes to OpenTimestamps calendars and retrieves proofs
type Client struct {
	httpClient *http.Client
	calendars  []string
}

// NewClient creates a new OTS client with default public calendars
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		calendars:  []string{calendarURL1, calendarURL2},
	}
}

// Submit submits a hash to OpenTimestamps calendars and returns the initial .ots proof
// The proof will be incomplete until the calendar commits to Bitcoin (hours to days)
func (c *Client) Submit(ctx context.Context, hash []byte) ([]byte, error) {
	hashHex := hex.EncodeToString(hash)

	for _, cal := range c.calendars {
		otsProof, err := c.submitToCalendar(ctx, cal, hashHex)
		if err == nil {
			return otsProof, nil
		}
		// Try next calendar on failure
	}

	return nil, ErrSubmissionFailed
}

func (c *Client) submitToCalendar(ctx context.Context, calendarURL, hashHex string) ([]byte, error) {
	url := calendarURL + submitPath

	reqBody := map[string]string{"digest": hashHex}
	jsonBody, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d: %s", ErrSubmissionFailed, resp.StatusCode, string(body))
	}

	var calResp CalendarResponse
	if err := json.Unmarshal(body, &calResp); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}

	if !calResp.Success || calResp.OTS == "" {
		return nil, fmt.Errorf("%w: %s", ErrInvalidResponse, calResp.Message)
	}

	// Decode base64 OTS file
	otsProof := make([]byte, len(calResp.OTS))
	_, err = hex.Decode(otsProof, []byte(calResp.OTS))
	if err != nil {
		// Try base64 decode
		otsProof = make([]byte, len(calResp.OTS))
		n, err := hex.Decode(otsProof, []byte(calResp.OTS))
		if err != nil {
			return nil, fmt.Errorf("%w: failed to decode ots: %v", ErrInvalidResponse, err)
		}
		otsProof = otsProof[:n]
	}

	return otsProof, nil
}

// Upgrade requests an upgraded proof from calendars (after Bitcoin confirmation)
// Returns the upgraded proof or ErrProofNotReady if not yet confirmed
func (c *Client) Upgrade(ctx context.Context, otsProof []byte) ([]byte, error) {
	for _, cal := range c.calendars {
		upgraded, err := c.upgradeAtCalendar(ctx, cal, otsProof)
		if err == nil {
			return upgraded, nil
		}
		if errors.Is(err, ErrProofNotReady) {
			return nil, ErrProofNotReady
		}
	}
	return nil, ErrSubmissionFailed
}

func (c *Client) upgradeAtCalendar(ctx context.Context, calendarURL string, otsProof []byte) ([]byte, error) {
	url := calendarURL + upgradePath

	// OTS file is binary, send as-is
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(otsProof))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusAccepted {
		// Proof not ready yet
		return nil, ErrProofNotReady
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d: %s", ErrSubmissionFailed, resp.StatusCode, string(body))
	}

	return body, nil
}

// GetProof retrieves the current proof for a hash from calendars
func (c *Client) GetProof(ctx context.Context, hash []byte) ([]byte, error) {
	hashHex := hex.EncodeToString(hash)

	for _, cal := range c.calendars {
		proof, err := c.getProofAtCalendar(ctx, cal, hashHex)
		if err == nil {
			return proof, nil
		}
	}
	return nil, ErrSubmissionFailed
}

func (c *Client) getProofAtCalendar(ctx context.Context, calendarURL, hashHex string) ([]byte, error) {
	url := calendarURL + proofPath + "/" + hashHex

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrProofNotReady
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrSubmissionFailed, resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// VerifyProof verifies an OTS proof against a hash
// Returns the Bitcoin block height and timestamp if valid
func VerifyProof(otsProof []byte, hash []byte) (blockHeight int, timestamp time.Time, err error) {
	// This is a simplified verification - in production you'd use
	// a proper OTS library like github.com/opentimestamps/opentimestamps-go
	// For now, we just check the proof commits to our hash
	
	// Parse the OTS file (simplified - real implementation would parse the full format)
	// The proof should contain our hash in a merkle tree leading to a Bitcoin block
	
	// TODO: Implement full OTS verification with bitcoin block header validation
	// For now return a placeholder
	return 0, time.Time{}, errors.New("ots: full verification not implemented - use opentimestamps-go library")
}

// Hash computes SHA256 of data for OTS submission
func Hash(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}