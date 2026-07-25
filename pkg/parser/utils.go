package parser

import "errors"

func extractFirstJSON(data []byte) ([]byte, error) {
	start := -1
	depth := 0
	inString := false
	escaped := false

	for i, b := range data {
		if inString {
			if escaped {
				escaped = false
			} else if b == '\\' {
				escaped = true
			} else if b == '"' {
				inString = false
			}
			continue
		}

		switch b {
		case '"':
			if depth > 0 {
				inString = true
			}
		case '{', '[':
			if depth == 0 {
				start = i
			}
			depth++
		case '}', ']':
			if depth > 0 {
				depth--
				if depth == 0 && start != -1 {
					return data[start : i+1], nil
				}
			}
		}
	}

	return nil, errors.New("no JSON object or array found")
}
