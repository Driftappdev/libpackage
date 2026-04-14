package goschema

import (
	"fmt"
	"reflect"
)

type FieldRule struct {
	Required bool
	TypeKind reflect.Kind
}

type Schema map[string]FieldRule

func Validate(input map[string]any, schema Schema) error {
	for name, rule := range schema {
		v, ok := input[name]
		if rule.Required && !ok {
			return fmt.Errorf("field %q is required", name)
		}
		if ok && rule.TypeKind != reflect.Invalid && reflect.TypeOf(v).Kind() != rule.TypeKind {
			return fmt.Errorf("field %q invalid type", name)
		}
	}
	return nil
}
