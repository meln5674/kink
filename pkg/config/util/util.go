package util

func OverrideString(s1, s2 *string) {
	if *s1 != "" {
		return
	}
	*s1 = *s2
}

func OverrideInt(s1, s2 *int) {
	if *s1 != 0 {
		return
	}
	*s1 = *s2
}

func OverrideStringSlice(s1, s2 *[]string) {
	if *s2 == nil || len(*s2) == 0 {
		return
	}
	*s1 = make([]string, len(*s2))
	copy(*s1, *s2)
}

func OverrideStringToString(m1, m2 *map[string]string) {
	if *m2 == nil {
		return
	}
	if *m1 == nil {
		*m1 = make(map[string]string, len(*m2))
	}
	for k, v := range *m2 {
		if v == "" {
			continue
		}
		(*m1)[k] = v
	}
}
