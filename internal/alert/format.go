package alert

import (
	"fmt"
	"sort"
	"strings"
)

// table is a tiny fixed-width text table builder, rendered inside a
// Discord ``` code block so it lines up in a monospace font.
type table struct {
	headers []string
	rows    [][]string
}

func newTable(headers ...string) *table {
	return &table{headers: headers}
}

func (t *table) addRow(cols ...string) {
	t.rows = append(t.rows, cols)
}

func (t *table) render() string {
	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = len(h)
	}
	for _, row := range t.rows {
		for i, c := range row {
			if i < len(widths) && len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
	}

	var b strings.Builder
	b.WriteString("```\n")
	writeRow := func(cols []string) {
		for i, c := range cols {
			if i > 0 {
				b.WriteString("  ")
			}
			fmt.Fprintf(&b, "%-*s", widths[i], c)
		}
		b.WriteString("\n")
	}
	writeRow(t.headers)
	for _, row := range t.rows {
		writeRow(row)
	}
	b.WriteString("```")
	return b.String()
}

// sortedDiskMounts returns disk mount points sorted for stable table
// ordering, "/boot" pinned last since it's called out separately in the
// task's metric list.
func sortedDiskMounts(mounts map[string]float64) []string {
	keys := make([]string, 0, len(mounts))
	for k := range mounts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == "/boot" {
			return false
		}
		if keys[j] == "/boot" {
			return true
		}
		return keys[i] < keys[j]
	})
	return keys
}
