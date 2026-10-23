package panel

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/InazumaV/V2bX/conf"
)

func TestReportSiteTraffic(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "tok" || r.URL.Query().Get("node_id") != "21" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := New(&conf.ApiConfig{APIHost: srv.URL, Key: "tok", NodeType: "vless", NodeID: 21})
	if err != nil {
		t.Fatal(err)
	}
	err = c.ReportSiteTraffic(SiteTrafficBatch{
		BatchID: "abc", PeriodStart: 1, PeriodEnd: 2,
		Items: []SiteTraffic{{UID: 3, Site: "googlevideo.com", Upload: 4, Download: 5, Conns: 6}},
	})
	if err != nil {
		t.Fatal(err)
	}
	items, _ := got["items"].([]any)
	if got["batch_id"] != "abc" || len(items) != 1 {
		t.Fatalf("body = %+v", got)
	}
}

func TestReportSiteTrafficNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c, err := New(&conf.ApiConfig{APIHost: srv.URL, Key: "tok", NodeType: "vless", NodeID: 1})
	if err != nil {
		t.Fatal(err)
	}
	err = c.ReportSiteTraffic(SiteTrafficBatch{BatchID: "x"})
	if err != ErrSiteTrafficUnsupported {
		t.Fatalf("err = %v", err)
	}
}

func TestReportSiteTrafficRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer srv.Close()
	c, err := New(&conf.ApiConfig{APIHost: srv.URL, Key: "tok", NodeType: "vless", NodeID: 1})
	if err != nil {
		t.Fatal(err)
	}
	err = c.ReportSiteTraffic(SiteTrafficBatch{BatchID: "x"})
	if !errors.Is(err, ErrSiteTrafficRejected) {
		t.Fatalf("err = %v", err)
	}
}
