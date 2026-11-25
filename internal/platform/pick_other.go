//go:build !windows && !darwin && !linux

package platform

func pickDirectory() (string, error) { return "", ErrUnavailable }
