package ots

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	otspkg "git.intruders.space/public/opentimestamps/ots"
	"git.intruders.space/public/opentimestamps/varn"
)

// serializeCalendarResponse renders seq the way real OpenTimestamps calendar
// servers answer /digest and /timestamp: as a bare timestamp (sequence list
// only — no file header, no digest).
func serializeCalendarResponse(seq otspkg.Sequence) []byte {
	return (&otspkg.File{Sequences: []otspkg.Sequence{seq}}).SerializeInstructionSequences()
}

func pendingSeq(calendarURL string) otspkg.Sequence {
	return otspkg.Sequence{
		{Attestation: &otspkg.Attestation{CalendarServerURL: calendarURL}},
	}
}

func TestSubmitUsesRawDigestProtocol(t *testing.T) {
	hash := bytes.Repeat([]byte{0x11}, sha256.Size)
	var (
		gotAccept string
		gotBody   []byte
	)
	calendarURL := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/digest" {
			t.Errorf("path = %s, want /digest", r.URL.Path)
		}
		gotAccept = r.Header.Get("Accept")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		gotBody = body
		w.Header().Set("Content-Type", "application/vnd.opentimestamps.v1")
		_, _ = w.Write(serializeCalendarResponse(pendingSeq(calendarURL)))
	}))
	defer srv.Close()
	calendarURL = srv.URL

	client := NewClientWithCalendars(calendarURL)
	proof, err := client.Submit(context.Background(), hash)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// The calendar protocol is: raw 32-byte digest in, bare timestamp out.
	if gotAccept != "application/vnd.opentimestamps.v1" {
		t.Errorf("Accept header = %q, want application/vnd.opentimestamps.v1", gotAccept)
	}
	if !bytes.Equal(gotBody, hash) {
		t.Errorf("submitted body = %x, want raw digest %x", gotBody, hash)
	}

	// What we store must be a complete, parseable .ots file carrying the digest.
	file, err := otspkg.ParseOTSFile(varn.NewBuffer(proof))
	if err != nil {
		t.Fatalf("stored proof does not parse as a file: %v", err)
	}
	if !bytes.Equal(file.Digest, hash) {
		t.Errorf("file.Digest = %x, want %x", file.Digest, hash)
	}
	if att := file.Sequences[0].GetAttestation(); att.CalendarServerURL != calendarURL {
		t.Errorf("attestation calendar = %q, want %q", att.CalendarServerURL, calendarURL)
	}
}

func TestSubmitFallsBackToNextCalendar(t *testing.T) {
	hash := bytes.Repeat([]byte{0x33}, sha256.Size)
	goodURL := ""

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer bad.Close()

	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.opentimestamps.v1")
		_, _ = w.Write(serializeCalendarResponse(pendingSeq(goodURL)))
	}))
	defer good.Close()
	goodURL = good.URL

	client := NewClientWithCalendars(bad.URL, good.URL)
	if _, err := client.Submit(context.Background(), hash); err != nil {
		t.Fatalf("Submit should have fallen back to the healthy calendar: %v", err)
	}
}

func TestSubmitRejectsShortHash(t *testing.T) {
	client := NewClient()
	if _, err := client.Submit(context.Background(), []byte("too short")); err == nil {
		t.Fatal("expected error for non-SHA256 digest")
	}
}

func TestUpgradeGETsCommitmentFromCalendar(t *testing.T) {
	digest := bytes.Repeat([]byte{0x22}, sha256.Size)
	serverURL := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		wantPath := "/timestamp/" + hex.EncodeToString(digest)
		if r.URL.Path != wantPath {
			t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
		}
		w.Header().Set("Content-Type", "application/vnd.opentimestamps.v1")
		// A simplified "upgraded" tail: the pending attestation is replaced
		// by this one. (A real tail carries the bitcoin merkle path; the
		// splice logic is identical.)
		_, _ = w.Write(serializeCalendarResponse(pendingSeq(serverURL)))
	}))
	defer srv.Close()
	serverURL = srv.URL

	file := &otspkg.File{
		Digest: digest,
		Sequences: []otspkg.Sequence{
			pendingSeq(srv.URL),
		},
	}
	client := NewClientWithCalendars("https://unused.pool.opentimestamps.org")

	upgraded, err := client.Upgrade(context.Background(), file.SerializeToFile())
	if err != nil {
		t.Fatalf("Upgrade: %v", err)
	}

	out, err := otspkg.ParseOTSFile(varn.NewBuffer(upgraded))
	if err != nil {
		t.Fatalf("upgraded proof does not parse as a file: %v", err)
	}
	// The pending sequence must have been spliced with the calendar's tail.
	if att := out.Sequences[0].GetAttestation(); att.CalendarServerURL != serverURL {
		t.Errorf("upgraded sequence attestation = %+v, want calendar %q", att, serverURL)
	}
}

func TestUpgradePendingReturnsNotReady(t *testing.T) {
	digest := bytes.Repeat([]byte{0x44}, sha256.Size)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Pending confirmation in Bitcoin blockchain", http.StatusNotFound)
	}))
	defer srv.Close()

	file := &otspkg.File{
		Digest: digest,
		Sequences: []otspkg.Sequence{
			pendingSeq(srv.URL),
		},
	}
	client := NewClientWithCalendars(srv.URL)

	_, err := client.Upgrade(context.Background(), file.SerializeToFile())
	if !errors.Is(err, ErrProofNotReady) {
		t.Fatalf("err = %v, want %v", err, ErrProofNotReady)
	}
}

func TestUpgradeRejectsMalformedProof(t *testing.T) {
	client := NewClient()
	if _, err := client.Upgrade(context.Background(), []byte("not a proof")); !errors.Is(err, ErrInvalidProof) {
		t.Fatalf("err = %v, want %v", err, ErrInvalidProof)
	}
}
