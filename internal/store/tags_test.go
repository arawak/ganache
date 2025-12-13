package store

import "testing"

func TestNormalizeTag(t *testing.T) {
	cases := map[string]string{
		"  Foo  ":      "foo",
		"Foo   Bar":    "foo bar",
		"":             "",
		"  ":           "",
		"Mixed	Case":   "mixed case",
		"Two  Words  ": "two words",
	}
	for in, expect := range cases {
		if got := NormalizeTag(in); got != expect {
			t.Fatalf("normalize %q => %q, expected %q", in, got, expect)
		}
	}
}

func TestNormalizeTagsAndText(t *testing.T) {
	in := []string{"TagOne", "tagone", " Tag Two ", "tag  three"}
	norm := NormalizeTags(in)
	expect := []string{"tag three", "tag two", "tagone"}
	if len(norm) != len(expect) {
		t.Fatalf("expected %d tags got %d", len(expect), len(norm))
	}
	for i := range norm {
		if norm[i] != expect[i] {
			t.Fatalf("tag %d expected %q got %q", i, expect[i], norm[i])
		}
	}
	if text := TagText(in); text != "tag three tag two tagone" {
		t.Fatalf("tag text expected %q got %q", "tag three tag two tagone", text)
	}
}
