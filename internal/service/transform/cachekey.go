package transform

import "github.com/frappe/pathway/internal/domain"

func init() {
	Register(cacheKey{})
}

// cacheKey pins a vendor's prompt-cache partition to the tenant: OpenAI's prompt_cache_key and
// DeepSeek's user_id become the team, or "<team>:<caller's value>" when the caller sent one.
// Passed through raw, one tenant could echo another's key and land inside their cache namespace;
// sent by nobody, every tenant on the credential shares one. Anthropic has no such field (its
// cache is per workspace), Baseten's affinity header is routing, and an engine of ours is
// cachesalt's to salt.
type cacheKey struct{}

func (cacheKey) Name() string        { return "cachekey" }
func (cacheKey) Endpoints() []string { return []string{"/v1/chat/completions", messages} }

func (cacheKey) Apply(ctx Context, body Body) (bool, error) {
	if !ctx.Provider || ctx.Team == "" {
		return false, nil
	}
	switch {
	case ctx.Vendor == "openai" && ctx.Path != messages:
		return namespaceKey(ctx.Team, body, "prompt_cache_key")
	case ctx.Vendor == "deepseek" && ctx.Path != messages:
		return namespaceKey(ctx.Team, body, "user_id")
	case ctx.Vendor == "deepseek":
		return namespaceMetadataKey(ctx.Team, body)
	}
	return false, nil
}

// namespaceKey sets body[field] to the team, or to the team-prefixed form of the string the caller
// sent. A non-string, or a value the prefix pushes past DeepSeek's 512 bytes, is the vendor's
// schema error to give, not ours to guess around.
func namespaceKey(team string, body Body, field string) (bool, error) {
	value := team
	if raw, present := body[field]; present {
		var sent string
		if domain.JSON.Unmarshal(raw, &sent) != nil {
			return false, nil
		}
		value = team + ":" + sent
	}
	encoded, err := domain.JSON.Marshal(value)
	if err != nil {
		return false, err
	}
	body[field] = encoded
	return true, nil
}

// namespaceMetadataKey is namespaceKey for the Anthropic shape, where DeepSeek reads user_id
// inside metadata. The other keys of a metadata the caller sent keep their bytes; one that is not
// an object is the vendor's to refuse.
func namespaceMetadataKey(team string, body Body) (bool, error) {
	metadata := Body{}
	if raw, present := body["metadata"]; present {
		if domain.JSON.Unmarshal(raw, &metadata) != nil {
			return false, nil
		}
		if metadata == nil {
			metadata = Body{}
		}
	}
	if changed, err := namespaceKey(team, metadata, "user_id"); !changed || err != nil {
		return changed, err
	}
	encoded, err := domain.JSON.Marshal(metadata)
	if err != nil {
		return false, err
	}
	body["metadata"] = encoded
	return true, nil
}
