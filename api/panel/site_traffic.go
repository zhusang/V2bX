package panel

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ErrSiteTrafficUnsupported means the panel has no site-traffic endpoint.
var ErrSiteTrafficUnsupported = errors.New("site traffic endpoint not found")

// ErrSiteTrafficRejected means the panel refused the batch content.
// Resending the same batch will not succeed.
var ErrSiteTrafficRejected = errors.New("site traffic batch rejected")

// SiteTraffic is one user's traffic on one site during a report period.
type SiteTraffic struct {
	UID      int
	Site     string
	Upload   int64
	Download int64
	Conns    int
}

// SiteTrafficBatch is one idempotent report.
type SiteTrafficBatch struct {
	BatchID     string
	PeriodStart int64
	PeriodEnd   int64
	Items       []SiteTraffic
}

type siteItem struct {
	UID      int
	Site     string
	Upload   int64
	Download int64
	Conns    int
}

func (s siteItem) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{s.UID, s.Site, s.Upload, s.Download, s.Conns})
}

// ReportSiteTraffic posts a site-traffic batch. A 404 returns ErrSiteTrafficUnsupported.
func (c *Client) ReportSiteTraffic(batch SiteTrafficBatch) error {
	items := make([]siteItem, len(batch.Items))
	for i, it := range batch.Items {
		items[i] = siteItem{it.UID, it.Site, it.Upload, it.Download, it.Conns}
	}
	body := map[string]any{
		"batch_id":     batch.BatchID,
		"period_start": batch.PeriodStart,
		"period_end":   batch.PeriodEnd,
		"items":        items,
	}
	const path = "/api/v1/server/site-traffic/push"
	r, err := c.client.R().
		SetBody(body).
		ForceContentType("application/json").
		Post(path)
	if err == nil && r != nil {
		switch r.StatusCode() {
		case http.StatusNotFound:
			return ErrSiteTrafficUnsupported
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			return fmt.Errorf("%w: %s", ErrSiteTrafficRejected, string(r.Body()))
		}
	}
	return c.checkResponse(r, path, err)
}
