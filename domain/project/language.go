package project

import (
	"fmt"
	"strings"
	"unicode"
)

// DefaultLanguage is the default commit message language (ISO 639-1 "en").
const DefaultLanguage = "en"

// iso639_1 is the set of all 183 two-letter language codes defined in ISO 639-1.
var iso639_1 = map[string]struct{}{
	"aa": {}, "ab": {}, "ae": {}, "af": {}, "ak": {}, "am": {}, "an": {}, "ar": {}, "as": {}, "av": {}, "ay": {}, "az": {},
	"ba": {}, "be": {}, "bg": {}, "bh": {}, "bi": {}, "bm": {}, "bn": {}, "bo": {}, "br": {}, "bs": {},
	"ca": {}, "ce": {}, "ch": {}, "co": {}, "cr": {}, "cs": {}, "cu": {}, "cv": {}, "cy": {},
	"da": {}, "de": {}, "dv": {}, "dz": {},
	"ee": {}, "el": {}, "en": {}, "eo": {}, "es": {}, "et": {}, "eu": {},
	"fa": {}, "ff": {}, "fi": {}, "fj": {}, "fo": {}, "fr": {}, "fy": {},
	"ga": {}, "gd": {}, "gl": {}, "gn": {}, "gu": {}, "gv": {},
	"ha": {}, "he": {}, "hi": {}, "ho": {}, "hr": {}, "ht": {}, "hu": {}, "hy": {}, "hz": {},
	"ia": {}, "id": {}, "ie": {}, "ig": {}, "ii": {}, "ik": {}, "io": {}, "is": {}, "it": {}, "iu": {},
	"ja": {}, "jv": {},
	"ka": {}, "kg": {}, "ki": {}, "kj": {}, "kk": {}, "kl": {}, "km": {}, "kn": {}, "ko": {}, "kr": {}, "ks": {}, "ku": {}, "kv": {}, "kw": {}, "ky": {},
	"la": {}, "lb": {}, "lg": {}, "li": {}, "ln": {}, "lo": {}, "lt": {}, "lu": {}, "lv": {},
	"mg": {}, "mh": {}, "mi": {}, "mk": {}, "ml": {}, "mn": {}, "mr": {}, "ms": {}, "mt": {}, "my": {},
	"na": {}, "nb": {}, "nd": {}, "ne": {}, "ng": {}, "nl": {}, "nn": {}, "no": {}, "nr": {}, "nv": {}, "ny": {},
	"oc": {}, "oj": {}, "om": {}, "or": {}, "os": {},
	"pa": {}, "pi": {}, "pl": {}, "ps": {}, "pt": {},
	"qu": {},
	"rm": {}, "rn": {}, "ro": {}, "ru": {}, "rw": {},
	"sa": {}, "sc": {}, "sd": {}, "se": {}, "sg": {}, "si": {}, "sk": {}, "sl": {}, "sm": {}, "sn": {}, "so": {}, "sq": {}, "sr": {}, "ss": {}, "st": {}, "su": {}, "sv": {}, "sw": {},
	"ta": {}, "te": {}, "tg": {}, "th": {}, "ti": {}, "tk": {}, "tl": {}, "tn": {}, "to": {}, "tr": {}, "ts": {}, "tt": {}, "tw": {}, "ty": {},
	"ug": {}, "uk": {}, "ur": {}, "uz": {},
	"ve": {}, "vi": {}, "vo": {},
	"wa": {}, "wo": {},
	"xh": {},
	"yi": {}, "yo": {},
	"za": {}, "zh": {}, "zu": {},
}

// commonLanguageNames maps common natural-language names to their ISO 639-1 codes.
var commonLanguageNames = map[string]string{
	"english":    "en",
	"chinese":    "zh",
	"japanese":   "ja",
	"german":     "de",
	"french":     "fr",
	"spanish":    "es",
	"korean":     "ko",
	"russian":    "ru",
	"italian":    "it",
	"portuguese": "pt",
	"dutch":      "nl",
	"arabic":     "ar",
	"hindi":      "hi",
	"turkish":    "tr",
	"polish":     "pl",
	"swedish":    "sv",
	"norwegian":  "no",
	"danish":     "da",
	"finnish":    "fi",
	"vietnamese": "vi",
	"thai":       "th",
	"indonesian": "id",
	"czech":      "cs",
	"greek":      "el",
	"hebrew":     "he",
	"ukrainian":  "uk",
}

