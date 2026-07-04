package output

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

type Mode string

const (
	ModeHuman Mode = "human"
	ModeJSON  Mode = "json"
)

func Normalize(raw string) (Mode, error) {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	switch trimmed {
	case "", string(ModeHuman):
		return ModeHuman, nil
	case string(ModeJSON):
		return ModeJSON, nil
	default:
		return "", errors.New("output must be human or json")
	}
}

func Write(mode Mode, writer io.Writer, human func(io.Writer) error, payload any) error {
	if mode == ModeJSON {
		encoder := json.NewEncoder(writer)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(payload)
	}
	return human(writer)
}
