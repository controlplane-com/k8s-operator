package common

func MapValues[T any](m map[string]T) []T {
	var v []T
	for _, mv := range m {
		v = append(v, mv)
	}
	return v
}
