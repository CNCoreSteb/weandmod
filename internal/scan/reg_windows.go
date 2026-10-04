//go:build windows

package scan

import (
	"golang.org/x/sys/windows/registry"
)

type regRoot = registry.Key

const (
	regHKCU regRoot = registry.CURRENT_USER
	regHKLM regRoot = registry.LOCAL_MACHINE
)

// regGetString reads a string value from the registry.
func regGetString(root regRoot, path, name string) (string, error) {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	return v, err
}

// regSubKeys lists subkey names under path.
func regSubKeys(root regRoot, path string) ([]string, error) {
	k, err := registry.OpenKey(root, path, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}
	defer k.Close()
	return k.ReadSubKeyNames(-1)
}
