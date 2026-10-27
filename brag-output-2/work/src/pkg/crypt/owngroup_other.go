// SPDX-License-Identifier: Apache-2.0

//go:build !unix

package crypt

import "os"

// ownGroup is false where file groups are not checked (LoadKEK skips the
// mode check on Windows).
func ownGroup(os.FileInfo) bool { return false }
