package scan

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/shawngmc/chrome-polish/app/internal/cdp"
)

// StorageOrigin is one origin's entry within a SiteGroup, as reported by
// chrome://settings/content/all's own data model (the same view backing
// the screenshots in the "cookies still there" bug report). Unlike
// DiscoverOrigins, this surfaces origins with no cookie and no service
// worker at all — pure localStorage/cache usage — and flags exactly which
// origins are storage-partitioned (see PartitionedStorageNote below).
type StorageOrigin struct {
	Origin                string
	Usage                 int64 // bytes
	NumCookies            int
	IsPartitioned         bool
	HasPermissionSettings bool
}

// SiteGroup is one eTLD+1 grouping as chrome://settings/content/all
// displays it (its own top-level row, like "alza.de" in the report),
// containing every origin — first-party and partitioned third-party
// embeds alike — Chrome attributes to it. GroupingKey is Chrome's own
// opaque internal identifier for the group (e.g. "etld:alza.de") — the
// same key its "remove this site" action passes to
// clearSiteGroupDataAndCookies, and the only reliable handle for removal
// package (identifying a group by displayName/ETLDPlus1 text isn't safe:
// unlike GroupingKey, those aren't guaranteed unique or stable).
type SiteGroup struct {
	DisplayName string
	ETLDPlus1   string
	GroupingKey string
	NumCookies  int
	Origins     []StorageOrigin
}

const allSitesURL = "chrome://settings/content/all"

// allSitesDataScript reads the <all-sites> WebUI element's own filteredList_
// property directly, rather than scraping its rendered rows. That element
// backs its list with an `iron-list`, which only ever renders however many
// rows fit the viewport (see DESIGN.md section 4.2 and
// cdp-remote-debugging-quirks item 4) — reading the underlying data model
// instead sidesteps that entirely and returns every site group Chrome
// knows about in one shot, partition flags included, with no scrolling or
// search-box interaction needed. filteredList_ is a "private" (underscore
// convention only, not real encapsulation) Polymer/Lit property, so this is
// still reading application state through its normal JS object model, not
// crossing a closed shadow boundary or driving synthetic UI input.
const allSitesDataScript = `
(function() {
  function findByTag(root, tag) {
    if (root.tagName && root.tagName.toLowerCase() === tag) return root;
    var kids = root.shadowRoot ? Array.from(root.shadowRoot.children).concat(Array.from(root.children)) : Array.from(root.children || []);
    for (var i = 0; i < kids.length; i++) {
      var r = findByTag(kids[i], tag);
      if (r) return r;
    }
    return null;
  }
  var el = findByTag(document.documentElement, 'all-sites');
  if (!el) return JSON.stringify({error: 'all-sites element not found'});
  return JSON.stringify({groups: el.filteredList_ || []});
})()
`

type siteDataPayload struct {
	Error  string `json:"error"`
	Groups []struct {
		DisplayName string `json:"displayName"`
		ETLDPlus1   string `json:"etldPlus1"`
		GroupingKey string `json:"groupingKey"`
		NumCookies  int    `json:"numCookies"`
		Origins     []struct {
			Origin                string `json:"origin"`
			Usage                 int64  `json:"usage"`
			NumCookies            int    `json:"numCookies"`
			IsPartitioned         bool   `json:"isPartitioned"`
			HasPermissionSettings bool   `json:"hasPermissionSettings"`
		} `json:"origins"`
	} `json:"groups"`
}

