package snapshot

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/zekihan/external-secrets-sops/internal/config"
)

type address struct {
	Key
	name string
}
type resolutionError struct {
	location address
	cause    error
}

func (e *resolutionError) Error() string {
	component := func(s string) string {
		if len(s) > 128 {
			return s[:128] + "..."
		}
		return s
	}
	return fmt.Sprintf(
		"secret %q/%q/%q/%q: %v",
		component(e.location.Project),
		component(e.location.Config),
		component(e.location.Environment),
		component(e.location.name),
		e.cause,
	)
}

func (e *resolutionError) Unwrap() error { return e.cause }

type resolved struct {
	value  string
	height int
}
type resolver struct {
	ctx         context.Context
	raw         map[Key]map[string]scalar
	limits      config.Limits
	memo        map[address]resolved
	visiting    map[address]bool
	evaluations int
	size        int64
}

func resolve(
	ctx context.Context,
	raw map[Key]map[string]scalar,
	limits config.Limits,
) (map[Key]map[string]string, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	r := resolver{
		ctx:      ctx,
		raw:      raw,
		limits:   limits,
		memo:     make(map[address]resolved),
		visiting: make(map[address]bool),
	}
	var addresses []address
	data := make(map[Key]map[string]string, len(raw))
	for k, values := range raw {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		data[k] = make(map[string]string, len(values))
		for name := range values {
			addresses = append(addresses, address{k, name})
		}
	}
	sort.Slice(addresses, func(i, j int) bool {
		a, b := addresses[i], addresses[j]
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		if a.Config != b.Config {
			return a.Config < b.Config
		}
		if a.Environment != b.Environment {
			return a.Environment < b.Environment
		}
		return a.name < b.name
	})
	for _, a := range addresses {
		v, err := r.value(a, 0)
		if err != nil {
			return nil, 0, err
		}
		data[a.Key][a.name] = v.value
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	return data, r.size, nil
}

func (r *resolver) value(a address, depth int) (result resolved, err error) {
	defer func() {
		var located *resolutionError
		if err != nil && !errors.As(err, &located) {
			err = &resolutionError{location: a, cause: err}
		}
	}()
	if err = r.ctx.Err(); err != nil {
		return result, err
	}
	if depth > r.limits.TemplateDepth {
		return result, errors.New("reference depth limit exceeded")
	}
	if v, ok := r.memo[a]; ok {
		if depth+v.height > r.limits.TemplateDepth {
			return result, errors.New("reference depth limit exceeded")
		}
		return v, nil
	}
	if r.visiting[a] {
		return result, errors.New("reference cycle")
	}
	s, ok := r.raw[a.Key][a.name]
	if !ok {
		return result, errors.New("reference target unavailable")
	}
	r.visiting[a] = true
	defer delete(r.visiting, a)
	if int64(len(a.name)) > r.limits.SnapshotBytes-r.size {
		return result, errors.New("resolved snapshot size limit exceeded")
	}
	r.size += int64(len(a.name))
	var out strings.Builder
	appendText := func(text string) error {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		n := int64(out.Len()) + int64(len(text))
		if n > r.limits.TemplateValueBytes || int64(len(text)) > r.limits.SnapshotBytes-r.size {
			return errors.New("resolved secret size limit exceeded")
		}
		r.size += int64(len(text))
		out.WriteString(text)
		return nil
	}
	reference := func(text, encoder string) error {
		r.evaluations++
		if r.evaluations > r.limits.TemplateEvaluations {
			return errors.New("reference evaluation limit exceeded")
		}
		target, e := parseReference(a.Key, text)
		if e != nil {
			return e
		}
		// A missing target is descriptor content, not an existing secret location.
		// Attribute the failure to the source instead of echoing the reference.
		if _, exists := r.raw[target.Key][target.name]; !exists {
			return errors.New("reference target unavailable")
		}
		v, e := r.value(target, depth+1)
		if e != nil {
			return e
		}
		result.height = max(result.height, v.height+1)
		return encode(v.value, encoder, appendText)
	}
	switch s.kind {
	case "":
		err = appendText(s.value)
	case "$ref":
		err = reference(s.value, "")
	case "$template":
		text := s.value
		for len(text) > 0 && err == nil {
			if e := r.ctx.Err(); e != nil {
				return result, e
			}
			switch {
			case strings.HasPrefix(text, "$${"):
				err = appendText("${")
				text = text[3:]
			case strings.HasPrefix(text, "${"):
				end := strings.IndexByte(text[2:], '}')
				if end < 0 {
					return result, errors.New("unterminated interpolation")
				}
				expression := text[2 : 2+end]
				parts := strings.Split(expression, "|")
				if len(parts) > 2 {
					return result, errors.New("invalid interpolation")
				}
				encoder := ""
				if len(parts) == 2 {
					encoder = strings.TrimSpace(parts[1])
					if encoder == "" {
						return result, errors.New("missing encoder")
					}
				}
				err = reference(strings.TrimSpace(parts[0]), encoder)
				text = text[3+end:]
			default:
				n := 1
				for n < len(text) && text[n] != '$' {
					n++
				}
				err = appendText(text[:n])
				text = text[n:]
			}
		}
	default:
		err = errors.New("invalid secret descriptor")
	}
	if err != nil {
		return result, err
	}
	result.value = out.String()
	r.memo[a] = result
	return result, nil
}

func parseReference(current Key, text string) (address, error) {
	parts := strings.Split(text, "/")
	if len(parts) != 1 && len(parts) != 3 && len(parts) != 4 {
		return address{}, errors.New("invalid reference syntax")
	}
	for i, p := range parts {
		// Only unreserved characters and literal '+' may appear without encoding.
		for j := 0; j < len(p); j++ {
			c := p[j]
			if c == '%' {
				if j+2 >= len(p) {
					return address{}, errors.New("invalid reference escape")
				}
				j += 2
				continue
			}
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') &&
				!strings.ContainsRune("-._~+", rune(c)) {
				return address{}, errors.New("unescaped reference character")
			}
		}
		decoded, err := url.PathUnescape(p)
		if err != nil || !utf8.ValidString(decoded) || !config.Component(decoded) {
			return address{}, errors.New("invalid reference component")
		}
		parts[i] = decoded
	}
	a := address{Key: current, name: parts[len(parts)-1]}
	if a.name == "<<" {
		return address{}, errors.New("invalid reference key")
	}
	if len(parts) >= 3 {
		a.Project = parts[0]
		a.Config = parts[1]
	}
	if len(parts) == 4 {
		a.Environment = parts[2]
	}
	return a, nil
}

func encode(value, encoder string, appendText func(string) error) error {
	switch encoder {
	case "":
		return appendText(value)
	case "base64":
		// Encode bounded chunks, without allocating an expanded copy of the input.
		for len(value) > 0 {
			n := min(len(value), 3072)
			if err := appendText(base64.StdEncoding.EncodeToString([]byte(value[:n]))); err != nil {
				return err
			}
			value = value[n:]
		}
		return nil
	case "urlquery", "urlpath", "urluserinfo":
		for i := 0; i < len(value); i++ {
			part := value[i : i+1]
			switch encoder {
			case "urlquery":
				part = url.QueryEscape(part)
			case "urlpath":
				part = url.PathEscape(part)
			case "urluserinfo":
				part = url.User(part).String()
			}
			if err := appendText(part); err != nil {
				return err
			}
		}
		return nil
	case "json":
		if err := appendText("\""); err != nil {
			return err
		}
		for _, c := range value {
			part, _ := json.Marshal(string(c))
			if err := appendText(string(part[1 : len(part)-1])); err != nil {
				return err
			}
		}
		return appendText("\"")
	default:
		return errors.New("unknown template encoder")
	}
}
