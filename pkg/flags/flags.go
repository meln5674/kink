package flags

func AsFlags(fMap map[string]string) []string {
	fList := make([]string, 0, len(fMap)*2)
	for key, value := range fMap {
		fList = append(fList, "--"+key, value)
	}
	return fList
}
