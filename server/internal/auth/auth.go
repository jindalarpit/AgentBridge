// Package auth provides JWT token management and authentication utilities.
package auth

import (
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// Ensure imports are used.
var (
	_ = jwt.SigningMethodHS256
	_ = bcrypt.DefaultCost
)
