package main

import (
	"bytes"
	"fmt"
	"strings"
)

type headerSet struct {
	Name  string
	Value string
}

func parseHeaderSet(raw string) ([]headerSet, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var out []headerSet
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if !ok || name == "" || strings.ContainsAny(name, " \t") {
			return nil, fmt.Errorf("SMTP_DEBUG_HEADER_SET line must be Header-Name: value")
		}
		out = append(out, headerSet{Name: name, Value: value})
	}
	return out, nil
}

func envelopeFrom(from string, sets []headerSet) string {
	for i := len(sets) - 1; i >= 0; i-- {
		if strings.EqualFold(sets[i].Name, "Return-Path") {
			return angleAddr(sets[i].Value)
		}
	}
	return from
}

func angleAddr(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && strings.HasPrefix(v, "<") && strings.HasSuffix(v, ">") {
		return strings.TrimSpace(v[1 : len(v)-1])
	}
	return v
}

func applyHeaderSet(raw []byte, sets []headerSet) []byte {
	sets = lastHeaderSet(sets)
	if len(sets) == 0 {
		return raw
	}

	header, body, nl := splitHeaderBytes(raw)
	lines := headerLines(header)
	drop := map[string]bool{}
	for _, set := range sets {
		drop[strings.ToLower(set.Name)] = true
	}

	var b strings.Builder
	for _, set := range sets {
		b.WriteString(set.Name)
		b.WriteString(": ")
		b.WriteString(set.Value)
		b.WriteString(nl)
	}
	var field []string
	flush := func() {
		if len(field) == 0 {
			return
		}
		name, _, ok := strings.Cut(field[0], ":")
		if ok && drop[strings.ToLower(strings.TrimSpace(name))] {
			field = nil
			return
		}
		for _, line := range field {
			b.WriteString(line)
			b.WriteString(nl)
		}
		field = nil
	}
	for _, line := range lines {
		if len(field) > 0 && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			field = append(field, line)
			continue
		}
		flush()
		field = []string{line}
	}
	flush()
	b.WriteString(nl)
	b.Write(body)
	return []byte(b.String())
}

func lastHeaderSet(sets []headerSet) []headerSet {
	if len(sets) == 0 {
		return nil
	}
	index := map[string]int{}
	var out []headerSet
	for _, set := range sets {
		key := strings.ToLower(set.Name)
		if i, ok := index[key]; ok {
			out[i] = set
			continue
		}
		index[key] = len(out)
		out = append(out, set)
	}
	return out
}

func splitHeaderBytes(raw []byte) (header, body []byte, nl string) {
	if i := bytes.Index(raw, []byte("\r\n\r\n")); i >= 0 {
		return raw[:i], raw[i+4:], "\r\n"
	}
	if i := bytes.Index(raw, []byte("\n\n")); i >= 0 {
		return raw[:i], raw[i+2:], "\n"
	}
	return raw, nil, "\r\n"
}

func headerLines(header []byte) []string {
	s := strings.ReplaceAll(string(header), "\r\n", "\n")
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
