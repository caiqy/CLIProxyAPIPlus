package thinking

import "strings"

const (
	// AdaptationDecisionNone indicates no explicit thinking variant was requested.
	AdaptationDecisionNone = "none"
	// AdaptationDecisionPass indicates requested variant was preserved.
	AdaptationDecisionPass = "pass"
	// AdaptationDecisionDowngrade indicates requested variant was changed or removed.
	AdaptationDecisionDowngrade = "downgrade"
)

// AdaptationMeta describes how requested thinking strength was adapted.
type AdaptationMeta struct {
	VariantOrigin string
	Variant       string
	Decision      string
	Reason        string
}

func variantFromConfig(config ThinkingConfig) string {
	switch config.Mode {
	case ModeLevel:
		return strings.ToLower(strings.TrimSpace(string(config.Level)))
	case ModeNone:
		if config.Level != "" {
			return strings.ToLower(strings.TrimSpace(string(config.Level)))
		}
		return string(LevelNone)
	case ModeAuto:
		return string(LevelAuto)
	case ModeBudget:
		if level, ok := ConvertBudgetToLevel(config.Budget); ok {
			return strings.ToLower(strings.TrimSpace(level))
		}
	}
	return ""
}

func buildAdaptationMeta(origin, resolved, reason string) AdaptationMeta {
	origin = strings.ToLower(strings.TrimSpace(origin))
	resolved = strings.ToLower(strings.TrimSpace(resolved))
	if origin == "" && resolved == "" {
		if reason == "" {
			reason = "no_explicit_variant"
		}
		return AdaptationMeta{VariantOrigin: "", Variant: "", Decision: AdaptationDecisionNone, Reason: reason}
	}
	if origin != "" && resolved != "" && origin == resolved {
		if reason == "" {
			reason = "preserved"
		}
		return AdaptationMeta{VariantOrigin: origin, Variant: resolved, Decision: AdaptationDecisionPass, Reason: reason}
	}
	if reason == "" {
		reason = "unsupported_by_model"
	}
	return AdaptationMeta{VariantOrigin: origin, Variant: resolved, Decision: AdaptationDecisionDowngrade, Reason: reason}
}
