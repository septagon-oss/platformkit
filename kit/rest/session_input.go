package rest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	"github.com/septagon-oss/platformkit/kit/entity"
)

// GuidedValues reads a closed session form through the ordinary command decoder.
// Unknown, missing and repeated arguments refuse before any callback runs.
func GuidedValues(body []byte, fields []entity.Field) (map[string]any, error) {
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, invalid("body", "is not a form")
	}
	for key, values := range form {
		if _, ok := entity.FieldNamed(fields, key); !ok || len(values) != 1 {
			return nil, invalid(key, "is unknown or repeated")
		}
	}
	for _, field := range fields {
		if len(form[field.Name]) != 1 || form.Get(field.Name) == "" {
			return nil, invalid(field.Name, "is required")
		}
	}
	return CommandValues(body, fields)
}

func sessionJSON[I any](body []byte) (I, error) {
	var zero I
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return zero, invalid("body", "must be an object")
	}
	seen := map[string]bool{}
	fields := entity.FieldsOf(reflect.TypeFor[I]())
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return zero, invalid("body", "is malformed")
		}
		name, ok := token.(string)
		if !ok || seen[name] {
			return zero, invalid("body", "has repeated members")
		}
		if _, ok := entity.FieldNamed(fields, name); !ok {
			return zero, invalid(name, "is unknown")
		}
		seen[name] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || bytes.Equal(value, []byte("null")) {
			return zero, invalid(name, "is required")
		}
	}
	if len(seen) != len(fields) {
		return zero, invalid("body", "is missing arguments")
	}
	var in I
	if err := json.Unmarshal(body, &in); err != nil {
		return zero, invalid("body", "has invalid argument types")
	}
	return in, sessionInput(in)
}

func sessionRequest[I any](req *http.Request) (I, error) {
	var zero I
	body, err := io.ReadAll(io.LimitReader(req.Body, 1<<20))
	if err != nil {
		return zero, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	if strings.HasPrefix(req.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		values, err := GuidedValues(body, entity.FieldsOf(reflect.TypeFor[I]()))
		if err != nil {
			return zero, err
		}
		body, err = json.Marshal(values)
		if err != nil {
			return zero, err
		}
	}
	return sessionJSON[I](body)
}
