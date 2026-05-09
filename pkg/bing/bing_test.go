package bing

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestBingSearch(t *testing.T) {
	opts := DefaultOptions()
	s := NewSearcher(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Printf("引擎: %v\n", s.Engines())
	responses := s.Search(ctx, "golang concurrency", 1)

	for _, resp := range responses {
		fmt.Printf("\n[%s] error=%q results=%d\n", resp.Engine, resp.Error, len(resp.Results))
		for i, r := range resp.Results {
			if i >= 3 {
				break
			}
			fmt.Printf("  %d. %s\n     %s\n", i+1, r.Title, r.URL)
		}
	}
}

func TestAcademicSearch(t *testing.T) {
	opts := AcademicOptions()
	opts.Strategy = StrategyParallel
	s := NewSearcher(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Printf("引擎: %v\n", s.Engines())
	responses := s.Search(ctx, "transformer attention mechanism", 1)

	for _, resp := range responses {
		fmt.Printf("\n[%s] error=%q results=%d\n", resp.Engine, resp.Error, len(resp.Results))
		for i, r := range resp.Results {
			if i >= 2 {
				break
			}
			fmt.Printf("  %d. [%s] %s\n     %s\n", i+1, r.Type, r.Title, r.URL)
		}
	}
}
