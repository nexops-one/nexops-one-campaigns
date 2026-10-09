// SPDX-License-Identifier: Apache-2.0

//go:build unix

package crypt

import (
	"os"
	"syscall"
)

// ownGroup reports whether the file's group is the process's group or one of
// its supplementary groups.
func ownGroup(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	gid := int(st.Gid)
	if gid == os.Getegid() || gid == os.Getgid() {
		return true
	}
	groups, err := os.Getgroups()
	if err != nil {
		return false
	}
	for _, g := range groups {
		if g == gid {
			return true
		}
	}
	return false
}
