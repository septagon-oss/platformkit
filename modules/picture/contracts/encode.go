package contracts

import (
	"encoding/xml"
	"maps"
	"slices"
)

func encode(root, background *element, state func(*element, map[string]string) bool) (string, error) {
	var out boundedOutput
	enc := xml.NewEncoder(&out)
	var emit func(*element) error
	emit = func(e *element) error {
		attrs := maps.Clone(e.attrs)
		if !state(e, attrs) {
			return nil
		}
		start := xml.StartElement{Name: xml.Name{Local: e.name}}
		for _, k := range slices.Sorted(maps.Keys(attrs)) {
			start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: k}, Value: attrs[k]})
		}
		if err := enc.EncodeToken(start); err != nil {
			return err
		}
		if e == root && background != nil {
			if err := emit(background); err != nil {
				return err
			}
		}
		if e.text != "" {
			if err := enc.EncodeToken(xml.CharData(e.text)); err != nil {
				return err
			}
		}
		for _, child := range e.children {
			if err := emit(child); err != nil {
				return err
			}
		}
		return enc.EncodeToken(start.End())
	}
	if err := emit(root); err != nil {
		return "", err
	}
	if err := enc.Flush(); err != nil {
		return "", err
	}
	return out.String(), nil
}
