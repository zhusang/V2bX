package node

import (
	"errors"
	"testing"

	"github.com/InazumaV/V2bX/api/panel"
)

func TestSiteReportRetriesSameBatchAndStopsOn404(t *testing.T) {
	var sent []string
	batch := panel.SiteTrafficBatch{BatchID: "b1", Items: []panel.SiteTraffic{{UID: 1, Site: "a.com", Download: 5}}}
	state := siteReportState{queue: []panel.SiteTrafficBatch{batch}}
	state.run(
		func() (int64, int64) { return 1, 2 },
		func() ([]panel.SiteTraffic, error) {
			t.Fatal("harvest should not run while a retry is pending")
			return nil, nil
		},
		func(b panel.SiteTrafficBatch) error {
			sent = append(sent, b.BatchID)
			return errors.New("unavailable")
		},
		nil,
	)
	if len(sent) != 1 || sent[0] != "b1" {
		t.Fatalf("sent = %v", sent)
	}
	if len(state.queue) != 1 || state.queue[0].BatchID != "b1" {
		t.Fatalf("queue = %+v", state.queue)
	}

	state.run(
		func() (int64, int64) { return 1, 2 },
		func() ([]panel.SiteTraffic, error) { return nil, nil },
		func(b panel.SiteTrafficBatch) error { return panel.ErrSiteTrafficUnsupported },
		func(string) {},
	)
	if !state.disabled || len(state.queue) != 0 {
		t.Fatalf("state after 404: disabled=%v queue=%d", state.disabled, len(state.queue))
	}
	state.run(
		func() (int64, int64) { return 1, 2 },
		func() ([]panel.SiteTraffic, error) {
			t.Fatal("disabled reporter harvested")
			return nil, nil
		},
		func(panel.SiteTrafficBatch) error {
			t.Fatal("disabled reporter sent")
			return nil
		},
		nil,
	)
}

func TestSiteReportQueueDropsOldest(t *testing.T) {
	items := make([]panel.SiteTraffic, siteBatchLimit+1)
	for i := range items {
		items[i] = panel.SiteTraffic{UID: 1, Site: "s", Download: 1}
	}
	var failed int
	state := siteReportState{}
	state.run(
		func() (int64, int64) { return 10, 20 },
		func() ([]panel.SiteTraffic, error) { return items, nil },
		func(panel.SiteTrafficBatch) error {
			failed++
			return errors.New("down")
		},
		nil,
	)
	if failed != 1 {
		t.Fatalf("send calls = %d, want 1 (rest queued unsent)", failed)
	}
	if len(state.queue) != 2 {
		t.Fatalf("queue len = %d, want 2", len(state.queue))
	}

	state.queue = nil
	for i := 0; i < siteQueueLimit+2; i++ {
		state.queue = append(state.queue, panel.SiteTrafficBatch{BatchID: string(rune('a' + i))})
	}
	got := trimSiteQueue(state.queue)
	if len(got) != siteQueueLimit {
		t.Fatalf("trimmed len = %d", len(got))
	}
	if got[0].BatchID == state.queue[0].BatchID {
		t.Fatal("oldest batch was kept")
	}
}

func TestSiteReportDropsRejectedBatch(t *testing.T) {
	bad := panel.SiteTrafficBatch{BatchID: "bad"}
	good := panel.SiteTrafficBatch{BatchID: "good"}
	state := siteReportState{queue: []panel.SiteTrafficBatch{bad, good}}
	var sent []string
	harvested := false
	state.run(
		func() (int64, int64) { return 1, 2 },
		func() ([]panel.SiteTraffic, error) {
			harvested = true
			return nil, nil
		},
		func(b panel.SiteTrafficBatch) error {
			sent = append(sent, b.BatchID)
			if b.BatchID == "bad" {
				return panel.ErrSiteTrafficRejected
			}
			return nil
		},
		func(string) {},
	)
	if len(sent) != 2 || sent[1] != "good" {
		t.Fatalf("sent = %v, rejected batch must not block the queue", sent)
	}
	if len(state.queue) != 0 || state.disabled || !harvested {
		t.Fatalf("queue=%d disabled=%v harvested=%v", len(state.queue), state.disabled, harvested)
	}
}

func TestSplitSiteBatches(t *testing.T) {
	items := make([]panel.SiteTraffic, 12000)
	batches := splitSiteBatches(items, 1, 2)
	if len(batches) != 3 {
		t.Fatalf("batches = %d, want 3", len(batches))
	}
	seen := map[string]bool{}
	for _, b := range batches {
		if len(b.Items) > siteBatchLimit {
			t.Fatalf("batch %s has %d items", b.BatchID, len(b.Items))
		}
		if seen[b.BatchID] {
			t.Fatal("duplicate batch id")
		}
		seen[b.BatchID] = true
	}
}
