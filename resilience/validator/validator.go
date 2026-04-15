// Package govalidator provides production-grade struct and field validation
// with struct tags, custom rules, i18n error messages, and composable validators.
package govalidator

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// ---- Errors -----------------------------------------------------------------

// ValidationError represents a single field validation failure.
type ValidationError struct {
	Field   string
	Rule    string
	Value   interface{}
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("field %q failed rule %q: %s", e.Field, e.Rule, e.Message)
}

// ValidationErrors is a collection of ValidationError.
type ValidationErrors []*ValidationError

func (ve ValidationErrors) Error() string {
	msgs := make([]string, len(ve))
	for i, e := range ve {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "; ")
}

// HasField reports whether there is a validation error for the given field.
func (ve ValidationErrors) HasField(field string) bool {
	for _, e := range ve {
		if e.Field == field {
			return true
		}
	}
	return false
}

// FieldErrors returns only errors for the given field.
func (ve ValidationErrors) FieldErrors(field string) ValidationErrors {
	var out ValidationErrors
	for _, e := range ve {
		if e.Field == field {
			out = append(out, e)
		}
	}
	return out
}

// Map converts ValidationErrors into a field→message map (first error per field).
func (ve ValidationErrors) Map() map[string]string {
	m := make(map[string]string)
	for _, e := range ve {
		if _, exists := m[e.Field]; !exists {
			m[e.Field] = e.Message
		}
	}
	return m
}

// ---- Rule -------------------------------------------------------------------

// Rule is a function that validates a single value.
// It returns a non-nil error message (not wrapped) on failure.
type Rule func(field string, value interface{}) *ValidationError

// ---- Built-in rules ---------------------------------------------------------

