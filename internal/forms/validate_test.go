package forms_test

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/nahuelsantos/contact-api/internal/config"
	"github.com/nahuelsantos/contact-api/internal/forms"
)

func form(fields ...config.Field) *config.Form {
	for i := range fields {
		if fields[i].MaxLen == 0 {
			fields[i].MaxLen = 500
		}
	}

	return &config.Form{Name: "contact", Fields: fields}
}

func TestValidate_Required(t *testing.T) {
	f := form(config.Field{Name: "a", Type: "text", Required: true}, config.Field{Name: "b", Type: "text", Required: true})
	_, errs := forms.Validate(f, map[string]string{"b": "   "})
	eq(t, forms.FieldErrors{"a": "required", "b": "required"}, errs)
}

func TestValidate_TooLong(t *testing.T) {
	f := form(config.Field{Name: "a", Type: "text"})
	_, errs := forms.Validate(f, map[string]string{"a": strings.Repeat("x", 501)})
	eq(t, "too_long", errs["a"])

	vals, errs := forms.Validate(f, map[string]string{"a": strings.Repeat("ñ", 500)})
	empty(t, errs)
	eq(t, strings.Repeat("ñ", 500), vals.Get("a"))
}

func TestValidate_EmailTrimmedAndBareOnly(t *testing.T) {
	f := form(config.Field{Name: "e", Type: "email", Required: true})
	vals, errs := forms.Validate(f, map[string]string{"e": " Juan@Mail.com "})
	empty(t, errs)
	eq(t, "Juan@Mail.com", vals.Get("e"))
	eq(t, "Juan@Mail.com", vals.FirstEmail())

	for _, bad := range []string{"Juan <j@x.com>", "nope"} {
		_, errs = forms.Validate(f, map[string]string{"e": bad})
		eq(t, "invalid_email", errs["e"], bad)
	}
}

func TestValidate_Select(t *testing.T) {
	f := form(config.Field{Name: "s", Type: "select", Options: []string{"a", "b"}})
	_, errs := forms.Validate(f, map[string]string{"s": "c"})
	eq(t, "invalid_option", errs["s"])
	_, errs = forms.Validate(f, map[string]string{"s": "b"})
	empty(t, errs)
}

func TestValidate_Number(t *testing.T) {
	f := form(config.Field{Name: "n", Type: "number"})
	_, errs := forms.Validate(f, map[string]string{"n": "12.5"})
	empty(t, errs)
	for _, bad := range []string{"doce", "NaN", "Inf"} {
		_, errs = forms.Validate(f, map[string]string{"n": bad})
		eq(t, "invalid_number", errs["n"], bad)
	}
}

func TestValidate_Country(t *testing.T) {
	f := form(config.Field{Name: "c", Type: "country"})
	vals, errs := forms.Validate(f, map[string]string{"c": "es"})
	empty(t, errs)
	eq(t, "ES", vals.Get("c"))
	_, errs = forms.Validate(f, map[string]string{"c": "XX"})
	eq(t, "invalid_country", errs["c"])
}

func TestValidate_Checkbox(t *testing.T) {
	req := form(config.Field{Name: "k", Type: "checkbox", Required: true})
	_, errs := forms.Validate(req, map[string]string{})
	eq(t, "required", errs["k"])
	for _, on := range []string{"on", "true"} {
		vals, errs := forms.Validate(req, map[string]string{"k": on})
		empty(t, errs)
		eq(t, "sí", vals.Get("k"))
	}

	opt := form(config.Field{Name: "k", Type: "checkbox"})
	vals, errs := forms.Validate(opt, map[string]string{})
	empty(t, errs)
	eq(t, "no", vals.Get("k"))
}

func TestValidate_Pattern(t *testing.T) {
	f := form(config.Field{Name: "p", Type: "text", Pattern: `^\d+$`, Re: regexp.MustCompile(`^\d+$`)})
	_, errs := forms.Validate(f, map[string]string{"p": "abc"})
	eq(t, "invalid_format", errs["p"])
	_, errs = forms.Validate(f, map[string]string{"p": " 123 "})
	empty(t, errs)
}

func TestValidate_DropsUnknownAndReserved(t *testing.T) {
	f := form(config.Field{Name: "a", Type: "text"})
	vals, _ := forms.Validate(f, map[string]string{"a": "x", "hack": "1", "_gotcha": "", "cf-turnstile-response": "t"})
	if len(vals) != 1 {
		t.Fatalf("want 1 value, got %v", vals)
	}
	eq(t, "a", vals[0].Name)
}

func TestValidate_AllErrorsAtOnce(t *testing.T) {
	f := form(
		config.Field{Name: "a", Type: "text", Required: true},
		config.Field{Name: "b", Type: "email"},
		config.Field{Name: "c", Type: "number"},
	)
	_, errs := forms.Validate(f, map[string]string{"b": "x", "c": "y"})
	if len(errs) != 3 {
		t.Errorf("want 3 errors, got %v", errs)
	}
}

func TestValues_OrderFollowsConfig(t *testing.T) {
	f := form(
		config.Field{Name: "z", Type: "text", Label: "Zed"},
		config.Field{Name: "a", Type: "text"},
		config.Field{Name: "m", Type: "textarea"},
	)
	vals, errs := forms.Validate(f, map[string]string{"m": "3", "a": "2", "z": "1"})
	empty(t, errs)
	if len(vals) != 3 {
		t.Fatalf("want 3 values, got %v", vals)
	}
	eq(t, []string{"z", "a", "m"}, []string{vals[0].Name, vals[1].Name, vals[2].Name})
	eq(t, "Zed", vals[0].Label)
	eq(t, "a", vals[1].Label)
}

func eq(t *testing.T, want, got any, msg ...string) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Errorf("%v: want %#v, got %#v", msg, want, got)
	}
}

func empty(t *testing.T, errs forms.FieldErrors) {
	t.Helper()
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
}
