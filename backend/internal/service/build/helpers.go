package build

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func defaultArgs(args []string) []string {
	if len(args) == 0 {
		return []string{"-c", "echo coyote-ci worker default step && exit 0"}
	}
	return args
}

func defaultEnv(env map[string]string) map[string]string {
	if env == nil {
		return map[string]string{}
	}
	return env
}

func maxInt(value int, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}
