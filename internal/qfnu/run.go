package qfnu

import (
	"fmt"
	"io"
)

var version = "dev"

func usage(w io.Writer) int {
	fmt.Fprintln(w, "Usage: easy-qfnu <command> [args]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  easy-qfnu jwc list [--channel notices] [--page 1] [--limit 10]")
	fmt.Fprintln(w, "  easy-qfnu jwc get <url-or-info-path>")
	fmt.Fprintln(w, "  easy-qfnu jwc search <keyword> [--page 1] [--limit 10]")
	fmt.Fprintln(w, "  easy-qfnu jwc channels")
	fmt.Fprintln(w, "  easy-qfnu freshman search <keyword> [--page 1] [--page-size 20]")
	fmt.Fprintln(w, "  easy-qfnu jwxt captcha [--out 图片路径]")
	fmt.Fprintln(w, "  easy-qfnu jwxt login [--username 学号] [--password 密码] [--captcha 识图结果]")
	fmt.Fprintln(w, "  easy-qfnu jwxt grades [--semester 学年学期]")
	fmt.Fprintln(w, "  easy-qfnu jwxt schedule [--semester 学年学期] [--week 周次] [--kbjcmsid 节次模式]")
	fmt.Fprintln(w, "  easy-qfnu jwxt evaluations | evaluate [--score 89] [--course ID] [--confirm]")
	fmt.Fprintln(w, "  easy-qfnu jwxt status | logout | forget-credentials")
	fmt.Fprintln(w, "  version")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Prints JSON. Most commands are read-only; jwxt evaluate submits only with explicit --confirm.")
	return 2
}

// Run dispatches the stable qfnu command contract. It deliberately avoids a
// third-party parser so the released binary has no runtime dependencies.
func Run(args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		return usage(errOut)
	}
	if args[0] == "version" || args[0] == "--version" {
		return writeJSON(out, success("qfnu", payload{"version": version, "command": "easy-qfnu"}))
	}
	if len(args) < 2 {
		return usage(errOut)
	}
	var code int
	switch args[0] {
	case "jwc":
		code = runJWC(args[1:], out)
	case "freshman":
		code = runFreshman(args[1:], out)
	case "jwxt":
		code = runJWXT(args[1:], out)
	default:
		fmt.Fprintf(errOut, "unknown command: %s\n", args[0])
		return usage(errOut)
	}
	return code
}
