package scan

import "testing"

func TestFindAll(t *testing.T) {
	root := &DOMNode{
		Tag: "div",
		Children: []DOMNode{
			{Tag: "site-list", Children: []DOMNode{
				{Tag: "site-list-entry"},
				{Tag: "site-list-entry"},
			}},
			{Tag: "site-list", Children: []DOMNode{
				{Tag: "site-list-entry"},
			}},
		},
	}

	got := FindAll(root, "site-list-entry")
	if len(got) != 3 {
		t.Fatalf("FindAll returned %d nodes, want 3", len(got))
	}

	lists := FindAll(root, "site-list")
	if len(lists) != 2 {
		t.Fatalf("FindAll returned %d site-list nodes, want 2", len(lists))
	}

	if got := FindAll(root, "nonexistent"); got != nil {
		t.Errorf("FindAll for a missing tag = %v, want nil", got)
	}

	// The root itself matches if its own tag matches.
	if got := FindAll(root, "div"); len(got) != 1 || got[0] != root {
		t.Errorf("FindAll should include the root node itself when it matches")
	}
}

func TestHasClass(t *testing.T) {
	n := &DOMNode{Attrs: map[string]string{"class": "url-directionality secondary"}}

	if !n.HasClass("url-directionality") {
		t.Error("expected HasClass to find a token among several space-separated classes")
	}
	if !n.HasClass("secondary") {
		t.Error("expected HasClass to find the last token")
	}
	if n.HasClass("url") {
		t.Error("HasClass should not match a substring of a token")
	}
	if n.HasClass("directionality") {
		t.Error("HasClass should not match a substring of a token")
	}

	empty := &DOMNode{}
	if empty.HasClass("anything") {
		t.Error("HasClass on a node with no class attribute should be false")
	}
}

func TestAllText(t *testing.T) {
	// {text: "..."} nodes (Tag == "") carry text; element nodes recurse.
	n := &DOMNode{
		Tag: "span",
		Children: []DOMNode{
			{Text: "https://example.com"},
			{Tag: "b", Children: []DOMNode{
				{Text: "bold"},
			}},
			{Text: "tail"},
		},
	}

	got := n.AllText()
	want := "https://example.com bold tail"
	if got != want {
		t.Errorf("AllText() = %q, want %q", got, want)
	}

	if got := (&DOMNode{}).AllText(); got != "" {
		t.Errorf("AllText() on an empty node = %q, want empty string", got)
	}
}
