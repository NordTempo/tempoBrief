package deliver

import (
	"strings"
	"testing"
)

func TestVoteLinksAreSigned(t *testing.T) {
	l := Links{Base: "https://brief.test", Secret: "s3cret"}
	up := l.Vote(42, 1)
	if !strings.HasPrefix(up, "https://brief.test/v/42/up/") {
		t.Fatalf("unexpected link %s", up)
	}
	sig := up[strings.LastIndexByte(up, '/')+1:]
	if !l.Verify(42, 1, sig) {
		t.Fatal("own signature should verify")
	}
	if l.Verify(42, -1, sig) || l.Verify(43, 1, sig) || (Links{Secret: "other"}).Verify(42, 1, sig) {
		t.Fatal("signature must bind story, vote and secret")
	}
	if (Links{}).Vote(1, 1) != "" || (Links{}).Click(1, "https://a") != "https://a" {
		t.Fatal("without a public URL, links go direct and votes are off")
	}
}
