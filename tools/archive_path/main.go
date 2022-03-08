// Copyright 2020-present Yarn.social
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/base32"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func decodeHash(hash string) ([]byte, error) {
	encoding := base32.StdEncoding.WithPadding(base32.NoPadding)
	return encoding.DecodeString(strings.ToUpper(hash))
}

func makePath(hash string) (string, error) {
	bs, err := decodeHash(hash)
	if err != nil {
		return "", err
	}

	if len(bs) < 2 {
		return "", fmt.Errorf("error: invalid hash %q", hash)
	}

	// Produces a path structure of:
	// ./data/archive/[0-9a-f]{2,}/[0-9a-f]+.json
	components := []string{"archive", fmt.Sprintf("%x", bs[0:1]), fmt.Sprintf("%x.json", bs[1:])}

	return filepath.Join(components...), nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <hash>\n", filepath.Base(os.Args[0]))
		os.Exit(1)
	}

	hash := os.Args[1]
	path, err := makePath(hash)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error computing archive path for hash %s: %s", hash, err)
		os.Exit(2)
	}

	fmt.Println(path)
}
