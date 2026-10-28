//go:build !windows

package main

import "fmt"

func platformEndpoint(socket, pipe, _ string) (string, error) {
	if socket == "" || pipe != "" {
		return "", fmt.Errorf("invalid local endpoint")
	}
	return socket, nil
}
