package search

// categoryIDs maps the App's result types to Newznab parent categories, the
// same families search-helper's category() names results by.
var categoryIDs = map[string]int{"movie": 2000, "music": 3000, "software": 4000, "series": 5000, "anime": 5070, "book": 7000, "other": 8000}

// CategoryIDs returns the Newznab IDs for a category name; "" is every
// category and sends none. ok is false for an unknown name.
func CategoryIDs(name string) (ids []int, ok bool) {
	if name == "" {
		return nil, true
	}
	id, ok := categoryIDs[name]
	if !ok {
		return nil, false
	}
	return []int{id}, true
}
