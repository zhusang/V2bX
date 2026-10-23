package sing

import (
	"sync"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/sitestat"
	"github.com/InazumaV/V2bX/conf"
)

func setSiteCollector(tag string, config *conf.Options) {
	if config != nil && config.SiteTraffic.Enable {
		sitestat.Enable(tag, sitestat.Options{
			Granularity:     config.SiteTraffic.Granularity,
			MaxSitesPerUser: config.SiteTraffic.MaxSitesPerUser,
		})
		return
	}
	sitestat.Disable(tag)
}

func siteTrafficFor(tag string, uidMap map[string]int, mu *sync.RWMutex) ([]panel.SiteTraffic, error) {
	col := sitestat.Get(tag)
	if col == nil {
		return nil, nil
	}
	entries := col.Harvest()
	mu.RLock()
	defer mu.RUnlock()
	out := make([]panel.SiteTraffic, 0, len(entries))
	for _, e := range entries {
		uid := uidMap[e.User]
		if uid == 0 {
			continue
		}
		out = append(out, panel.SiteTraffic{
			UID:      uid,
			Site:     e.Site,
			Upload:   e.Upload,
			Download: e.Download,
			Conns:    int(e.Conns),
		})
	}
	return out, nil
}