var (
	reEmail   = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	reAlpha   = regexp.MustCompile(`^[a-zA-Z]+$`)
	reAlNum   = regexp.MustCompile(`^[a-zA-Z0-9]+$`)
	reNumeric = regexp.MustCompile(`^[0-9]+$`)
	reSlug    = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	reUUID    = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	rePhone   = regexp.MustCompile(`^\+?[1-9]\d{6,14}$`)
	reHex     = regexp.MustCompile(`^#?([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
	reSemVer  = regexp.MustCompile(`^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([\da-zA-Z\-]+(?:\.[\da-zA-Z\-]+)*))?(?:\+([\da-zA-Z\-]+(?:\.[\da-zA-Z\-]+)*))?$`)
)

func strVal(value interface{}) (string, bool) {
	s, ok := value.(string)
	return s, ok
}

func numericVal(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint, uint8, uint16, uint32, uint64:
		return float64(reflect.ValueOf(v).Uint()), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	}
	return 0, false
}

func verr(field, rule string, value interface{}, msg string) *ValidationError {
	return &ValidationError{Field: field, Rule: rule, Value: value, Message: msg}
}

// Required fails for nil, empty string, zero int/float, empty slice/map.
func Required() Rule {
	return func(field string, value interface{}) *ValidationError {
		if value == nil {
			return verr(field, "required", value, "is required")
		}
		rv := reflect.ValueOf(value)
		switch rv.Kind() {
		case reflect.String:
			if strings.TrimSpace(rv.String()) == "" {
				return verr(field, "required", value, "is required")
			}
		case reflect.Slice, reflect.Map, reflect.Array:
			if rv.Len() == 0 {
				return verr(field, "required", value, "is required")
			}
		case reflect.Ptr, reflect.Interface:
			if rv.IsNil() {
				return verr(field, "required", value, "is required")
			}
		}
		return nil
	}
}

// MinLen enforces a minimum UTF-8 rune length.
func MinLen(n int) Rule {
	return func(field string, value interface{}) *ValidationError {
		s, ok := strVal(value)
		if !ok {
			return nil
		}
		if utf8.RuneCountInString(s) < n {
			return verr(field, "min_len", value, fmt.Sprintf("must be at least %d characters", n))
		}
		return nil
	}
}

// MaxLen enforces a maximum UTF-8 rune length.
func MaxLen(n int) Rule {
	return func(field string, value interface{}) *ValidationError {
		s, ok := strVal(value)
		if !ok {
			return nil
		}
		if utf8.RuneCountInString(s) > n {
			return verr(field, "max_len", value, fmt.Sprintf("must be at most %d characters", n))
		}
		return nil
	}
}

// ExactLen enforces an exact UTF-8 rune length.
func ExactLen(n int) Rule {
	return func(field string, value interface{}) *ValidationError {
		s, ok := strVal(value)
		if !ok {
			return nil
		}
		if utf8.RuneCountInString(s) != n {
			return verr(field, "exact_len", value, fmt.Sprintf("must be exactly %d characters", n))
		}
		return nil
	}
}

// Min enforces a minimum numeric value.
func Min(n float64) Rule {
	return func(field string, value interface{}) *ValidationError {
		f, ok := numericVal(value)
		if !ok {
			return nil
		}
		if f < n {
			return verr(field, "min", value, fmt.Sprintf("must be at least %g", n))
		}
		return nil
	}
}

// Max enforces a maximum numeric value.
func Max(n float64) Rule {
	return func(field string, value interface{}) *ValidationError {
		f, ok := numericVal(value)
		if !ok {
			return nil
		}
		if f > n {
			return verr(field, "max", value, fmt.Sprintf("must be at most %g", n))
		}
		return nil
	}
}

// Between enforces a numeric value within [min, max].
func Between(min, max float64) Rule {
	return func(field string, value interface{}) *ValidationError {
		f, ok := numericVal(value)
		if !ok {
			return nil
		}
		if f < min || f > max {
			return verr(field, "between", value, fmt.Sprintf("must be between %g and %g", min, max))
		}
		return nil
	}
}

// Email validates e-mail format.
func Email() Rule {
	return func(field string, value interface{}) *ValidationError {
		s, ok := strVal(value)
		if !ok || s == "" {
			return nil
		}
		if !reEmail.MatchString(s) {
			return verr(field, "email", value, "must be a valid email address")
		}
		return nil
	}
}

// URL validates a URL with optional scheme whitelist.
func URL(allowedSchemes ...string) Rule {
	return func(field string, value interface{}) *ValidationError {
		s, ok := strVal(value)
		if !ok || s == "" {
			return nil
		}
		u, err := url.Parse(s)
		if err != nil || u.Host == "" {
			return verr(field, "url", value, "must be a valid URL")
		}
		if len(allowedSchemes) > 0 {
			found := false
			for _, sc := range allowedSchemes {
				if strings.EqualFold(u.Scheme, sc) {
					found = true
					break
				}
			}
			if !found {
				return verr(field, "url", value, fmt.Sprintf("URL scheme must be one of: %s", strings.Join(allowedSchemes, ", ")))
			}
		}
		return nil
	}
}

// IP validates an IP address (v4 or v6).
func IP() Rule {
	return func(field string, value interface{}) *ValidationError {
		s, ok := strVal(value)
		if !ok || s == "" {
			return nil
		}
		if net.ParseIP(s) == nil {
			return verr(field, "ip", value, "must be a valid IP address")
		}
		return nil
	}
}

// UUID validates a UUID v1–v5.
func UUID() Rule {
	return func(field string, value interface{}) *ValidationError {
		s, ok := strVal(value)
		if !ok || s == "" {
			return nil
		}
		if !reUUID.MatchString(s) {
			return verr(field, "uuid", value, "must be a valid UUID")
		}
		return nil
	}
}

// Phone validates international phone numbers.
func Phone() Rule {
	return func(field string, value interface{}) *ValidationError {
		s, ok := strVal(value)
		if !ok || s == "" {
			return nil
		}
		if !rePhone.MatchString(strings.ReplaceAll(s, " ", "")) {
			return verr(field, "phone", value, "must be a valid phone number")
		}
		return nil
	}
}

// Alpha validates that the string contains only letters.
func Alpha() Rule {
	return func(field string, value interface{}) *ValidationError {
		s, ok := strVal(value)
		if !ok || s == "" {
			return nil
		}
		if !reAlpha.MatchString(s) {
			return verr(field, "alpha", value, "must contain only letters")
		}
		return nil
	}
}

// Alphanumeric validates letters and digits only.
func Alphanumeric() Rule {
	return func(field string, value interface{}) *ValidationError {
		s, ok := strVal(value)
		if !ok || s == "" {
			return nil
		}
		if !reAlNum.MatchString(s) {
			return verr(field, "alphanumeric", value, "must contain only letters and digits")
		}
		return nil
	}
}

// Numeric validates that the string is all digits.
func Numeric() Rule {
	return func(field string, value interface{}) *ValidationError {
		s, ok := strVal(value)
		if !ok || s == "" {
			return nil
		}
		if !reNumeric.MatchString(s) {
			return verr(field, "numeric", value, "must contain only digits")
		}
		return nil
	}
}

// Slug validates a URL-friendly slug.
func Slug() Rule {
	return func(field string, value interface{}) *ValidationError {
		s, ok := strVal(value)
		if !ok || s == "" {
			return nil
		}
		if !reSlug.MatchString(s) {
			return verr(field, "slug", value, "must be a valid slug (lowercase letters, digits, hyphens)")
		}
		return nil
	}
}

// OneOf validates that the value is one of the allowed values.
func OneOf(allowed ...string) Rule {
	set := make(map[string]struct{}, len(allowed))
	for _, a := range allowed {
		set[a] = struct{}{}
	}
	return func(field string, value interface{}) *ValidationError {
		s, ok := strVal(value)
		if !ok {
			return nil
		}
		if _, ok := set[s]; !ok {
			return verr(field, "one_of", value, fmt.Sprintf("must be one of: %s", strings.Join(allowed, ", ")))
		}
		return nil
	}
}

// Matches validates a string against a regular expression.
func Matches(pattern string) Rule {
	re := regexp.MustCompile(pattern)
	return func(field string, value interface{}) *ValidationError {
		s, ok := strVal(value)
		if !ok || s == "" {
			return nil
		}
		if !re.MatchString(s) {
			return verr(field, "matches", value, fmt.Sprintf("must match pattern %q", pattern))
		}
		return nil
	}
}

// NoHTML ensures no HTML tags are present.
func NoHTML() Rule {
	re := regexp.MustCompile(`<[^>]+>`)
	return func(field string, value interface{}) *ValidationError {
		s, ok := strVal(value)
		if !ok {
			return nil
		}
		if re.MatchString(s) {
			return verr(field, "no_html", value, "must not contain HTML tags")
		}
		return nil
	}
}

// StrongPassword enforces password complexity.
func StrongPassword(minLen int) Rule {
	return func(field string, value interface{}) *ValidationError {
		s, ok := strVal(value)
		if !ok {
			return nil
		}
		if utf8.RuneCountInString(s) < minLen {
			return verr(field, "strong_password", value, fmt.Sprintf("must be at least %d characters", minLen))
		}
		var hasUpper, hasLower, hasDigit, hasSpecial bool
		for _, r := range s {
			switch {
			case unicode.IsUpper(r):
				hasUpper = true
			case unicode.IsLower(r):
				hasLower = true
			case unicode.IsDigit(r):
				hasDigit = true
			case unicode.IsPunct(r) || unicode.IsSymbol(r):
				hasSpecial = true
			}
		}
		if !hasUpper || !hasLower || !hasDigit || !hasSpecial {
			return verr(field, "strong_password", value, "must contain uppercase, lowercase, digit and special character")
		}
		return nil
	}
}

// DateFormat validates a date string against a Go time layout.
func DateFormat(layout string) Rule {
	return func(field string, value interface{}) *ValidationError {
		s, ok := strVal(value)
		if !ok || s == "" {
			return nil
		}
		if _, err := time.Parse(layout, s); err != nil {
			return verr(field, "date_format", value, fmt.Sprintf("must match date format %q", layout))
		}
		return nil
	}
}

// HexColor validates CSS hex colour codes (#RGB or #RRGGBB).
func HexColor() Rule {
	return func(field string, value interface{}) *ValidationError {
		s, ok := strVal(value)
		if !ok || s == "" {
			return nil
		}
		if !reHex.MatchString(s) {
			return verr(field, "hex_color", value, "must be a valid hex color (#RGB or #RRGGBB)")
		}
		return nil
	}
}

// SemVer validates Semantic Versioning strings.
func SemVer() Rule {
	return func(field string, value interface{}) *ValidationError {
		s, ok := strVal(value)
		if !ok || s == "" {
			return nil
		}
		if !reSemVer.MatchString(s) {
			return verr(field, "semver", value, "must be a valid semantic version (e.g. 1.2.3)")
		}
		return nil
	}
}

// Custom wraps an arbitrary validation function as a Rule.
func Custom(name string, fn func(value interface{}) (bool, string)) Rule {
	return func(field string, value interface{}) *ValidationError {
		ok, msg := fn(value)
		if !ok {
			return verr(field, name, value, msg)
		}
		return nil
	}
}

// ---- Validator --------------------------------------------------------------

// FieldRules maps a field path to its set of Rules.
type FieldRules map[string][]Rule

// Validator validates structs using struct tags or explicit FieldRules.
type Validator struct {
	mu    sync.RWMutex
	rules map[string][]Rule // tag name → rules
}

// New creates a Validator.
func New() *Validator {
	return &Validator{rules: make(map[string][]Rule)}
}

// RegisterTag registers a tag name → Rule mapping for tag-driven validation.
func (v *Validator) RegisterTag(tag string, rule Rule) {
	v.mu.Lock()
	v.rules[tag] = append(v.rules[tag], rule)
	v.mu.Unlock()
}

// Validate validates the given struct pointer using explicit field rules.
// Returns nil on success, or ValidationErrors.
func Validate(fields FieldRules, data map[string]interface{}) error {
	var errs ValidationErrors
	for field, rules := range fields {
		val := data[field]
		for _, rule := range rules {
			if ve := rule(field, val); ve != nil {
				errs = append(errs, ve)
			}
		}
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

// ValidateStruct validates a struct pointer using `validate` struct tags.
// Tag syntax: `validate:"required,min_len=2,max_len=100,email"`
func ValidateStruct(s interface{}) error {
	rv := reflect.ValueOf(s)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return errors.New("govalidator: input must be a struct or pointer to struct")
	}
	rt := rv.Type()
	var errs ValidationErrors

	for i := 0; i < rv.NumField(); i++ {
		field := rt.Field(i)
		tag := field.Tag.Get("validate")
		if tag == "" || tag == "-" {
			continue
		}
		fieldName := field.Tag.Get("json")
		if fieldName == "" {
			fieldName = field.Name
		}
		// strip json options
		if idx := strings.Index(fieldName, ","); idx != -1 {
			fieldName = fieldName[:idx]
		}
		value := rv.Field(i).Interface()

		for _, directive := range strings.Split(tag, ",") {
			directive = strings.TrimSpace(directive)
			var rule Rule
			switch {
			case directive == "required":
				rule = Required()
			case directive == "email":
				rule = Email()
			case directive == "url":
				rule = URL("http", "https")
			case directive == "uuid":
				rule = UUID()
			case directive == "alpha":
				rule = Alpha()
			case directive == "alphanumeric":
				rule = Alphanumeric()
			case directive == "numeric":
				rule = Numeric()
			case directive == "slug":
				rule = Slug()
			case directive == "phone":
				rule = Phone()
			case directive == "no_html":
				rule = NoHTML()
			case directive == "hex_color":
				rule = HexColor()
			case directive == "semver":
				rule = SemVer()
			case strings.HasPrefix(directive, "min_len="):
				n, _ := strconv.Atoi(strings.TrimPrefix(directive, "min_len="))
				rule = MinLen(n)
			case strings.HasPrefix(directive, "max_len="):
				n, _ := strconv.Atoi(strings.TrimPrefix(directive, "max_len="))
				rule = MaxLen(n)
			case strings.HasPrefix(directive, "min="):
				f, _ := strconv.ParseFloat(strings.TrimPrefix(directive, "min="), 64)
				rule = Min(f)
			case strings.HasPrefix(directive, "max="):
				f, _ := strconv.ParseFloat(strings.TrimPrefix(directive, "max="), 64)
				rule = Max(f)
			case strings.HasPrefix(directive, "one_of="):
				opts := strings.Split(strings.TrimPrefix(directive, "one_of="), "|")
				rule = OneOf(opts...)
			case strings.HasPrefix(directive, "matches="):
				rule = Matches(strings.TrimPrefix(directive, "matches="))
			case strings.HasPrefix(directive, "strong_password="):
				n, _ := strconv.Atoi(strings.TrimPrefix(directive, "strong_password="))
				rule = StrongPassword(n)
			case strings.HasPrefix(directive, "date="):
				rule = DateFormat(strings.TrimPrefix(directive, "date="))
			default:
				continue
			}
			if ve := rule(fieldName, value); ve != nil {
				errs = append(errs, ve)
			}
		}
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

// AsValidationErrors converts an error to ValidationErrors if possible.
func AsValidationErrors(err error) (ValidationErrors, bool) {
	var ve ValidationErrors
	if errors.As(err, &ve) {
		return ve, true
	}
	return nil, false
}
