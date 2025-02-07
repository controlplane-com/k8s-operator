package common

import (
	"errors"
	"fmt"
)

var DependentResourceErr = errors.New("resource deletion failed. Another resource depends on this one. Deletion will be retried later")

var NotFoundError = fmt.Errorf("cpln resource not found")
