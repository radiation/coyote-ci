package domain

const (
	MinPriority     = 1
	MaxPriority     = 10
	DefaultPriority = 5
)

func NormalizePriority(priority int) int {
	if priority == 0 {
		return DefaultPriority
	}
	return priority
}

func ValidPriority(priority int) bool {
	return priority >= MinPriority && priority <= MaxPriority
}
