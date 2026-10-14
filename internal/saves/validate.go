// Package saves manages world saves: each save is a self-contained server
// working directory (universe, mods, config and player lists).
package saves

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Validation errors.
var (
	ErrNotFound    = errors.New("élément introuvable")
	ErrInvalidID   = errors.New("identifiant invalide")
	ErrInvalidName = errors.New("nom invalide (1 à 64 caractères imprimables)")
)

var idPattern = regexp.MustCompile(`^[a-f0-9]{12}$`)

const maxNameLength = 64

func newID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func validID(id string) bool { return idPattern.MatchString(id) }

// normalizeName trims and validates a human readable name.
func normalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxNameLength {
		return "", ErrInvalidName
	}
	for _, r := range name {
		if !unicode.IsPrint(r) {
			return "", ErrInvalidName
		}
	}
	return name, nil
}
