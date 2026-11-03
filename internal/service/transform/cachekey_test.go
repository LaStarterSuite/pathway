package transform

import (
	"encoding/json"
	"testing"
)

var cacheKeyFields = []struct{ vendor, field string }{
	{"openai", "prompt_cache_key"},
	{"deepseek", "user_id"},
}

func chatHop(vendor string) Context {
	return Context{Path: "/v1/chat/completions", Provider: true, Vendor: vendor, Team: "test-team"}
}

// Sent by nobody, every tenant on the credential would share one cache partition at the vendor.
func TestAVendorCacheKeyDefaultsToTheTenant(t *testing.T) {
	for _, tc := range cacheKeyFields {
		body := saltBody(t, `{"model":"m"}`)
		changed, err := cacheKey{}.Apply(chatHop(tc.vendor), body)
		if err != nil || !changed {
			t.Fatalf("%s: changed=%v err=%v", tc.vendor, changed, err)
		}
		if string(body[tc.field]) != `"test-team"` {
			t.Errorf("%s: %s = %s", tc.vendor, tc.field, body[tc.field])
		}
	}
}

// The prefix on a provided key is the security: passed through raw, one tenant could echo
// another's key and land inside their cache namespace.
func TestAProvidedCacheKeyIsNamespacedUnderTheTenant(t *testing.T) {
	for _, tc := range cacheKeyFields {
		body := saltBody(t, `{"model":"m","`+tc.field+`":"team-a"}`)
		changed, _ := cacheKey{}.Apply(chatHop(tc.vendor), body)
		if !changed || string(body[tc.field]) != `"test-team:team-a"` {
			t.Errorf("%s: %s = %s, changed = %v", tc.vendor, tc.field, body[tc.field], changed)
		}
	}
}

// DeepSeek reads user_id inside metadata on the Anthropic shape. A metadata the caller sent keeps
// its other keys byte for byte: the 1.000 stays 1.000.
func TestDeepSeekMessagesCarryTheTenantInMetadata(t *testing.T) {
	ctx := Context{Path: messages, Provider: true, Vendor: "deepseek", Team: "test-team"}

	body := saltBody(t, `{"model":"m"}`)
	changed, _ := cacheKey{}.Apply(ctx, body)
	if !changed || string(body["metadata"]) != `{"user_id":"test-team"}` {
		t.Errorf("metadata = %s, changed = %v", body["metadata"], changed)
	}

	body = saltBody(t, `{"model":"m","metadata":{"other":1.000,"user_id":"u1"}}`)
	changed, _ = cacheKey{}.Apply(ctx, body)
	if !changed || string(body["metadata"]) != `{"other":1.000,"user_id":"test-team:u1"}` {
		t.Errorf("metadata = %s, changed = %v", body["metadata"], changed)
	}
}

// Nowhere to put it, nothing to prefix with, or not ours to parse: the body goes as sent. An engine
// of ours is cachesalt's; Anthropic has no such field; Baseten's affinity is a header.
func TestACacheKeyIsLeftAloneEverywhereElse(t *testing.T) {
	for name, tc := range map[string]struct {
		ctx  Context
		body string
	}{
		"anthropic":             {Context{Path: messages, Provider: true, Vendor: "anthropic", Team: "t"}, `{"metadata":{"user_id":"u"}}`},
		"baseten":               {Context{Path: "/v1/chat/completions", Provider: true, Vendor: "baseten", Team: "t"}, `{"user_id":"u"}`},
		"an engine of ours":     {Context{Path: "/v1/chat/completions", Team: "t"}, `{"prompt_cache_key":"u"}`},
		"no team":               {Context{Path: "/v1/chat/completions", Provider: true, Vendor: "openai"}, `{"model":"m"}`},
		"openai on messages":    {Context{Path: messages, Provider: true, Vendor: "openai", Team: "t"}, `{"model":"m"}`},
		"a non-string key":      {chatHop("openai"), `{"prompt_cache_key":42}`},
		"a non-object metadata": {Context{Path: messages, Provider: true, Vendor: "deepseek", Team: "t"}, `{"metadata":"x"}`},
	} {
		body := saltBody(t, tc.body)
		changed, err := cacheKey{}.Apply(tc.ctx, body)
		if changed || err != nil {
			t.Errorf("%s: changed=%v err=%v", name, changed, err)
		}
		if got, _ := json.Marshal(body); string(got) != tc.body {
			t.Errorf("%s: body = %s, want it as sent", name, got)
		}
	}
}
