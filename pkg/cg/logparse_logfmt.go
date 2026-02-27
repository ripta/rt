package cg

// parseLogfmt parses a logfmt-encoded line into key-value pairs. Returns nil
// if no pairs are found.
func parseLogfmt(line string) map[string]any {
	m := make(map[string]any)
	i := 0
	n := len(line)

	for i < n {
		// Skip whitespace between pairs
		for i < n && line[i] == ' ' {
			i++
		}
		if i >= n {
			break
		}

		// Parse key
		keyStart := i
		for i < n && line[i] != '=' && line[i] != ' ' {
			i++
		}
		if i == keyStart {
			break
		}
		key := line[keyStart:i]

		if i >= n || line[i] == ' ' {
			// Bare key (no '='), treat as boolean true
			m[key] = true
			continue
		}

		// Skip '='
		i++

		if i >= n || line[i] == ' ' {
			// key= with no value, treat as empty string
			m[key] = ""
			continue
		}

		if line[i] == '"' {
			// Quoted value
			i++ // skip opening quote
			var val []byte
			for i < n && line[i] != '"' {
				if line[i] == '\\' && i+1 < n {
					switch line[i+1] {
					case '"', '\\':
						val = append(val, line[i+1])
						i += 2
						continue
					}
				}
				val = append(val, line[i])
				i++
			}
			if i < n {
				i++ // skip closing quote
			}
			m[key] = string(val)
		} else {
			// Unquoted value
			valStart := i
			for i < n && line[i] != ' ' {
				i++
			}
			m[key] = line[valStart:i]
		}
	}

	if len(m) == 0 {
		return nil
	}
	return m
}

// LogfmtProcessorOptions configures the logfmt log line processor.
type LogfmtProcessorOptions struct {
	MessageKey   string
	TimestampKey string
	TimestampFmt string
	Fields       []string
	Format       string
}

// NewLogfmtProcessor returns a LineProcessor that extracts the message from
// logfmt log lines. Non-logfmt lines and lines missing the message key pass
// through unchanged.
func NewLogfmtProcessor(opts LogfmtProcessorOptions) LineProcessor {
	var tsParser *TimestampParser
	if opts.TimestampKey != "" {
		tsParser = NewTimestampParser(opts.TimestampFmt, opts.Format)
	}

	return func(line string) *ProcessedLine {
		obj := parseLogfmt(line)
		if obj == nil {
			return nil
		}

		msgVal, ok := obj[opts.MessageKey]
		if !ok {
			return nil
		}

		msg := stringify(msgVal)

		var suffix string
		if len(opts.Fields) > 0 {
			suffix = formatFields(obj, opts.Fields, opts.MessageKey, opts.TimestampKey)
		}

		var prefix string
		if tsParser != nil {
			if tsVal, ok := obj[opts.TimestampKey]; ok {
				prefix = tsParser.Parse(tsVal)
			}
		}

		return &ProcessedLine{
			Prefix: prefix,
			Line:   msg + suffix,
		}
	}
}
