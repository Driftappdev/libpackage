package schema

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

type Engine struct{}

func NewEngine() *Engine { return &Engine{} }

func (e *Engine) Validate(v any) error {
	return ValidateStruct(v)
}

var emailRE = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func ValidateStruct(v any) error {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return errors.New("schema: nil pointer")
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return errors.New("schema: expected struct")
	}
	rt := rv.Type()
	var errs []error
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		rules := field.Tag.Get("validate")
		if rules == "" || field.PkgPath != "" {
			continue
		}
		value := rv.Field(i)
		if err := validateField(field.Name, value, rules); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func validateField(name string, v reflect.Value, rules string) error {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			if strings.Contains(rules, "required") {
				return fmt.Errorf("%s: required", name)
			}
			return nil
		}
		v = v.Elem()
	}
	for _, rule := range strings.Split(rules, ",") {
		switch {
		case rule == "required":
			if v.IsZero() {
				return fmt.Errorf("%s: required", name)
			}
		case rule == "email":
			if s := fmt.Sprint(v.Interface()); s != "" && !emailRE.MatchString(s) {
				return fmt.Errorf("%s: invalid email", name)
			}
		case strings.HasPrefix(rule, "min="):
			n, _ := strconv.Atoi(strings.TrimPrefix(rule, "min="))
			if lengthOf(v) < n {
				return fmt.Errorf("%s: min=%d", name, n)
			}
		case strings.HasPrefix(rule, "max="):
			n, _ := strconv.Atoi(strings.TrimPrefix(rule, "max="))
			if lengthOf(v) > n {
				return fmt.Errorf("%s: max=%d", name, n)
			}
		case strings.HasPrefix(rule, "oneof="):
			want := strings.Split(strings.TrimPrefix(rule, "oneof="), "|")
			got := fmt.Sprint(v.Interface())
			ok := false
			for _, item := range want {
				if got == item {
					ok = true
					break
				}
			}
			if !ok {
				return fmt.Errorf("%s: value must be one of %v", name, want)
			}
		}
	}
	return nil
}

func lengthOf(v reflect.Value) int {
	switch v.Kind() {
	case reflect.String, reflect.Array, reflect.Slice, reflect.Map:
		return v.Len()
	default:
		return len(fmt.Sprint(v.Interface()))
	}
}
