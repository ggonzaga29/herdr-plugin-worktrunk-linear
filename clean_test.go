package main

import "testing"

func TestClean(t *testing.T) {
	hostile := "Fix\x1b]0;pwned\x07 login\r\x1b[2J\x9b\u009bnow\x7f\tok\nnext"
	if got, want := clean(hostile, false), "Fix]0;pwned login [2Jnow ok next"; got != want {
		t.Fatalf("clean(_, false) = %q, want %q", got, want)
	}
	if got, want := clean("a\nb\x1bc", true), "a\nbc"; got != want {
		t.Fatalf("clean(_, true) = %q, want %q", got, want)
	}
	if got := clean("ENG-2922 · Match web…", false); got != "ENG-2922 · Match web…" {
		t.Fatalf("clean dropped printable runes: %q", got)
	}
}
