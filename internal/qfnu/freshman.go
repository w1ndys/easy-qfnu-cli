package qfnu

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

type freshmanSearch struct {
	keyword  string
	page     int
	pageSize int
}

func runFreshman(args []string, out io.Writer) int {
	if len(args) == 0 || args[0] == "--help" {
		return printFreshmanUsage(out)
	}
	if args[0] != "search" {
		return writeJSON(out, failure("freshman", "unknown action: "+args[0], ""))
	}
	search, err := parseFreshmanSearch(args[1:])
	if err != nil {
		reportUsage("freshman.search", "failure")
		return writeJSON(out, failure("freshman", err.Error(), ""))
	}
	result, queryErr := queryFreshman(search)
	if queryErr != nil {
		reportUsage("freshman.search", "failure")
		return writeJSON(out, failure("freshman", queryErr.message, queryErr.hint))
	}
	reportUsage("freshman.search", "success")
	return writeJSON(out, result)
}

func printFreshmanUsage(out io.Writer) int {
	if _, err := fmt.Fprintln(out, "Usage: easy-qfnu freshman search <keyword> [--page 1] [--page-size 20]"); err != nil {
		return 1
	}
	return 2
}

func parseFreshmanSearch(args []string) (freshmanSearch, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return freshmanSearch{}, fmt.Errorf("search keyword is empty")
	}
	search := freshmanSearch{keyword: strings.TrimSpace(args[0]), page: 1, pageSize: 20}
	for index := 1; index < len(args); index++ {
		arg := args[index]
		if index+1 >= len(args) {
			return freshmanSearch{}, fmt.Errorf("%s requires a value", arg)
		}
		value, err := strconv.Atoi(args[index+1])
		if err != nil && (arg == "--page" || arg == "--page-size") {
			return freshmanSearch{}, fmt.Errorf("%s must be an integer", arg)
		}
		switch arg {
		case "--page":
			search.page = value
		case "--page-size":
			search.pageSize = value
		default:
			return freshmanSearch{}, fmt.Errorf("unknown option: %s", arg)
		}
		index++
	}
	if search.page < 1 || search.pageSize < 1 || search.pageSize > 100 {
		return freshmanSearch{}, fmt.Errorf("page must be >= 1 and page-size must be between 1 and 100")
	}
	return search, nil
}
