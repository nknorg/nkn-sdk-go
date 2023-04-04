package nkn

import (
	"context"
	"log"

	"github.com/nknorg/nkngomobile"
)

type Resolver interface {
	nkngomobile.Resolver
	ResolveContext(ctx context.Context, address string) (string, error)
}

func ResolveDest(ctx context.Context, dest string, resolvers []Resolver) (string, bool, error) {
	var err error
	for _, r := range resolvers {
		var d string
		d, err = r.ResolveContext(ctx, dest)
		if err != nil {
			return "", true, err
		}
		if len(d) == 0 {
			continue
		}
		return d, true, nil
	}
	return dest, false, err
}

func ResolveDestN(ctx context.Context, dest string, resolvers []Resolver, depth int32) (string, error) {
	for i := int32(0); i < depth; i++ {
		d, isResolved, err := ResolveDest(ctx, dest, resolvers)
		if err != nil {
			return "", err
		}
		if !isResolved {
			return dest, nil
		}
		dest = d
	}
	return "", ErrResolveLimit
}

func ResolveDests(ctx context.Context, dests []string, resolvers []Resolver, depth int32) ([]string, error) {
	if len(dests) == 0 {
		return nil, nil
	}
	resolvedDests := make([]string, 0, len(dests))
	for _, dest := range dests {
		resolvedDest, err := ResolveDestN(ctx, dest, resolvers, depth)
		if err != nil {
			log.Println(err)
			continue
		}
		resolvedDests = append(resolvedDests, resolvedDest)
	}
	if len(resolvedDests) == 0 {
		return nil, ErrInvalidDestination
	}
	return resolvedDests, nil
}
