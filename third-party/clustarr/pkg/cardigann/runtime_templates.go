package cardigann

import (
	"reflect"
	"strings"
)

// RuntimeTemplatesSupported rejects unknown template syntax before a definition
// is offered for execution. Uses the engine's exact parser and function set.
func RuntimeTemplatesSupported(def *Definition) bool {
	ok := true
	var walk func(reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.String:
			if strings.Contains(v.String(), "{{") {
				if _, err := parseTemplate(v.String(), nil); err != nil {
					ok = false
				}
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				walk(v.Field(i))
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Map:
			it := v.MapRange()
			for it.Next() {
				walk(it.Key())
				walk(it.Value())
			}
		}
	}
	walk(reflect.ValueOf(def))
	return ok
}
