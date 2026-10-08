//go:build !testseed

package main

import (
	"context"
	"errors"
)

func (s *Service) seed(_ context.Context) error {
	return errors.New("test seed is not included in this production build")
}
