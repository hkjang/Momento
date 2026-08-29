// Package privacy answers one question — what is the privacy policy — so that
// every screen, tool and worker gets the same answer.
//
// It used to be asked in seven places with seven inline queries, and they did
// not agree. Visitor Explorer's timeline read visitor_profiles with a default of
// true while its profile list and the MCP tool read the same key with a default
// of false, so a policy missing that one field left half the feature working and
// the other half reporting that an administrator had turned it off. Do Not Track
// defaulted to false against a shipped setting of true, which is the difference
// between honouring the header and ignoring it.
//
// The rule this package exists to hold: a field nobody has written is the value
// this product ships, not the zero value of its Go type. Those are opposites for
// every protective setting here — false is "do not anonymise", "" is "do not
// look for PII", nil is "mask nothing" — and a policy that fails open is
// indistinguishable from one an administrator chose.
package privacy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Policy is the privacy settings group. The json tags are the stored keys.
type Policy struct {
	IPAnonymization         bool     `json:"ip_anonymization"`
	CollectUserAgent        bool     `json:"collect_user_agent"`
	StripQueryString        bool     `json:"strip_query_string"`
	MaskedParameters        []string `json:"masked_parameters"`
	CollectUserID           bool     `json:"collect_user_id"`
	VisitorProfiles         bool     `json:"visitor_profiles"`
	DoNotTrack              bool     `json:"do_not_track"`
	BlockedProperties       []string `json:"blocked_properties"`
	PIIDetectionMode        string   `json:"pii_detection_mode"`
	RawEventRetentionMonths int      `json:"raw_event_retention_months"`
	DebugRetentionDays      int      `json:"debug_retention_days"`
}

// Default is what migration 001 seeds and 009 amended. Keep the two together:
// this is the answer for a field the stored policy does not carry, and a value
// that disagrees with the migration would make a fresh install behave
// differently from an upgraded one.
func Default() Policy {
	return Policy{
		IPAnonymization:         true,
		CollectUserAgent:        true,
		StripQueryString:        false,
		MaskedParameters:        []string{"token", "password", "email"},
		CollectUserID:           true,
		VisitorProfiles:         true,
		DoNotTrack:              true,
		BlockedProperties:       []string{"email", "phone", "resident_number"},
		PIIDetectionMode:        "mask",
		RawEventRetentionMonths: 13,
		DebugRetentionDays:      7,
	}
}

// Parse reads a stored policy over the shipped defaults, so a field the stored
// value does not carry keeps the shipped answer rather than falling to zero.
//
// A blob that will not parse is an error, not an empty policy. The collector
// used to unmarshal it with the result discarded, which turned a malformed
// settings row into full IP addresses, unmasked query parameters and no PII
// detection — stored silently, while the console still displayed the policy the
// administrator had set.
func Parse(raw []byte) (Policy, error) {
	policy := Default()
	if len(bytes.TrimSpace(raw)) == 0 {
		return policy, nil
	}
	if err := json.Unmarshal(raw, &policy); err != nil {
		return Default(), err
	}
	return policy, nil
}

// Row is the part of a pool or a transaction this package needs.
type Row interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Load reads the stored policy. A missing row is the shipped policy; anything
// else is a failure to find out, which callers must not read as an answer.
func Load(ctx context.Context, db Row) (Policy, error) {
	var raw []byte
	if err := db.QueryRow(ctx, `SELECT value FROM settings WHERE key='privacy'`).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Default(), nil
		}
		return Default(), err
	}
	return Parse(raw)
}
