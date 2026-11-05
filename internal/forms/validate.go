// Package forms validates submissions against form definitions.
package forms

import (
	"math"
	"net/mail"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nahuelsantos/contact-api/internal/config"
)

// Error codes reported per field.
const (
	CodeRequired      = "required"
	CodeTooLong       = "too_long"
	CodeInvalidEmail  = "invalid_email"
	CodeInvalidOption = "invalid_option"
	CodeInvalidNumber = "invalid_number"
	CodeInvalidCtry   = "invalid_country"
	CodeInvalidFormat = "invalid_format"
)

const (
	checkboxYes = "sí"
	checkboxNo  = "no"
	typeEmail   = "email"
)

// Value is one validated field of a submission.
type Value struct{ Name, Label, Type, Value string }

// Values holds the validated fields in config order.
type Values []Value

// Get returns the value of the named field, or "" if absent.
func (v Values) Get(name string) string {
	for _, x := range v {
		if x.Name == name {
			return x.Value
		}
	}

	return ""
}

// FirstEmail returns the value of the first email-type field.
func (v Values) FirstEmail() string {
	for _, x := range v {
		if x.Type == typeEmail {
			return x.Value
		}
	}

	return ""
}

// FieldErrors maps field name to error code.
type FieldErrors map[string]string

// Validate checks in against the form definition. Inputs not defined in the
// form (including reserved fields) are dropped. Values has one entry per
// configured field; errs is empty when the submission is valid.
func Validate(f *config.Form, in map[string]string) (Values, FieldErrors) {
	vals := make(Values, 0, len(f.Fields))
	errs := FieldErrors{}

	for i := range f.Fields {
		fld := &f.Fields[i]
		raw, present := in[fld.Name]
		val := strings.TrimSpace(raw)

		if fld.Type == "checkbox" {
			val = checkbox(val, present)
		}

		if code := check(fld, val); code != "" {
			errs[fld.Name] = code
		} else if fld.Type == "country" {
			val = strings.ToUpper(val)
		} else if fld.Type == "checkbox" && val == "" {
			val = checkboxNo
		}

		label := fld.Label
		if label == "" {
			label = fld.Name
		}
		vals = append(vals, Value{Name: fld.Name, Label: label, Type: fld.Type, Value: val})
	}

	return vals, errs
}

// checkbox normalises a checkbox to "sí"/"no"; a required unticked box stays "".
func checkbox(val string, present bool) string {
	if present && val != "" && !strings.EqualFold(val, "false") && !strings.EqualFold(val, "off") && val != "0" {
		return checkboxYes
	}

	return ""
}

func check(fld *config.Field, val string) string {
	if val == "" {
		if fld.Required {
			return CodeRequired
		}

		return ""
	}
	if fld.MaxLen > 0 && utf8.RuneCountInString(val) > fld.MaxLen {
		return CodeTooLong
	}
	if code := checkType(fld, val); code != "" {
		return code
	}
	if fld.Re != nil && !fld.Re.MatchString(val) {
		return CodeInvalidFormat
	}

	return ""
}

func checkType(fld *config.Field, val string) string {
	switch fld.Type {
	case typeEmail:
		addr, err := mail.ParseAddress(val)
		if err != nil || addr.Name != "" || addr.Address != val {
			return CodeInvalidEmail
		}
	case "select":
		for _, o := range fld.Options {
			if o == val {
				return ""
			}
		}

		return CodeInvalidOption
	case "number":
		if n, err := strconv.ParseFloat(val, 64); err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return CodeInvalidNumber
		}
	case "country":
		if _, ok := countries[strings.ToUpper(val)]; !ok {
			return CodeInvalidCtry
		}
	}

	return ""
}
