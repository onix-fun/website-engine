// Package environment describes the deployable content environment.
package environment

import (
	"context"
	"fmt"
)

type Environment string

const (
	Dev  Environment = "dev"
	Prod Environment = "prod"
)

func Parse(raw string) (Environment, error) {
	switch Environment(raw) {
	case Dev:
		return Dev, nil
	case Prod:
		return Prod, nil
	default:
		return "", fmt.Errorf("environment must be dev or prod")
	}
}

func (e Environment) Valid() bool {
	return e == Dev || e == Prod
}

func (e Environment) String() string { return string(e) }

type contextKey struct{}

func WithContext(ctx context.Context, env Environment) context.Context {
	return context.WithValue(ctx, contextKey{}, env)
}

func FromContext(ctx context.Context) Environment {
	if env, ok := ctx.Value(contextKey{}).(Environment); ok && env.Valid() {
		return env
	}
	return Prod
}

func IsDev(env Environment) bool  { return env == Dev }
func IsProd(env Environment) bool { return env == Prod }
