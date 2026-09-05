package scan

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/shawngmc/chrome-polish/app/internal/cdp"
)

// DOMNode is a lightweight, JSON-friendly serialization of a piece of DOM,
// with shadow-root contents inlined as ordinary children. Elements with no
// children, no shadow root, and no text of their own are pruned to keep
// the tree readable — this is meant for reading rendered content and
// structure, not for reproducing the DOM exactly.
type DOMNode struct {
	Tag      string            `json:"tag,omitempty"`
	Text     string            `json:"text,omitempty"`
	Attrs    map[string]string `json:"attrs,omitempty"`
	Children []DOMNode         `json:"children,omitempty"`
}

// pierceDOMScript walks document.documentElement, descending into
// shadowRoot on every element that has one, and returns a JSON-serialized
// DOMNode tree. Only a handful of attributes are kept (enough to identify
// elements without hauling over every Polymer/Lit internal binding), and
// non-content tags are skipped entirely.
const pierceDOMScript = `
(function() {
  var SKIP_TAGS = {script:1, style:1, template:1, link:1, meta:1, svg:1, path:1};
  var KEEP_ATTRS = {id:1, class:1, 'aria-label':1, role:1, href:1, title:1};

  function walk(node, depth) {
    if (depth > 80) return null;

    if (node.nodeType === Node.TEXT_NODE) {
      var t = node.textContent.replace(/\s+/g, ' ').trim();
      return t ? {text: t} : null;
    }
    if (node.nodeType !== Node.ELEMENT_NODE) return null;

    var tag = node.tagName.toLowerCase();
    if (SKIP_TAGS[tag]) return null;

    var children = [];
    var root = node.shadowRoot;
    if (root) {
      for (var i = 0; i < root.childNodes.length; i++) {
        var s = walk(root.childNodes[i], depth + 1);
        if (s) children.push(s);
      }
    }
    for (var j = 0; j < node.childNodes.length; j++) {
      var s2 = walk(node.childNodes[j], depth + 1);
      if (s2) children.push(s2);
    }

    if (children.length === 0 && !root) return null;

    var attrs = {};
    var hasAttrs = false;
    for (var k = 0; k < (node.attributes || []).length; k++) {
      var a = node.attributes[k];
      if (KEEP_ATTRS[a.name]) {
        attrs[a.name] = a.value;
        hasAttrs = true;
      }
    }

    var out = {tag: tag, children: children};
    if (hasAttrs) out.attrs = attrs;
    return out;
  }

  return JSON.stringify(walk(document.documentElement, 0));
})()
`

// DumpDOM navigates a throwaway target to targetURL — which must be a
// local, static Chrome page such as "chrome://settings/..." or
// "about:blank", never a candidate/suspect origin — waits briefly for it
// to render, and returns its DOM with shadow roots pierced.
//
// settleDelay controls how long to wait after navigation before reading
// the DOM, to let client-side-rendered (Polymer/Lit) pages finish
// rendering; pass 0 for a sensible default. foreground brings the target
// to the front rather than opening it in the background; some Chrome
// WebUI pages (e.g. chrome://settings' all-sites list) use virtualized
// lists that render no rows at all without real layout, which only a
// foreground tab gets.
func DumpDOM(ctx context.Context, client *cdp.Client, targetURL string, settleDelay time.Duration, foreground bool) (*DOMNode, error) {
	if settleDelay <= 0 {
		settleDelay = 500 * time.Millisecond
	}

	sessionID, cleanup, err := attachTarget(ctx, client, targetURL, !foreground)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	select {
	case <-time.After(settleDelay):
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
		"expression":    pierceDOMScript,
		"returnByValue": true,
	}, &evalResult); err != nil {
		return nil, fmt.Errorf("scan: Runtime.evaluate on %s: %w", targetURL, err)
	}
	if evalResult.ExceptionDetails != nil {
		return nil, fmt.Errorf("scan: pierce script threw on %s: %s", targetURL, evalResult.ExceptionDetails.Text)
	}

	var root DOMNode
	if err := json.Unmarshal([]byte(evalResult.Result.Value), &root); err != nil {
		return nil, fmt.Errorf("scan: decode pierced DOM from %s: %w", targetURL, err)
	}
	return &root, nil
}

// FindAll returns every node in the tree rooted at n (n included) whose tag
// matches, in document order.
func FindAll(n *DOMNode, tag string) []*DOMNode {
	var out []*DOMNode
	var walk func(*DOMNode)
	walk = func(cur *DOMNode) {
		if cur.Tag == tag {
			out = append(out, cur)
		}
		for i := range cur.Children {
			walk(&cur.Children[i])
		}
	}
	walk(n)
	return out
}

// HasClass reports whether n's class attribute includes class as one of
// its whitespace-separated tokens.
func (n *DOMNode) HasClass(class string) bool {
	for _, token := range strings.Fields(n.Attrs["class"]) {
		if token == class {
			return true
		}
	}
	return false
}

// AllText concatenates all text-node descendants of n, in document order,
// separated by spaces.
func (n *DOMNode) AllText() string {
	var parts []string
	var walk func(*DOMNode)
	walk = func(cur *DOMNode) {
		if cur.Tag == "" && cur.Text != "" {
			parts = append(parts, cur.Text)
		}
		for i := range cur.Children {
			walk(&cur.Children[i])
		}
	}
	walk(n)
	return strings.Join(parts, " ")
}
