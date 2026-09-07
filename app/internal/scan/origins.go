// Package scan implements origin discovery and permission-state reads
// (DESIGN.md sections 4.1 and 4.2): finding candidate origins purely from
// the browser's own bookkeeping, without ever loading a page from one of
// them.
package scan

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/shawngmc/chrome-polish/app/internal/cdp"
)

// Source identifies which browser-wide signal surfaced a candidate origin.
type Source string

const (
	SourceCookie        Source = "cookie"
	SourceServiceWorker Source = "service-worker"
	// SourceStorage marks an origin seen only in chrome://settings/content/
	// all's own storage-usage data (DiscoverSiteData) — an origin with
	// localStorage/cache/etc. usage but no cookie and no service worker,
	// which DiscoverOrigins alone can never see.
	SourceStorage Source = "storage"
)

// Origin is a candidate origin discovered from one or more browser-wide
// signals, along with which signals produced it.
type Origin struct {
	Origin  string
	Sources []Source
}

// DefaultServiceWorkerWindow is how long DiscoverOrigins listens for
// ServiceWorker.workerRegistrationUpdated events before concluding it has
// seen the currently-registered set. Chrome emits one such event per
// existing registration immediately after ServiceWorker.enable, so this
// only needs to cover that initial burst.
const DefaultServiceWorkerWindow = 2 * time.Second

// DiscoverOrigins unions candidate origins from the browser's cookie jar
// (Storage.getCookies) and its registered service workers
// (ServiceWorker.workerRegistrationUpdated), both browser-wide and without
// navigating to any page. swWindow controls how long to listen for service
// worker registrations; pass 0 to use DefaultServiceWorkerWindow.
func DiscoverOrigins(ctx context.Context, client *cdp.Client, swWindow time.Duration) ([]Origin, error) {
	if swWindow <= 0 {
		swWindow = DefaultServiceWorkerWindow
	}

	combined := make(map[string]map[Source]struct{})
	add := func(origin string, source Source) {
		if origin == "" {
			return
		}
		if combined[origin] == nil {
			combined[origin] = make(map[Source]struct{})
		}
		combined[origin][source] = struct{}{}
	}

	cookieOrigins, err := discoverFromCookies(ctx, client)
	if err != nil {
		return nil, err
	}
	for _, o := range cookieOrigins {
		add(o, SourceCookie)
	}

	swOrigins, err := discoverFromServiceWorkers(ctx, client, swWindow)
	if err != nil {
		return nil, err
	}
	for _, o := range swOrigins {
		add(o, SourceServiceWorker)
	}

	origins := make([]Origin, 0, len(combined))
	for origin, sources := range combined {
		list := make([]Source, 0, len(sources))
		for s := range sources {
			list = append(list, s)
		}
		sort.Slice(list, func(i, j int) bool { return list[i] < list[j] })
		origins = append(origins, Origin{Origin: origin, Sources: list})
	}
	sort.Slice(origins, func(i, j int) bool { return origins[i].Origin < origins[j].Origin })

	return origins, nil
}

type cdpCookie struct {
	Domain       string `json:"domain"`
	SourceScheme string `json:"sourceScheme"`
}

type getCookiesResult struct {
	Cookies []cdpCookie `json:"cookies"`
}

func discoverFromCookies(ctx context.Context, client *cdp.Client) ([]string, error) {
	var result getCookiesResult
	if err := client.Call(ctx, "Storage.getCookies", nil, &result); err != nil {
		return nil, fmt.Errorf("scan: Storage.getCookies: %w", err)
	}

	seen := make(map[string]bool)
	var origins []string
	for _, c := range result.Cookies {
		origin := cookieOrigin(c)
		if origin == "" || seen[origin] {
			continue
		}
		seen[origin] = true
		origins = append(origins, origin)
	}
	return origins, nil
}

// cookieOrigin builds an origin string from a cookie's domain and scheme.
// Chrome's Cookie.domain omits any leading "." for host-only cookies and
// includes it for domain cookies; either way the host itself is what we
// want. sourceScheme is "Secure" or "NonSecure" once the cookie has
// actually been set over a connection; default to https for the rare
// "Unset" case since that's the overwhelmingly common scheme in practice.
func cookieOrigin(c cdpCookie) string {
	host := strings.TrimPrefix(c.Domain, ".")
	if host == "" {
		return ""
	}
	scheme := "https"
	if c.SourceScheme == "NonSecure" {
		scheme = "http"
	}
	return scheme + "://" + host
}

type serviceWorkerRegistration struct {
	ScopeURL  string `json:"scopeURL"`
	IsDeleted bool   `json:"isDeleted"`
}

type workerRegistrationUpdatedParams struct {
	Registrations []serviceWorkerRegistration `json:"registrations"`
}

// discoverFromServiceWorkers enables the ServiceWorker domain and collects
// workerRegistrationUpdated events for window. Chrome immediately replays
// the full current registration set as this event fires once enabled, so a
// short fixed window is enough to observe it.
//
// The ServiceWorker domain isn't available on the browser-level connection
// itself — it's only exposed within a target session (Chrome returns
// "method not found" otherwise) — so this attaches to a throwaway
// about:blank target purely to obtain a session to enable it on.
// Registrations reported once enabled cover the whole browser context
// (Chrome tracks service workers per profile, not per tab), not just the
// blank tab, and no candidate/suspect origin is ever navigated to.
func discoverFromServiceWorkers(ctx context.Context, client *cdp.Client, window time.Duration) ([]string, error) {
	sessionID, cleanup, err := cdp.AttachTarget(ctx, client, "about:blank", true)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	events, unsubscribe := client.Subscribe("ServiceWorker.workerRegistrationUpdated")
	defer unsubscribe()

	if err := client.CallSession(ctx, sessionID, "ServiceWorker.enable", nil, nil); err != nil {
		return nil, fmt.Errorf("scan: ServiceWorker.enable: %w", err)
	}
	defer client.CallSession(context.Background(), sessionID, "ServiceWorker.disable", nil, nil)

	timer := time.NewTimer(window)
	defer timer.Stop()

	seen := make(map[string]bool)
	var origins []string

	for {
		select {
		case raw, ok := <-events:
			if !ok {
				return origins, nil
			}
			var params workerRegistrationUpdatedParams
			if err := json.Unmarshal(raw, &params); err != nil {
				continue
			}
			for _, reg := range params.Registrations {
				if reg.IsDeleted {
					continue
				}
				origin := scopeOrigin(reg.ScopeURL)
				if origin == "" || seen[origin] {
					continue
				}
				seen[origin] = true
				origins = append(origins, origin)
			}

		case <-timer.C:
			return origins, nil

		case <-ctx.Done():
			return origins, ctx.Err()
		}
	}
}

func scopeOrigin(scopeURL string) string {
	u, err := url.Parse(scopeURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
