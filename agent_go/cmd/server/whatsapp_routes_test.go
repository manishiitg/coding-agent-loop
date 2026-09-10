package server

import (
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
)

// PLAT (WhatsApp device=next / SparkQuill deadlock): a paired primary with no
// unpaired extra slot yet must prepare one even without ?device=next, or
// SparkQuill's status poller (which never sends that param, and only renders
// the pairing image -- the one thing that DOES send device=next -- once
// next_device.qr_available is already true) can never bootstrap.
func TestShouldPrepareNextPairingDevice(t *testing.T) {
	cases := []struct {
		name             string
		deviceQueryParam string
		primaryPaired    bool
		devices          []services.WhatsAppDevice
		want             bool
	}{
		{
			name:             "explicit device=next always prepares, even before primary is paired",
			deviceQueryParam: "next",
			primaryPaired:    false,
			want:             true,
		},
		{
			name:          "unpaired primary with no query param must not create a phantom slot",
			primaryPaired: false,
			want:          false,
		},
		{
			name:          "paired primary with no extra slot yet prepares one (the SparkQuill add-another-parent moment)",
			primaryPaired: true,
			want:          true,
		},
		{
			name:          "paired primary with an existing unpaired extra slot advertises it, does not create another",
			primaryPaired: true,
			devices: []services.WhatsAppDevice{
				{Slot: "", Paired: true},
				{Slot: "phone-2", Paired: false},
			},
			want: false,
		},
		{
			name:          "paired primary where every extra slot is already paired prepares a fresh one",
			primaryPaired: true,
			devices: []services.WhatsAppDevice{
				{Slot: "", Paired: true},
				{Slot: "phone-2", Paired: true},
			},
			want: true,
		},
		{
			name:             "explicit device=next still wins even with an existing unpaired extra slot",
			deviceQueryParam: "  next  ",
			primaryPaired:    true,
			devices: []services.WhatsAppDevice{
				{Slot: "phone-2", Paired: false},
			},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldPrepareNextPairingDevice(tc.deviceQueryParam, tc.primaryPaired, tc.devices)
			if got != tc.want {
				t.Errorf("shouldPrepareNextPairingDevice(%q, %v, %v) = %v, want %v",
					tc.deviceQueryParam, tc.primaryPaired, tc.devices, got, tc.want)
			}
		})
	}
}
