package e

import (
	"errors"
	"fmt"
)

func Wrap(msg string, err error) error {
	if err == nil {
		return errors.New(msg)
	}

	return fmt.Errorf("%s: %w", msg, err)
}

func WrapIfErr(msg string, err error) error {
	if err == nil {
		return nil
	}

	return Wrap(msg, err)
}