// DiscoverSiteData reads every site group (and every origin within it,
// partitioned or not) from chrome://settings/content/all — a local, static
// Chrome page, never a candidate/suspect origin. This is the storage-usage
// counterpart to DiscoverOrigins/DiscoverPermissions: it's the only signal
// covering origins with storage but neither a cookie nor a service worker
// (DiscoverOrigins can't see those at all), and the only signal that knows
// which origins are storage-partitioned under a top-level site (relevant
// to removal.ClearOrigin's cookie-partition handling, and to the
// non-cookie partitioned storage that has no CDP removal path at all yet
// — see removal package docs).
func DiscoverSiteData(ctx context.Context, client *cdp.Client) ([]SiteGroup, error) {
	sessionID, cleanup, err := cdp.AttachTarget(ctx, client, allSitesURL, false)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	defer cleanup()

	select {
	case <-time.After(3 * time.Second):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	var evalResult struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := client.CallSession(ctx, sessionID, "Runtime.evaluate", map[string]any{
		"expression":    allSitesDataScript,
		"returnByValue": true,
	}, &evalResult); err != nil {
		return nil, fmt.Errorf("scan: Runtime.evaluate on %s: %w", allSitesURL, err)
	}
	if evalResult.ExceptionDetails != nil {
		return nil, fmt.Errorf("scan: script threw on %s: %s", allSitesURL, evalResult.ExceptionDetails.Text)
	}

	var payload siteDataPayload
	if err := json.Unmarshal([]byte(evalResult.Result.Value), &payload); err != nil {
		return nil, fmt.Errorf("scan: decode all-sites data: %w", err)
	}
	if payload.Error != "" {
		return nil, fmt.Errorf("scan: %s", payload.Error)
	}

	groups := make([]SiteGroup, 0, len(payload.Groups))
	for _, g := range payload.Groups {
		sg := SiteGroup{DisplayName: g.DisplayName, ETLDPlus1: g.ETLDPlus1, GroupingKey: g.GroupingKey, NumCookies: g.NumCookies}
		for _, o := range g.Origins {
			origin := normalizeOrigin(o.Origin)
			if origin == "" {
				continue
			}
			sg.Origins = append(sg.Origins, StorageOrigin{
				Origin:                origin,
				Usage:                 o.Usage,
				NumCookies:            o.NumCookies,
				IsPartitioned:         o.IsPartitioned,
				HasPermissionSettings: o.HasPermissionSettings,
			})
		}
		groups = append(groups, sg)
	}
	return groups, nil
}

// MergeSiteData folds groups' non-partitioned origins into origins as
// SourceStorage-tagged candidates (an origin already present just gains
// the extra source), and returns a lookup from origin to the GroupingKey
// of the one SiteGroup it's a non-partitioned ("home") member of.
//
// That lookup deliberately excludes partitioned origins: a partitioned
// origin (a third-party embed like challenges.cloudflare.com) routinely
// shows up as a child of many unrelated groups at once, with no single
// correct "owner" group — there is no safe origin -> group mapping for
// those. An origin can be a non-partitioned member of at most one group,
// so that direction is always unambiguous, and it's what a whole-site
// removal (removal.ClearSiteGroup) needs to identify the group to clear.
func MergeSiteData(origins []Origin, groups []SiteGroup) (merged []Origin, homeGroup map[string]string) {
	combined := make(map[string]map[Source]struct{}, len(origins))
	for _, o := range origins {
		set := make(map[Source]struct{}, len(o.Sources))
		for _, s := range o.Sources {
			set[s] = struct{}{}
		}
		combined[o.Origin] = set
	}

	homeGroup = make(map[string]string)
	for _, g := range groups {
		for _, o := range g.Origins {
			if o.IsPartitioned {
				continue
			}
			homeGroup[o.Origin] = g.GroupingKey
			if combined[o.Origin] == nil {
				combined[o.Origin] = make(map[Source]struct{})
			}
			combined[o.Origin][SourceStorage] = struct{}{}
		}
	}

	merged = make([]Origin, 0, len(combined))
	for origin, set := range combined {
		list := make([]Source, 0, len(set))
		for s := range set {
			list = append(list, s)
		}
		sort.Slice(list, func(i, j int) bool { return list[i] < list[j] })
		merged = append(merged, Origin{Origin: origin, Sources: list})
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Origin < merged[j].Origin })

	return merged, homeGroup
}
