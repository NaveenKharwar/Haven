//go:build !linux

package main

import "os"

func publishNoReplace(root *os.Root, staged, name string) error {
	return root.Link(staged, name)
}
