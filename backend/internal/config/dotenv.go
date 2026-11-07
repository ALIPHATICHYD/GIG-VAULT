package config

import "os"

// LoadDotEnv reads KEY=VALUE lines from path into the environment without
// overriding variables that are already set, so a real environment variable
// always wins over the file. A missing file is not an error: in production the
// environment is set by the platform and there is no .env at all.
//
// Paths are relative to the working directory, which is why the commands are
// run from backend/.
func LoadDotEnv(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range splitLines(string(b)) {
		if line == "" || line[0] == '#' {
			continue
		}
		for i := 0; i < len(line); i++ {
			if line[i] == '=' {
				k, v := line[:i], line[i+1:]
				if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
					v = v[1 : len(v)-1]
				} else {
					// Strip a trailing comment, but only when the # follows
					// whitespace, so values containing # survive. .env.example
					// documents settings this way: "DEPOSIT_BPS=2000  # 20%".
					for j := 0; j < len(v); j++ {
						if v[j] == '#' && (j == 0 || v[j-1] == ' ' || v[j-1] == '\t') {
							v = v[:j]
							break
						}
					}
					for len(v) > 0 && (v[len(v)-1] == ' ' || v[len(v)-1] == '\t') {
						v = v[:len(v)-1]
					}
				}
				if v == "" {
					break
				}
				if _, set := os.LookupEnv(k); !set {
					os.Setenv(k, v)
				}
				break
			}
		}
	}
}

func splitLines(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			out = append(out, line)
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