// isoLanguageDisplayNames maps ISO codes and tags to natural English names for prompt formatting.
var isoLanguageDisplayNames = map[string]string{
	"en":      "English",
	"zh":      "Chinese",
	"zh-cn":   "Simplified Chinese",
	"zh-hans": "Simplified Chinese",
	"zh-tw":   "Traditional Chinese",
	"zh-hant": "Traditional Chinese",
	"ja":      "Japanese",
	"es":      "Spanish",
	"de":      "German",
	"fr":      "French",
	"ko":      "Korean",
	"pt":      "Portuguese",
	"pt-br":   "Brazilian Portuguese",
	"ru":      "Russian",
	"it":      "Italian",
	"nl":      "Dutch",
	"pl":      "Polish",
	"vi":      "Vietnamese",
	"th":      "Thai",
	"ar":      "Arabic",
	"hi":      "Hindi",
	"tr":      "Turkish",
	"id":      "Indonesian",
	"sv":      "Swedish",
	"da":      "Danish",
	"fi":      "Finnish",
	"no":      "Norwegian",
	"cs":      "Czech",
	"el":      "Greek",
	"he":      "Hebrew",
	"uk":      "Ukrainian",
}

// ValidateLanguage validates and normalizes a language string according to ISO 639
// and BCP 47 conventions, or accepts "auto".
// It returns the normalized canonical tag (e.g. "en", "zh-CN", "en-US", "auto") or an error.
func ValidateLanguage(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("empty value for language: must be an ISO 639 code (e.g. \"en\", \"zh\") or \"auto\"")
	}

	if strings.EqualFold(s, "auto") {
		return "auto", nil
	}

	lower := strings.ToLower(s)
	if iso, ok := commonLanguageNames[lower]; ok {
		return iso, nil
	}

	// Normalize separators ('_' from POSIX locales e.g. "zh_CN" to '-')
	clean := strings.ReplaceAll(s, "_", "-")
	parts := strings.Split(clean, "-")
	if len(parts) == 0 || len(parts) > 3 {
		return "", invalidLanguageError(raw)
	}

	// Part 0: Primary language subtag (2 or 3 ASCII letters)
	primary := strings.ToLower(parts[0])
	if !isAllLetters(primary) || len(primary) < 2 || len(primary) > 3 {
		return "", invalidLanguageError(raw)
	}
	if len(primary) == 2 {
		if _, ok := iso639_1[primary]; !ok {
			return "", fmt.Errorf("invalid ISO 639-1 language code %q in %q", primary, raw)
		}
	}

	normalized := []string{primary}

	for i := 1; i < len(parts); i++ {
		p := parts[i]
		switch {
		case len(p) == 4 && isAllLetters(p):
			// ISO 15924 script subtag (e.g. "Hans", "Hant", "Latn") -> Titlecase
			normalized = append(normalized, titleCase(p))
		case len(p) == 2 && isAllLetters(p):
			// ISO 3166-1 alpha-2 region code (e.g. "US", "CN", "GB") -> Uppercase
			normalized = append(normalized, strings.ToUpper(p))
		case len(p) == 3 && isAllDigits(p):
			// UN M.49 numeric region code (e.g. "419" for Latin America)
			normalized = append(normalized, p)
		default:
			return "", invalidLanguageError(raw)
		}
	}

	return strings.Join(normalized, "-"), nil
}

func invalidLanguageError(raw string) error {
	return fmt.Errorf("invalid language %q: must be a valid ISO 639 code (e.g. \"en\", \"zh\", \"ja\") or BCP 47 tag (e.g. \"en-US\", \"zh-CN\"), or \"auto\"", raw)
}

// IsEnglishLanguage reports whether the given language tag represents English.
func IsEnglishLanguage(lang string) bool {
	l := strings.ToLower(strings.TrimSpace(lang))
	return l == "english" || l == "en" || strings.HasPrefix(l, "en-") || strings.HasPrefix(l, "en_") ||
		l == "eng" || strings.HasPrefix(l, "eng-")
}

// ResolveLanguageDisplayName resolves an ISO language code or tag to a natural English
// name for LLM prompting (e.g. "zh" -> "Chinese", "zh-CN" -> "Simplified Chinese", "ja" -> "Japanese").
// If the tag is not in the known map, it returns the tag unchanged.
func ResolveLanguageDisplayName(lang string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	if name, ok := isoLanguageDisplayNames[l]; ok {
		return name
	}
	// Try primary language subtag (e.g. "zh" from "zh-Hans-CN")
	if idx := strings.IndexAny(l, "-_"); idx != -1 {
		base := l[:idx]
		if name, ok := isoLanguageDisplayNames[base]; ok {
			return name
		}
	}
	return strings.TrimSpace(lang)
}

func isAllLetters(s string) bool {
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return len(s) > 0
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

func titleCase(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(strings.ToLower(s))
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
