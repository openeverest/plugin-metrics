package promapi

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Dashboard queries use these placeholders; the selector confines every query
// to one instance's series, so it is mandatory.
const (
	SelectorPlaceholder = "${selector}"
	RatePlaceholder     = "${rate}"
)

// Shorter windows than four scrape intervals make rate() return gaps.
const minRateInterval = 2 * time.Minute

// ValidateQuery rejects a query that would read every instance's metrics.
func ValidateQuery(query string) error {
	if !strings.Contains(query, SelectorPlaceholder) {
		return fmt.Errorf("query must be scoped with %s", SelectorPlaceholder)
	}
	return nil
}

// RenderQuery fills in the placeholders: selector is the label matchers that
// scope the query to the instance, without braces.
func RenderQuery(query, selector string, step time.Duration) (string, error) {
	if err := ValidateQuery(query); err != nil {
		return "", err
	}
	rate := max(step, minRateInterval)
	return strings.NewReplacer(
		SelectorPlaceholder, selector,
		RatePlaceholder, strconv.Itoa(int(rate.Seconds()))+"s",
	).Replace(query), nil
}
