//go:build !testseed

package main

import (
	"context"
	"errors"
)

// testSeedBuild marks the isolated Compose test binary (go build -tags testseed).
const testSeedBuild = false

func (s *Service) seed(_ context.Context) error {
	return errors.New("test seed is not included in this production build")
}
