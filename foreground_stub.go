//go:build !windows

package main

import "errors"

func ForegroundProcessName() (string, error) {
	return "", errors.New("ForegroundProcessName is only supported on Windows")
}
