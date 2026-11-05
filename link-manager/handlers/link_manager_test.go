package handlers

import (
	"math"
	"testing"
	"time"

	authmodels "site/auth/models"
)

const day = 24 * time.Hour

func TestExpirationCapAppliesToEveryUnit(t *testing.T) {
	regular := &authmodels.User{Email: "someone@example.com"}

	tests := []struct {
		name    string
		user    *authmodels.User
		value   int
		unit    string
		want    time.Duration
		wantErr bool
	}{
		{name: "90 days allowed", user: regular, value: 90, unit: "days", want: 90 * day},
		{name: "91 days rejected", user: regular, value: 91, unit: "days", wantErr: true},
		{name: "90 days in hours allowed", user: regular, value: 2160, unit: "hr", want: 90 * day},
		{name: "one hour over rejected", user: regular, value: 2161, unit: "hr", wantErr: true},
		{name: "90 days in minutes allowed", user: regular, value: 129600, unit: "min", want: 90 * day},
		{name: "one minute over rejected", user: regular, value: 129601, unit: "min", wantErr: true},
		{name: "anonymous user is capped", user: nil, value: 2161, unit: "hr", wantErr: true},
		{name: "overflowing hours rejected", user: regular, value: math.MaxInt, unit: "hr", wantErr: true},
		{name: "overflowing minutes rejected", user: regular, value: math.MaxInt, unit: "min", wantErr: true},
		{name: "zero rejected", user: regular, value: 0, unit: "days", wantErr: true},
		{name: "negative rejected", user: regular, value: -1, unit: "hr", wantErr: true},
		{name: "unknown unit rejected", user: regular, value: 1, unit: "weeks", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveExpiration(tt.user, false, tt.value, tt.unit)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got duration %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNeverExpireIsOwnerOnly(t *testing.T) {
	owner := &authmodels.User{Email: ownerEmail}
	got, err := resolveExpiration(owner, true, 0, "")
	if err != nil {
		t.Fatalf("owner: unexpected error: %v", err)
	}
	if want := 36500 * day; got != want {
		t.Fatalf("owner: got %v, want %v", got, want)
	}

	for name, user := range map[string]*authmodels.User{
		"regular user": {Email: "someone@example.com"},
		"anonymous":    nil,
	} {
		if got, err := resolveExpiration(user, true, 90, "days"); err == nil {
			t.Errorf("%s: expected an error, got duration %v", name, got)
		}
	}
}
