package ratelimit

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
)

var ErrEmptyIdentity = errors.New("empty key identity")

type Key struct {
	Namespace  string
	Identity   string
	Route      string
	Method     string
	Tenant     string
	Dimensions map[string]string
}

func NewKey(namespace, identity string) Key {
	return Key{Namespace: namespace, Identity: identity, Dimensions: map[string]string{}}
}

func (k Key) WithDimension(name, value string) Key {
	name = strings.TrimSpace(name)
	if name == "" {
		return k
	}
	if k.Dimensions == nil {
		k.Dimensions = map[string]string{}
	}
	k.Dimensions[name] = strings.TrimSpace(value)
	return k
}

func (k Key) Normalized() Key {
	k.Namespace = normalizePart(k.Namespace)
	k.Identity = normalizePart(k.Identity)
	k.Route = normalizePart(k.Route)
	k.Method = strings.ToUpper(strings.TrimSpace(k.Method))
	k.Tenant = normalizePart(k.Tenant)
	if k.Dimensions == nil {
		k.Dimensions = map[string]string{}
	}
	return k
}

func (k Key) Validate() error {
	nk := k.Normalized()
	if nk.Identity == "" {
		return ErrEmptyIdentity
	}
	return nil
}

func (k Key) String() string {
	nk := k.Normalized()
	base := nk.Identity
	if nk.Namespace != "" {
		base = nk.Namespace + ":" + base
	}
	parts := make([]string, 0, 4+len(nk.Dimensions))
	if nk.Route != "" {
		parts = append(parts, "route="+nk.Route)
	}
	if nk.Method != "" {
		parts = append(parts, "method="+nk.Method)
	}
	if nk.Tenant != "" {
		parts = append(parts, "tenant="+nk.Tenant)
	}
	if len(nk.Dimensions) > 0 {
		keys := make([]string, 0, len(nk.Dimensions))
		for k := range nk.Dimensions {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, dk := range keys {
			parts = append(parts, normalizePart(dk)+"="+normalizePart(nk.Dimensions[dk]))
		}
	}
	if len(parts) == 0 {
		return base
	}
	return base + "|" + strings.Join(parts, "|")
}

func (k Key) HashString() string {
	s := k.String()
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func normalizePart(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, " ", "_")
	return s
}
