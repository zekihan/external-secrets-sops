// Package reposync provides a shared RWMutex used to coordinate git worktree
// writes (Pull) against concurrent secret file reads.
package reposync

import "sync"

// Mu guards access to the local git worktree. Git.Pull acquires a write lock;
// secret.GetAllSecrets acquires a read lock.
var Mu sync.RWMutex
