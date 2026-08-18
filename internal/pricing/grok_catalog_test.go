package pricing

import "testing"

func TestGrokRateForModelDoesNotPriceFastSKUAsGrok4(t *testing.T) {
	t.Parallel()
	fast, ok := GrokRateForModel("grok-4.1-fast")
	if !ok || fast.ModelID != "grok-4.1-fast" || fast.InputMicros != 200_000 {
		t.Fatalf("grok-4.1-fast rate = %#v, %t", fast, ok)
	}
	base, ok := GrokRateForModel("grok-4")
	if !ok || base.ModelID != "grok-4" || base.InputMicros != 3_000_000 {
		t.Fatalf("grok-4 rate = %#v, %t", base, ok)
	}
	build, ok := GrokRateForModel("grok-4.6-build")
	if !ok || build.ModelID != "grok-4.6" {
		t.Fatalf("grok-4.6-build rate = %#v, %t", build, ok)
	}
	if _, ok := GrokRateForModel("cursor-grok-4.6-xhigh"); ok {
		t.Fatal("cursor-prefixed model should not match xAI catalog")
	}
}
