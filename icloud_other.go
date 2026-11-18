//go:build !darwin

package main

import "github.com/monoedge-labs/zmail/internal/mailbox"

func openICloudKVS(onChange func()) mailbox.KVStore { return nil }
