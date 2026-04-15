package goversioning

import "strconv"

func Parse(v string) (int, int, int) {
	parts := [3]string{"", "", ""}
	idx := 0
	for _, r := range v {
		if r == '.' {
			idx++
			if idx > 2 {
				break
			}
			continue
		}
		parts[idx] += string(r)
	}
	maj, _ := strconv.Atoi(parts[0])
	min, _ := strconv.Atoi(parts[1])
	pat, _ := strconv.Atoi(parts[2])
	return maj, min, pat
}
