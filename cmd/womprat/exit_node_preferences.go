package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
)

type exitNodePrefsClient interface {
	Status(context.Context) (*ipnstate.Status, error)
	GetPrefs(context.Context) (*ipn.Prefs, error)
	EditPrefs(context.Context, *ipn.MaskedPrefs) (*ipn.Prefs, error)
}

// Resolve the saved name to a stable ID from the authenticated peer snapshot.
// A missing/ambiguous peer is an error, never an instruction to clear routing.
func exitNodePreference(status *ipnstate.Status, name string) (*ipn.MaskedPrefs, error) {
	out := &ipn.MaskedPrefs{Prefs: ipn.Prefs{RouteAll: name != ""}, RouteAllSet: true,
		ExitNodeIDSet: true, ExitNodeIPSet: true, AutoExitNodeSet: true}
	if name == "" {
		return out, nil
	}
	if status == nil {
		return nil, fmt.Errorf("tailnet peer status unavailable")
	}
	wanted := strings.TrimSuffix(strings.ToLower(name), ".")
	var selected *ipnstate.PeerStatus
	for _, peer := range status.Peer {
		if peer == nil {
			continue
		}
		host := strings.TrimSuffix(strings.ToLower(peer.HostName), ".")
		dns := strings.TrimSuffix(strings.ToLower(peer.DNSName), ".")
		if wanted != host && wanted != dns && name != string(peer.ID) {
			continue
		}
		if selected != nil && selected.ID != peer.ID {
			return nil, fmt.Errorf("exit node %q matches multiple peers", name)
		}
		selected = peer
	}
	if selected == nil {
		return nil, fmt.Errorf("exit node %q was not found in the authenticated tailnet peer list", name)
	}
	if !selected.ExitNodeOption {
		return nil, fmt.Errorf("%s is not advertised as an exit node", name)
	}
	if selected.ID == tailcfg.StableNodeID("") {
		return nil, fmt.Errorf("exit node %s has no stable node ID", name)
	}
	out.ExitNodeID = selected.ID
	return out, nil
}

func effectiveExitNode(prefs *ipn.Prefs, status *ipnstate.Status, wanted tailcfg.StableNodeID) error {
	if prefs == nil {
		return fmt.Errorf("Tailscale returned no routing preferences")
	}
	if wanted == "" {
		if prefs.ExitNodeID != "" || prefs.ExitNodeIP.IsValid() || prefs.AutoExitNode.IsSet() {
			return fmt.Errorf("Tailscale did not clear exit-node selection")
		}
		return nil
	}
	if !prefs.RouteAll || prefs.ExitNodeID != wanted || prefs.AutoExitNode.IsSet() {
		return fmt.Errorf("requested exit node %s is not selected in effective Tailscale preferences (ID=%q, IP=%s, acceptRoutes=%t)", wanted, prefs.ExitNodeID, prefs.ExitNodeIP, prefs.RouteAll)
	}
	if status == nil || status.ExitNodeStatus == nil || status.ExitNodeStatus.ID != wanted {
		return fmt.Errorf("exit node %s is selected but has no active exit-node status yet", wanted)
	}
	if !status.ExitNodeStatus.Online {
		return fmt.Errorf("selected exit node %s is offline", wanted)
	}
	return nil
}

func applyAndVerifyExitNode(ctx context.Context, lc exitNodePrefsClient, name string) (result error) {
	var status *ipnstate.Status
	var err error
	if name != "" {
		status, err = lc.Status(ctx)
		if err != nil {
			return fmt.Errorf("read exit-node peer list: %w", err)
		}
	}
	change, err := exitNodePreference(status, name)
	if err != nil {
		return err
	}
	previous, err := lc.GetPrefs(ctx)
	if err != nil || previous == nil {
		return fmt.Errorf("read routing preferences before change: %v", err)
	}
	// Clone: tests and implementations may mutate the returned preference value.
	previous = previous.Clone()
	if _, err = lc.EditPrefs(ctx, change); err != nil {
		return fmt.Errorf("apply exit-node preferences: %w", err)
	}
	defer func() {
		if result == nil {
			return
		}
		// A failed readback must not leave an unpersisted settings change active.
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, rollbackErr := lc.EditPrefs(rollbackCtx, &ipn.MaskedPrefs{Prefs: ipn.Prefs{
			RouteAll: previous.RouteAll, ExitNodeID: previous.ExitNodeID, ExitNodeIP: previous.ExitNodeIP, AutoExitNode: previous.AutoExitNode,
		}, RouteAllSet: true, ExitNodeIDSet: true, ExitNodeIPSet: true, AutoExitNodeSet: true})
		if rollbackErr != nil {
			result = errors.Join(result, fmt.Errorf("restore prior routing preferences: %w", rollbackErr))
		}
	}()
	// EditPrefs success is not proof of effective routing. Allow asynchronous
	// backend updates to settle within the caller's existing bounded deadline.
	for {
		prefs, err := lc.GetPrefs(ctx)
		if err != nil {
			return fmt.Errorf("read effective routing preferences: %w", err)
		}
		if name != "" {
			status, err = lc.Status(ctx)
			if err != nil {
				return fmt.Errorf("read effective exit-node status: %w", err)
			}
		}
		if err = effectiveExitNode(prefs, status, change.ExitNodeID); err == nil {
			return nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%v: %w", err, ctx.Err())
		case <-timer.C:
		}
	}
}
