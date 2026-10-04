//go:build !windows

package scan

import "errors"

type regRoot uintptr

const (
	regHKCU regRoot = iota
	regHKLM
)

var errNoRegistry = errors.New("registry not available on this platform")

func regGetString(root regRoot, path, name string) (string, error) { return "", errNoRegistry }

func regSubKeys(root regRoot, path string) ([]string, error) { return nil, errNoRegistry }
