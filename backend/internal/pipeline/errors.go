package pipeline

import "fmt"

// ValidationError represents a single validation problem.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s", e.Field, e.Message)
	}
	return e.Message
}

// ValidationErrors collects multiple validation problems.
type ValidationErrors []ValidationError

func (errs ValidationErrors) Error() string {
	if len(errs) == 0 {
		return "validation failed"
	}
	if len(errs) == 1 {
		return errs[0].Error()
	}
	msg := fmt.Sprintf("%d validation errors:", len(errs))
	for _, e := range errs {
		msg += "\n  - " + e.Error()
	}
	return msg
}

// ParseError indicates a failure to parse YAML.
type ParseError struct {
	Err error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("pipeline YAML parse error: %v", e.Err)
}

func (e *ParseError) Unwrap() error {
	return e.Err
}
