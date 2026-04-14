package govalidate

import (
	"errors"
	"regexp"
	"strings"
)

var (
	emailPattern = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
)

func Required(name, v string) error {
	if strings.TrimSpace(v) == "" {
		return errors.New(name + " is required")
	}
	return nil
}

func MaxLen(name, v string, n int) error {
	if len(v) > n {
		return errors.New(name + " exceeds max length")
	}
	return nil
}

func OneOf(name, v string, allowed ...string) error {
	for _, a := range allowed {
		if strings.EqualFold(v, a) {
			return nil
		}
	}
	return errors.New(name + " is invalid")
}

func Email(v string) error {
	if !emailPattern.MatchString(strings.TrimSpace(v)) {
		return errors.New("email is invalid")
	}
	return nil
}
