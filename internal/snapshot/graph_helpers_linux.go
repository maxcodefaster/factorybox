package snapshot

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

func graphWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." &&
		!strings.HasPrefix(relative, "../") && !filepath.IsAbs(relative)
}

func asJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}
