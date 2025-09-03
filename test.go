package main

func main() {
	if err := run(); err != nil {
		panic(err)
	}

	println("All tests passed")

}

// create a function that compares two slices of CSVRow structs and returns true if they are equal
func compareCSVRows(a, b []CSVRow) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}