package bdds

import "fmt"

// AuthError represents an authentication error
type AuthError struct {
	StatusCode int
	Message    string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("authentication failed (status %d): %s", e.StatusCode, e.Message)
}

// NotFoundError represents a resource not found error
type NotFoundError struct {
	Resource string
	ID       string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s not found: %s", e.Resource, e.ID)
}

// RateLimitError represents a rate limit error
type RateLimitError struct {
	RetryAfter int // seconds
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limited, retry after %d seconds", e.RetryAfter)
}

// statusError represents an unexpected HTTP status response. It carries the
// status code so retry logic can distinguish transient (5xx) from permanent
// (4xx) failures.
type statusError struct {
	StatusCode int
	Body       string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("unexpected status %d: %s", e.StatusCode, e.Body)
}
