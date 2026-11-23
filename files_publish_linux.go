package main

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func publishNoReplace(root *os.Root, staged, name string) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	fd := int(dir.Fd())
	err = unix.Renameat2(fd, staged, fd, name, unix.RENAME_NOREPLACE)
	// Older kernels or mounts can support links without supporting rename flags.
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EOPNOTSUPP) {
		return root.Link(staged, name)
	}
	return err
}
