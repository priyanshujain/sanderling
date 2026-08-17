package main

import (
	"regexp"
	"strings"
)

// tableRow is one pipe-delimited markdown row with its 1-based line number, so
// a malformed cell can name the line the author has to go and fix.
type tableRow struct {
	Line  int
	Cells []string
}

var separatorCell = regexp.MustCompile(`^:?-{2,}:?$`)

func parseTableRows(body string) []tableRow {
	var rows []tableRow
	for index, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := splitCells(line)
		if len(cells) == 0 || isSeparatorRow(cells) {
			continue
		}
		rows = append(rows, tableRow{Line: index + 1, Cells: cells})
	}
	return rows
}

func splitCells(line string) []string {
	trimmed := strings.Trim(line, "|")
	parts := strings.Split(trimmed, "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, normalizeCell(part))
	}
	return cells
}

func isSeparatorRow(cells []string) bool {
	for _, cell := range cells {
		if !separatorCell.MatchString(cell) {
			return false
		}
	}
	return true
}

func normalizeCell(value string) string {
	cleaned := strings.ReplaceAll(value, "**", "")
	cleaned = strings.ReplaceAll(cleaned, "`", "")
	return strings.Join(strings.Fields(cleaned), " ")
}

func (row tableRow) cell(index int) string {
	if index >= len(row.Cells) {
		return ""
	}
	return row.Cells[index]
}

var listSeparators = regexp.MustCompile(`[,\s]+`)

func splitList(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	var items []string
	for _, item := range listSeparators.Split(trimmed, -1) {
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}
