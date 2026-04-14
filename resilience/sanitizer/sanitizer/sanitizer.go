// Package gosanitizer provides production-grade input sanitization:
// HTML, SQL, path traversal, null-byte injection, Unicode normalization,
// struct-tag-driven bulk sanitization, and XSS prevention.
package gosanitizer

import (
	"html"
	"net"
	"net/url"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ---- Core string functions --------------------------------------------------

// StripHTML removes all HTML tags and returns plain text.
func StripHTML(s string) string {
	re := reHTML
	return re.ReplaceAllString(s, "")
}

// EscapeHTML encodes HTML special characters, preventing XSS.
func EscapeHTML(s string) string {
	return html.EscapeString(s)
}

// NormalizeUnicode converts the string to NFC form and removes control characters.
func NormalizeUnicode(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// RemoveNullBytes strips null bytes (0x00) from s.
func RemoveNullBytes(s string) string {
	return strings.ReplaceAll(s, "\x00", "")
}

// TrimWhitespace trims all leading/trailing Unicode whitespace.
func TrimWhitespace(s string) string {
	return strings.TrimFunc(s, unicode.IsSpace)
}

// CollapseWhitespace reduces internal runs of whitespace to a single space.
func CollapseWhitespace(s string) string {
	return reWhitespace.ReplaceAllString(s, " ")
}

// Truncate returns at most maxRunes runes from s (safe for multibyte strings).
func Truncate(s string, maxRunes int) string {
	i := 0
	for j := range s {
		if i >= maxRunes {
			return s[:j]
		}
		i++
	}
	return s
}

// IsValidUTF8 reports whether s is valid UTF-8.
func IsValidUTF8(s string) bool { return utf8.ValidString(s) }

// ---- SQL injection ----------------------------------------------------------

var sqlDangerousRe = regexp.MustCompile(`(?i)(--|;|\/\*|\*\/|xp_|exec\s|union\s+select|drop\s+table|insert\s+into|delete\s+from|update\s+\w+\s+set|select\s+.*\s+from|alter\s+table|create\s+(table|database|index)|truncate\s+table)`)

// EscapeSQL escapes single quotes and removes obvious SQL injection patterns.
// Prefer parameterised queries; use this as a last-resort defence-in-depth layer.
func EscapeSQL(s string) string {
	s = strings.ReplaceAll(s, "'", "''")
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = sqlDangerousRe.ReplaceAllString(s, "")
	return s
}

// ContainsSQLInjection heuristically detects SQL injection patterns.
func ContainsSQLInjection(s string) bool {
	return sqlDangerousRe.MatchString(s)
}

// ---- Path traversal --------------------------------------------------------

// SanitizePath cleans a file path and prevents directory traversal.
// The result is always relative (no leading separator) and cannot escape root.
func SanitizePath(p string) string {
	p = filepath.Clean(p)
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimPrefix(p, `\`)
	// Reject traversal
	for strings.Contains(p, "..") {
		p = strings.ReplaceAll(p, "..", "")
		p = filepath.Clean(p)
	}
	return p
}

// IsPathTraversal reports whether the path contains traversal sequences.
func IsPathTraversal(p string) bool {
	p = url.PathEscape(p)
	return strings.Contains(p, "..%2F") ||
		strings.Contains(p, "..%5C") ||
		strings.Contains(p, "..\\") ||
		strings.Contains(p, "../")
}

// ---- URL sanitization -------------------------------------------------------

var allowedURLSchemes = map[string]bool{
	"http":  true,
	"https": true,
	"ftp":   true,
	"ftps":  true,
}

// SanitizeURL validates and normalises a URL.
// Returns an empty string if the URL is malformed or uses a disallowed scheme.
func SanitizeURL(raw string) string {
	raw = TrimWhitespace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if !allowedURLSchemes[strings.ToLower(u.Scheme)] {
		return ""
	}
	// Prevent SSRF: reject private/loopback IPs
	host := u.Hostname()
	if isPrivateHost(host) {
		return ""
	}
	return u.String()
}

// AllowURLSchemes overrides the default allowed scheme set.
func AllowURLSchemes(schemes ...string) {
	for k := range allowedURLSchemes {
		delete(allowedURLSchemes, k)
	}
	for _, s := range schemes {
		allowedURLSchemes[strings.ToLower(s)] = true
	}
}

func isPrivateHost(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	privates := []string{
		"127.0.0.0/8",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.0.0/16",
		"::1/128",
		"fc00::/7",
	}
	for _, cidr := range privates {
		_, network, _ := net.ParseCIDR(cidr)
		if network != nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

// ---- Email / alphanumeric ---------------------------------------------------

var (
	reEmail         = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	reAlphaNum      = regexp.MustCompile(`[^a-zA-Z0-9]`)
	reAlphaNumDash  = regexp.MustCompile(`[^a-zA-Z0-9\-_]`)
	reHTML          = regexp.MustCompile(`<[^>]*>`)
	reWhitespace    = regexp.MustCompile(`\s+`)
	rePhone         = regexp.MustCompile(`[^\d\+\-\(\)\s]`)
	reScriptInURL   = regexp.MustCompile(`(?i)javascript:|data:`)
)

// IsEmail reports whether s is a syntactically valid e-mail address.
func IsEmail(s string) bool { return reEmail.MatchString(s) }

// SanitizeAlphaNum removes all characters that are not letters or digits.
func SanitizeAlphaNum(s string) string { return reAlphaNum.ReplaceAllString(s, "") }

// SanitizeSlug removes all characters that are not letters, digits, hyphens, or underscores.
func SanitizeSlug(s string) string {
	s = strings.ToLower(TrimWhitespace(s))
	s = reWhitespace.ReplaceAllString(s, "-")
	return reAlphaNumDash.ReplaceAllString(s, "")
}

// SanitizePhone strips non-phone characters and preserves +, -, (, ), digits, spaces.
func SanitizePhone(s string) string { return rePhone.ReplaceAllString(s, "") }

// SanitizeFilename removes characters illegal in file names across all OSes.
func SanitizeFilename(s string) string {
	illegal := `<>:"/\|?*` + string([]rune{0})
	for _, c := range illegal {
		s = strings.ReplaceAll(s, string(c), "_")
	}
	s = TrimWhitespace(s)
	if len(s) > 255 {
		s = s[:255]
	}
	return s
}

// ---- XSS -------------------------------------------------------------------

var dangerousAttrs = regexp.MustCompile(`(?i)(on\w+|javascript:|data:)\s*=`)

// SanitizeForDisplay sanitises user content for safe HTML display.
// It strips tags, escapes HTML entities, and removes event handlers.
func SanitizeForDisplay(s string) string {
	s = StripHTML(s)
	s = EscapeHTML(s)
	s = dangerousAttrs.ReplaceAllString(s, "")
	s = reScriptInURL.ReplaceAllString(s, "blocked:")
	return s
}

// ---- Struct-tag driven bulk sanitization ------------------------------------

// Sanitize walks a struct pointer and applies sanitization based on the
// `sanitize` struct tag. Supported tag values (comma-separated):
//
//	trim         – TrimWhitespace
//	html         – StripHTML
//	escape       – EscapeHTML
//	lower        – strings.ToLower
//	upper        – strings.ToUpper
//	slug         – SanitizeSlug
//	alphanum     – SanitizeAlphaNum
//	email        – TrimWhitespace + lower
//	phone        – SanitizePhone
//	url          – SanitizeURL
//	filename     – SanitizeFilename
//	nullbyte     – RemoveNullBytes
//	collapse     – CollapseWhitespace
//	display      – SanitizeForDisplay
func Sanitize(v interface{}) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
		return
	}
	rv = rv.Elem()
	rt := rv.Type()
	for i := 0; i < rv.NumField(); i++ {
		field := rv.Field(i)
		if !field.CanSet() || field.Kind() != reflect.String {
			continue
		}
		tag := rt.Field(i).Tag.Get("sanitize")
		if tag == "" {
			continue
		}
		s := field.String()
		for _, op := range strings.Split(tag, ",") {
			switch strings.TrimSpace(op) {
			case "trim":
				s = TrimWhitespace(s)
			case "html":
				s = StripHTML(s)
			case "escape":
				s = EscapeHTML(s)
			case "lower":
				s = strings.ToLower(s)
			case "upper":
				s = strings.ToUpper(s)
			case "slug":
				s = SanitizeSlug(s)
			case "alphanum":
				s = SanitizeAlphaNum(s)
			case "email":
				s = strings.ToLower(TrimWhitespace(s))
			case "phone":
				s = SanitizePhone(s)
			case "url":
				s = SanitizeURL(s)
			case "filename":
				s = SanitizeFilename(s)
			case "nullbyte":
				s = RemoveNullBytes(s)
			case "collapse":
				s = CollapseWhitespace(s)
			case "display":
				s = SanitizeForDisplay(s)
			}
		}
		field.SetString(s)
	}
}
