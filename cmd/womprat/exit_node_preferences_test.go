package main

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"
)

func relayStatus() *ipnstate.Status {
	return &ipnstate.Status{Peer: map[key.NodePublic]*ipnstate.PeerStatus{
		{}: {ID: "relay-stable-id", HostName: "relay", DNSName: "relay.example.ts.net.", ExitNodeOption: true, TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.2")}},
	}, ExitNodeStatus: &ipnstate.ExitNodeStatus{ID: "relay-stable-id", Online: true}}
}

func TestExitNodePreferenceUsesStableIDWithoutChangingDNS(t *testing.T) {
	for _, name := range []string{"relay", "relay.example.ts.net.", "RELAY", "relay-stable-id"} {
		change, err := exitNodePreference(relayStatus(), name)
		if err != nil {
			t.Fatal(err)
		}
		if change.ExitNodeID != "relay-stable-id" || !change.ExitNodeIDSet || change.ExitNodeIP.IsValid() || !change.ExitNodeIPSet || !change.RouteAll || !change.RouteAllSet || !change.AutoExitNodeSet || change.AutoExitNode.IsSet() {
			t.Fatal("wrong selection", change)
		}
		if change.CorpDNSSet || change.WantRunningSet || change.AdvertiseRoutesSet {
			t.Fatal("unrelated prefs changed")
		}
	}
}

func TestExitNodePreferenceDoesNotClearOnMissingOrUnapprovedPeer(t *testing.T) {
	for _, name := range []string{"missing", ""} {
		status := relayStatus()
		if name == "" {
			for _, p := range status.Peer {
				p.ExitNodeOption = false
			}
		}
		if name == "" {
			name = "relay"
		}
		if _, err := exitNodePreference(status, name); err == nil {
			t.Fatal("unsafe selection accepted")
		}
	}
	change, err := exitNodePreference(nil, "")
	if err != nil || change.ExitNodeID != "" || change.ExitNodeIP.IsValid() || change.RouteAll {
		t.Fatal("clear rejected")
	}
}

type fakeExitPrefs struct {
	status     *ipnstate.Status
	prefs      *ipn.Prefs
	change     *ipn.MaskedPrefs
	ignoreEdit bool
	reads      int
	editErr    error
}

func (f *fakeExitPrefs) Status(context.Context) (*ipnstate.Status, error) { return f.status, nil }
func (f *fakeExitPrefs) GetPrefs(context.Context) (*ipn.Prefs, error)     { f.reads++; return f.prefs, nil }
func (f *fakeExitPrefs) EditPrefs(_ context.Context, change *ipn.MaskedPrefs) (*ipn.Prefs, error) {
	f.change = change
	if f.editErr != nil {
		return nil, f.editErr
	}
	if !f.ignoreEdit {
		f.prefs.ApplyEdits(change)
	}
	return f.prefs, nil
}

func TestExitNodeEditSuccessMustReadBackEffectiveSelection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	f := &fakeExitPrefs{status: relayStatus(), prefs: &ipn.Prefs{RouteAll: true}, ignoreEdit: true}
	err := applyAndVerifyExitNode(ctx, f, "relay")
	if err == nil || !strings.Contains(err.Error(), "not selected") || f.reads == 0 {
		t.Fatal("empty effective ID accepted as active", err)
	}
	f.ignoreEdit = false
	if err := applyAndVerifyExitNode(context.Background(), f, "relay"); err != nil {
		t.Fatal(err)
	}
	if f.prefs.ExitNodeID != "relay-stable-id" {
		t.Fatal("stable ID not applied")
	}
}

func TestExitNodeSelectionDoesNotClaimOfflineStatusActive(t *testing.T) {
	for _, mode := range []string{"missing-status", "offline", "wrong-peer"} {
		t.Run(mode, func(t *testing.T) {
			f := &fakeExitPrefs{status: relayStatus(), prefs: &ipn.Prefs{}}
			switch mode {
			case "missing-status":
				f.status.ExitNodeStatus = nil
			case "offline":
				f.status.ExitNodeStatus.Online = false
			case "wrong-peer":
				f.status.ExitNodeStatus.ID = "other"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			if err := applyAndVerifyExitNode(ctx, f, "relay"); err == nil {
				t.Fatal("unavailable route accepted")
			}
		})
	}
	f := &fakeExitPrefs{status: relayStatus(), prefs: &ipn.Prefs{}, editErr: errors.New("policy denies exit nodes")}
	if err := applyAndVerifyExitNode(context.Background(), f, "relay"); err == nil || !strings.Contains(err.Error(), "policy denies") {
		t.Fatal("edit error hidden", err)
	}
}

func TestExitNodeFailedVerificationRestoresPriorPrefs(t *testing.T) {
	status := relayStatus()
	status.ExitNodeStatus.Online = false
	f := &fakeExitPrefs{status: status, prefs: &ipn.Prefs{ExitNodeID: "previous-exit", RouteAll: true, CorpDNS: true}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := applyAndVerifyExitNode(ctx, f, "relay"); err == nil {
		t.Fatal("offline route accepted")
	}
	if f.prefs.ExitNodeID != "previous-exit" || !f.prefs.RouteAll || !f.prefs.CorpDNS {
		t.Fatal("failed verification left an unpersisted route change", f.prefs)
	}
}

func TestExitNodeClearDoesNotNeedCurrentPeerList(t *testing.T) {
	f := &fakeExitPrefs{prefs: &ipn.Prefs{ExitNodeID: "old", RouteAll: true}}
	if err := applyAndVerifyExitNode(context.Background(), f, ""); err != nil {
		t.Fatal(err)
	}
	if f.prefs.ExitNodeID != "" || f.prefs.RouteAll {
		t.Fatal("explicit clear ineffective")
	}
}
