package qfnu

import (
	"fmt"
	"io"
)

var version = "dev"

func usage(w io.Writer) int {
	text := `Usage: easy-qfnu <command> [args]

Commands:
  easy-qfnu jwc list [--channel notices] [--page 1] [--limit 10]
  easy-qfnu jwc get <url-or-info-path>
  easy-qfnu jwc search <keyword> [--page 1] [--limit 10]
  easy-qfnu jwc channels
  easy-qfnu precourse search [keyword] [--course-code value] [--campus value]
  easy-qfnu precourse meta | popular --field <teacherName|courseName|college>
  easy-qfnu recommendation search [--course value] [--teacher value] [--top 20]
  easy-qfnu jwxt captcha [--out 图片路径]
  easy-qfnu jwxt login [--username 学号] [--password 密码] [--captcha 识图结果]
  easy-qfnu jwxt grades [--semester 学年学期]
  easy-qfnu jwxt schedule [--semester 学年学期] [--week 周次] [--kbjcmsid 节次模式]
  easy-qfnu jwxt evaluations | evaluate [--score 89] [--course ID] [--confirm]
  easy-qfnu jwxt status | logout | forget-credentials | relay <feedback|recommendation|rank>
  version
在线查询与提交功能（jwc、freshman、precourse、recommendation、jwxt 登录成功/成绩/课表/评教、relay）执行后会
上报匿名功能事件：功能名、成功状态、时间、CLI 版本、操作系统和架构；不含学号、姓名、查询内容
或 Cookie；登录失败与纯本地操作（captcha/status/logout）不上报，上报失败不影响命令结果。

Prints JSON. Most commands are read-only; jwxt evaluate submits only with explicit --confirm.
`
	if _, err := fmt.Fprint(w, text); err != nil {
		return 1
	}
	return 2
}

// Run dispatches the stable easy-qfnu command contract. It deliberately avoids a
// third-party parser so the released binary has no runtime dependencies.
func Run(args []string, out, errOut io.Writer) int {
	if code := checkUpdates(out); code != 0 {
		return code
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		return usage(errOut)
	}
	if args[0] == "version" || args[0] == "--version" {
		return writeJSON(out, success("easy-qfnu", payload{"version": version, "command": "easy-qfnu"}))
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
	case "precourse", "precourses":
		code = runPrecourse(args[1:], out)
	case "recommendation", "recommendations":
		code = runRecommendation(args[1:], out)
	case "jwxt":
		code = runJWXT(args[1:], out)
	default:
		if _, err := fmt.Fprintf(errOut, "unknown command: %s\n", args[0]); err != nil {
			return 1
		}
		return usage(errOut)
	}
	return code
}
